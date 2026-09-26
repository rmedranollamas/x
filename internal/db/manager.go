package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNoDatabase is returned when a requested operation requires an existing database file.
var ErrNoDatabase = errors.New("no database found")

// DBManager manages SQLite connection lifecycle, transaction execution,
// migrations, and database backup routines.
type DBManager struct {
	dbPath string
	sqlDB  *sql.DB
	mu     sync.Mutex // Serializes backup operations and lifecycle mutations
}

// DBTX abstracts *sql.DB and *sql.Tx for common query/exec operations.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// NewDBManager initializes a new pure Go SQLite connection pool with zero CGO dependencies.
func NewDBManager(dbPath string) (*DBManager, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, errors.New("database path cannot be empty")
	}

	cleanPath := filepath.Clean(dbPath)
	dir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", dir, err)
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(DELETE)&_pragma=foreign_keys(ON)", cleanPath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// SQLite single-writer safety: enforce 1 open connection to eliminate SQLITE_BUSY
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("failed to ping sqlite database at %s: %w", cleanPath, err)
	}

	return &DBManager{
		dbPath: cleanPath,
		sqlDB:  sqlDB,
	}, nil
}

// Close closes the underlying SQLite connection pool.
func (m *DBManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sqlDB != nil {
		return m.sqlDB.Close()
	}
	return nil
}

// DB returns the underlying *sql.DB instance.
func (m *DBManager) DB() *sql.DB {
	return m.sqlDB
}

// DBPath returns the configured filesystem path of the database.
func (m *DBManager) DBPath() string {
	return m.dbPath
}

// WithTx executes the given callback within an atomic transaction.
// If fn returns nil, the transaction commits. If fn returns an error or panics,
// the transaction automatically rolls back.
func (m *DBManager) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	tx, beginErr := m.sqlDB.BeginTx(ctx, nil)
	if beginErr != nil {
		return fmt.Errorf("failed to begin transaction: %w", beginErr)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p) // Re-throw panic after rollback
		} else if err != nil {
			_ = tx.Rollback()
		} else {
			if commitErr := tx.Commit(); commitErr != nil {
				err = fmt.Errorf("failed to commit transaction: %w", commitErr)
			}
		}
	}()

	err = fn(tx)
	return err
}

// BackupDatabase creates a timestamped copy of the current database file.
// Target matches Python: <db_dir>/backups/{stem}_%Y%m%d_%H%M%S.db.
// If the database file does not exist, returns ("", nil) without error.
func (m *DBManager) BackupDatabase() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backupDatabaseLocked()
}

// backupDatabaseLocked performs the actual database backup while m.mu is already held.
func (m *DBManager) backupDatabaseLocked() (string, error) {
	fi, err := os.Stat(m.dbPath)
	if os.IsNotExist(err) || (err == nil && fi.Size() == 0) {
		slog.Warn("No database found to backup.", "path", m.dbPath)
		return "", nil
	}

	backupDir := filepath.Join(filepath.Dir(m.dbPath), "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory %s: %w", backupDir, err)
	}

	baseName := filepath.Base(m.dbPath)
	stem := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("%s_%s.db", stem, timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	srcFile, err := os.Open(m.dbPath)
	if err != nil {
		return "", fmt.Errorf("failed to open source database for backup: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file %s: %w", backupPath, err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return "", fmt.Errorf("failed to copy database content to %s: %w", backupPath, err)
	}

	if err := dstFile.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync backup file %s: %w", backupPath, err)
	}

	slog.Info("Database backed up successfully", "backup_path", backupPath)
	return backupPath, nil
}
