package db

import (
	"context"
	"database/sql"
	"fmt"
)

// Migration represents a single database schema migration.
type Migration interface {
	Version() int
	Description() string
	Up(ctx context.Context, tx *sql.Tx) error
}

// defaultRegistry lists all standard migrations in sequential order.
var defaultRegistry = []Migration{
	&m001Initial{},
	&m002AddListedCount{},
	&m003CreateDeletedTweets{},
	&m004RenameViewsToEngagement{},
}

// RunMigrations executes all pending migrations sequentially within a single transaction.
// It automatically creates a database backup if pending migrations exist.
func (m *DBManager) RunMigrations() error {
	return m.RunMigrationsContext(context.Background())
}

// RunMigrationsContext executes all pending migrations with context cancellation support.
func (m *DBManager) RunMigrationsContext(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Ensure schema_versions table exists
	if err := m.ensureSchemaVersionsTable(ctx); err != nil {
		return fmt.Errorf("ensure schema_versions table: %w", err)
	}

	// 2. Query already applied migration versions
	applied, err := m.getAppliedVersionsMap(ctx)
	if err != nil {
		return fmt.Errorf("get applied versions: %w", err)
	}

	// 3. Filter pending migrations
	var pending []Migration
	for _, mig := range defaultRegistry {
		if !applied[mig.Version()] {
			pending = append(pending, mig)
		}
	}

	// Idempotency: no-op if no pending migrations (0 backups, 0 tx)
	if len(pending) == 0 {
		return nil
	}

	// 4. Trigger pre-migration backup before applying any changes
	if _, err := m.backupDatabaseLocked(); err != nil {
		return fmt.Errorf("pre-migration backup failed: %w", err)
	}

	// 5. Execute pending migrations in a single atomic transaction
	tx, err := m.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	var committed bool
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, mig := range pending {
		if err := mig.Up(ctx, tx); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", mig.Version(), mig.Description(), err)
		}

		insertSQL := `INSERT INTO schema_versions (version, description) VALUES (?, ?);`
		if _, err := tx.ExecContext(ctx, insertSQL, mig.Version(), mig.Description()); err != nil {
			return fmt.Errorf("record migration %d: %w", mig.Version(), err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
	}
	committed = true

	return nil
}

func (m *DBManager) ensureSchemaVersionsTable(ctx context.Context) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS schema_versions (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
		description TEXT
	);`
	_, err := m.sqlDB.ExecContext(ctx, ddl)
	return err
}

func (m *DBManager) getAppliedVersionsMap(ctx context.Context) (map[int]bool, error) {
	rows, err := m.sqlDB.QueryContext(ctx, `SELECT version FROM schema_versions ORDER BY version ASC;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// ----------------------------------------------------------------------------
// Introspection & Dynamic DDL Helpers
// ----------------------------------------------------------------------------

func tableColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	switch table {
	case "insights", "blocked_users", "following_users", "followers", "unfollows", "deleted_tweets", "schema_versions":
	default:
		return nil, fmt.Errorf("table %q not in allowed migration schema", table)
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err != nil {
		return nil, fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("scan pragma table_info: %w", err)
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func ensureColumn(ctx context.Context, tx *sql.Tx, table, column, colDef string) error {
	cols, err := tableColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	if !cols[column] {
		query := fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "%s" %s;`, table, column, colDef)
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("add column %s to %s: %w", column, table, err)
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// Concrete Migrations: m001 - m004
// ----------------------------------------------------------------------------

// m001Initial creates initial schema and handles legacy column backfills & cleanups.
type m001Initial struct{}

func (m *m001Initial) Version() int { return 1 }
func (m *m001Initial) Description() string {
	return "Initial schema with insights, blocked_users, and followers tables."
}

func (m *m001Initial) Up(ctx context.Context, tx *sql.Tx) error {
	ddls := []string{
		`CREATE TABLE IF NOT EXISTS insights (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
			followers INTEGER,
			following INTEGER,
			tweet_count INTEGER DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS blocked_users (
			user_id INTEGER PRIMARY KEY,
			status TEXT DEFAULT 'PENDING',
			updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
		);`,
		`CREATE TABLE IF NOT EXISTS following_users (
			user_id INTEGER PRIMARY KEY,
			status TEXT DEFAULT 'PENDING',
			updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
		);`,
		`CREATE TABLE IF NOT EXISTS followers (
			user_id INTEGER PRIMARY KEY,
			updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
		);`,
		`CREATE TABLE IF NOT EXISTS unfollows (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			timestamp DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
		);`,
	}

	for _, ddl := range ddls {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("m001 execute ddl: %w", err)
		}
	}

	// Idempotent column additions for pre-m001 schemas
	if err := ensureColumn(ctx, tx, "insights", "tweet_count", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, tx, "blocked_users", "status", "TEXT DEFAULT 'PENDING'"); err != nil {
		return err
	}
	if err := ensureColumn(ctx, tx, "blocked_users", "updated_at", "DATETIME"); err != nil {
		return err
	}

	// Legacy data cleanup
	cols, err := tableColumns(ctx, tx, "blocked_users")
	if err != nil {
		return err
	}

	if cols["updated_at"] {
		_, err = tx.ExecContext(ctx, `
			UPDATE blocked_users
			SET updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
			WHERE updated_at IS NULL;
		`)
		if err != nil {
			return fmt.Errorf("m001 backfill null updated_at: %w", err)
		}
	}

	if cols["unblocked_at"] {
		_, err = tx.ExecContext(ctx, `
			UPDATE blocked_users
			SET status = 'UNBLOCKED'
			WHERE unblocked_at IS NOT NULL AND status = 'PENDING';
		`)
		if err != nil {
			return fmt.Errorf("m001 migrate legacy unblocked_at data: %w", err)
		}
	}

	return nil
}

// m002AddListedCount adds listed_count column to insights table.
type m002AddListedCount struct{}

func (m *m002AddListedCount) Version() int { return 2 }
func (m *m002AddListedCount) Description() string {
	return "Add listed_count column to insights table."
}

func (m *m002AddListedCount) Up(ctx context.Context, tx *sql.Tx) error {
	return ensureColumn(ctx, tx, "insights", "listed_count", "INTEGER DEFAULT 0")
}

// m003CreateDeletedTweets creates deleted_tweets table with views column.
type m003CreateDeletedTweets struct{}

func (m *m003CreateDeletedTweets) Version() int { return 3 }
func (m *m003CreateDeletedTweets) Description() string {
	return "Create deleted_tweets table for audit logging."
}

func (m *m003CreateDeletedTweets) Up(ctx context.Context, tx *sql.Tx) error {
	ddl := `
	CREATE TABLE IF NOT EXISTS deleted_tweets (
		tweet_id INTEGER PRIMARY KEY,
		text TEXT,
		created_at DATETIME,
		views INTEGER,
		is_response BOOLEAN,
		deleted_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
	);`
	_, err := tx.ExecContext(ctx, ddl)
	return err
}

// m004RenameViewsToEngagement renames views column to engagement_score in deleted_tweets.
type m004RenameViewsToEngagement struct{}

func (m *m004RenameViewsToEngagement) Version() int { return 4 }
func (m *m004RenameViewsToEngagement) Description() string {
	return "Rename views column to engagement_score in deleted_tweets table."
}

func (m *m004RenameViewsToEngagement) Up(ctx context.Context, tx *sql.Tx) error {
	cols, err := tableColumns(ctx, tx, "deleted_tweets")
	if err != nil {
		return err
	}

	if cols["views"] && !cols["engagement_score"] {
		_, err = tx.ExecContext(ctx, `ALTER TABLE deleted_tweets RENAME COLUMN views TO engagement_score;`)
		if err != nil {
			// Engine fallback: recreate table
			fallbackDDL := `
			ALTER TABLE deleted_tweets RENAME TO deleted_tweets_old;
			CREATE TABLE deleted_tweets (
				tweet_id INTEGER PRIMARY KEY,
				text TEXT,
				created_at DATETIME,
				engagement_score INTEGER,
				is_response BOOLEAN,
				deleted_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
			);
			INSERT INTO deleted_tweets (tweet_id, text, created_at, engagement_score, is_response, deleted_at)
			SELECT tweet_id, text, created_at, views, is_response, deleted_at FROM deleted_tweets_old;
			DROP TABLE deleted_tweets_old;`
			if _, fallbackErr := tx.ExecContext(ctx, fallbackDDL); fallbackErr != nil {
				return fmt.Errorf("rename column views fallback failed: %w (original: %v)", fallbackErr, err)
			}
		}
	}
	return nil
}
