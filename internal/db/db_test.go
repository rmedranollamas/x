package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rmedranollamas/x-agent/internal/db"
)

// setupTestDB creates a temporary database with migrations applied.
func setupTestDB(t *testing.T) (*db.DBManager, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	mgr, err := db.NewDBManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create db manager: %v", err)
	}

	if err := mgr.RunMigrations(); err != nil {
		mgr.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	return mgr, dbPath
}

// 1. Test clean database migration from version 0 to 4
func TestMigrations_FreshDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fresh.db")

	mgr, err := db.NewDBManager(dbPath)
	if err != nil {
		t.Fatalf("NewDBManager failed: %v", err)
	}
	defer mgr.Close()

	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	ctx := context.Background()
	versions, err := mgr.GetAppliedVersions(ctx)
	if err != nil {
		t.Fatalf("GetAppliedVersions failed: %v", err)
	}

	if len(versions) != 4 {
		t.Fatalf("expected 4 applied migrations, got %d", len(versions))
	}

	expectedDescriptions := map[int]string{
		1: "Initial schema with insights, blocked_users, and followers tables.",
		2: "Add listed_count column to insights table.",
		3: "Create deleted_tweets table for audit logging.",
		4: "Rename views column to engagement_score in deleted_tweets table.",
	}

	for _, v := range versions {
		desc, ok := expectedDescriptions[v.Version]
		if !ok {
			t.Errorf("unexpected migration version: %d", v.Version)
		} else if v.Description != desc {
			t.Errorf("version %d description mismatch: expected %q, got %q", v.Version, desc, v.Description)
		}
		if v.AppliedAt.IsZero() {
			t.Errorf("version %d applied_at is zero", v.Version)
		}
	}

	// Verify all 7 tables exist
	expectedTables := []string{"insights", "blocked_users", "following_users", "followers", "unfollows", "deleted_tweets", "schema_versions"}
	for _, table := range expectedTables {
		var name string
		err := mgr.DB().QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q does not exist: %v", table, err)
		}
	}

	// Verify column listed_count in insights
	var listedCountCol string
	err = mgr.DB().QueryRowContext(ctx, "SELECT name FROM pragma_table_info('insights') WHERE name='listed_count'").Scan(&listedCountCol)
	if err != nil {
		t.Errorf("column listed_count missing in insights: %v", err)
	}

	// Verify column engagement_score in deleted_tweets, and views does NOT exist
	var engScoreCol string
	err = mgr.DB().QueryRowContext(ctx, "SELECT name FROM pragma_table_info('deleted_tweets') WHERE name='engagement_score'").Scan(&engScoreCol)
	if err != nil {
		t.Errorf("column engagement_score missing in deleted_tweets: %v", err)
	}

	var viewsCol string
	err = mgr.DB().QueryRowContext(ctx, "SELECT name FROM pragma_table_info('deleted_tweets') WHERE name='views'").Scan(&viewsCol)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("column views should have been renamed: %v", err)
	}
}

// 2. Test migration idempotency (0 backups, 0 errors, 0 changes on second run)
func TestMigrations_Idempotency(t *testing.T) {
	mgr, dbPath := setupTestDB(t)
	defer mgr.Close()

	backupDir := filepath.Join(filepath.Dir(dbPath), "backups")
	initialBackups, _ := os.ReadDir(backupDir)

	// Run migrations again
	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("second RunMigrations failed: %v", err)
	}

	// Verify no new backups were created
	secondBackups, _ := os.ReadDir(backupDir)
	if len(secondBackups) != len(initialBackups) {
		t.Fatalf("expected %d backups, found %d", len(initialBackups), len(secondBackups))
	}

	// Verify schema_versions count remains 4
	ctx := context.Background()
	versions, err := mgr.GetAppliedVersions(ctx)
	if err != nil {
		t.Fatalf("GetAppliedVersions failed: %v", err)
	}
	if len(versions) != 4 {
		t.Fatalf("expected 4 applied migrations, got %d", len(versions))
	}
}

// 3. Test legacy schema upgrade (m001 unblocked_at -> status = 'UNBLOCKED' and null updated_at backfill)
func TestMigrations_LegacyUpgrade(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	// Open raw SQLite database without running Go migrations to create a legacy state
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open raw db: %v", err)
	}

	// Create legacy tables mimicking pre-m001 / legacy system
	legacyDDLs := []string{
		`CREATE TABLE insights (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
			followers INTEGER,
			following INTEGER
		);`,
		`CREATE TABLE blocked_users (
			user_id INTEGER PRIMARY KEY,
			status TEXT DEFAULT 'PENDING',
			unblocked_at DATETIME,
			updated_at DATETIME
		);`,
	}
	for _, ddl := range legacyDDLs {
		if _, err := rawDB.Exec(ddl); err != nil {
			rawDB.Close()
			t.Fatalf("failed to execute legacy DDL: %v", err)
		}
	}

	// Insert legacy rows:
	// row 1: unblocked_at is set, status is 'PENDING', updated_at is NULL
	// row 2: unblocked_at is NULL, status is 'PENDING', updated_at is NULL
	_, err = rawDB.Exec(`INSERT INTO blocked_users (user_id, status, unblocked_at, updated_at) VALUES (1001, 'PENDING', '2023-01-01 12:00:00', NULL)`)
	if err != nil {
		rawDB.Close()
		t.Fatalf("failed to insert legacy row 1: %v", err)
	}
	_, err = rawDB.Exec(`INSERT INTO blocked_users (user_id, status, unblocked_at, updated_at) VALUES (1002, 'PENDING', NULL, NULL)`)
	if err != nil {
		rawDB.Close()
		t.Fatalf("failed to insert legacy row 2: %v", err)
	}
	rawDB.Close()

	// Now open with DBManager and run migrations
	mgr, err := db.NewDBManager(dbPath)
	if err != nil {
		t.Fatalf("NewDBManager failed on legacy db: %v", err)
	}
	defer mgr.Close()

	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed on legacy upgrade: %v", err)
	}

	ctx := context.Background()

	// Verify row 1 was converted to UNBLOCKED and updated_at was backfilled
	var status1 string
	var updatedAt1 sql.NullString
	err = mgr.DB().QueryRowContext(ctx, "SELECT status, updated_at FROM blocked_users WHERE user_id = 1001").Scan(&status1, &updatedAt1)
	if err != nil {
		t.Fatalf("query row 1 failed: %v", err)
	}
	if status1 != "UNBLOCKED" {
		t.Errorf("expected row 1 status to be 'UNBLOCKED', got %q", status1)
	}
	if !updatedAt1.Valid || updatedAt1.String == "" {
		t.Errorf("expected row 1 updated_at to be backfilled, got null")
	}

	// Verify row 2 remained PENDING and updated_at was backfilled
	var status2 string
	var updatedAt2 sql.NullString
	err = mgr.DB().QueryRowContext(ctx, "SELECT status, updated_at FROM blocked_users WHERE user_id = 1002").Scan(&status2, &updatedAt2)
	if err != nil {
		t.Fatalf("query row 2 failed: %v", err)
	}
	if status2 != "PENDING" {
		t.Errorf("expected row 2 status to be 'PENDING', got %q", status2)
	}
	if !updatedAt2.Valid || updatedAt2.String == "" {
		t.Errorf("expected row 2 updated_at to be backfilled, got null")
	}

	// Verify listed_count added to insights
	var listedCountCol string
	err = mgr.DB().QueryRowContext(ctx, "SELECT name FROM pragma_table_info('insights') WHERE name='listed_count'").Scan(&listedCountCol)
	if err != nil {
		t.Errorf("column listed_count missing in insights after legacy upgrade: %v", err)
	}

	// Verify deleted_tweets created with engagement_score
	var engScoreCol string
	err = mgr.DB().QueryRowContext(ctx, "SELECT name FROM pragma_table_info('deleted_tweets') WHERE name='engagement_score'").Scan(&engScoreCol)
	if err != nil {
		t.Errorf("column engagement_score missing in deleted_tweets after legacy upgrade: %v", err)
	}
}

// 4. Test pre-migration backup trigger when pending migrations exist vs none
func TestMigrations_BackupTrigger(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "backup_trigger.db")

	mgr, err := db.NewDBManager(dbPath)
	if err != nil {
		t.Fatalf("NewDBManager failed: %v", err)
	}
	defer mgr.Close()

	// Seed one table so the DB has content to back up
	_, err = mgr.DB().Exec("CREATE TABLE seed_dummy (id INTEGER PRIMARY KEY);")
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	backupDir := filepath.Join(dir, "backups")
	entriesBefore, _ := os.ReadDir(backupDir)
	if len(entriesBefore) != 0 {
		t.Fatalf("expected 0 backups before migrations, found %d", len(entriesBefore))
	}

	// Run migrations (pending = 4)
	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	entriesAfter, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	if len(entriesAfter) != 1 {
		t.Fatalf("expected exactly 1 backup created, found %d", len(entriesAfter))
	}

	// Run migrations again (pending = 0)
	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("second RunMigrations failed: %v", err)
	}

	entriesAfterSecond, _ := os.ReadDir(backupDir)
	if len(entriesAfterSecond) != 1 {
		t.Fatalf("expected backup count to remain 1, found %d", len(entriesAfterSecond))
	}
}

// 5. Test Insights CRUD, UTC timestamps, and relative offset queries
func TestInsights_CRUD_And_UTC_Offsets(t *testing.T) {
	mgr, _ := setupTestDB(t)
	defer mgr.Close()
	ctx := context.Background()

	// Empty state check
	latest, err := mgr.GetLatestInsight(ctx)
	if err != nil {
		t.Fatalf("GetLatestInsight on empty table failed: %v", err)
	}
	if latest != nil {
		t.Fatalf("expected nil for empty table, got %+v", latest)
	}

	// Add first insight
	if err := mgr.AddInsight(ctx, 100, 50, 10, 2); err != nil {
		t.Fatalf("AddInsight failed: %v", err)
	}

	latest, err = mgr.GetLatestInsight(ctx)
	if err != nil {
		t.Fatalf("GetLatestInsight failed: %v", err)
	}
	if latest == nil {
		t.Fatal("expected insight, got nil")
	}
	if latest.Followers != 100 || latest.Following != 50 || latest.TweetCount != 10 || latest.ListedCount != 2 {
		t.Errorf("insight field mismatch: %+v", latest)
	}
	if latest.Timestamp.IsZero() {
		t.Error("insight timestamp is zero")
	}

	// Add second insight with higher stats
	if err := mgr.AddInsight(ctx, 120, 55, 15, 3); err != nil {
		t.Fatalf("second AddInsight failed: %v", err)
	}

	latest, err = mgr.GetLatestInsight(ctx)
	if err != nil {
		t.Fatalf("GetLatestInsight failed: %v", err)
	}
	if latest.Followers != 120 || latest.Following != 55 {
		t.Errorf("expected latest insight to have followers 120, got %+v", latest)
	}

	// Test GetInsightAtOffset
	offsetInsight, err := mgr.GetInsightAtOffset(ctx, 0)
	if err != nil {
		t.Fatalf("GetInsightAtOffset(0) failed: %v", err)
	}
	if offsetInsight == nil {
		t.Error("expected non-nil insight at offset 0")
	}

	// Query offset far in the past should return nil if no old data exists
	offsetOld, err := mgr.GetInsightAtOffset(ctx, 365)
	if err != nil {
		t.Fatalf("GetInsightAtOffset(365) failed: %v", err)
	}
	if offsetOld != nil {
		t.Errorf("expected nil for offset 365 days ago, got %+v", offsetOld)
	}
}

// 6. Test BlockedUsers Upsert, Conflict handling, and Status transitions
func TestBlockedUsers_Upsert_Conflict(t *testing.T) {
	mgr, _ := setupTestDB(t)
	defer mgr.Close()
	ctx := context.Background()

	// Initial upsert
	userIDs := []int64{101, 102, 103}
	if err := mgr.UpsertBlockedUsers(ctx, userIDs); err != nil {
		t.Fatalf("UpsertBlockedUsers failed: %v", err)
	}

	count, err := mgr.GetAllBlockedUsersCount(ctx)
	if err != nil {
		t.Fatalf("GetAllBlockedUsersCount failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}

	pending, err := mgr.GetPendingBlockedUsers(ctx)
	if err != nil {
		t.Fatalf("GetPendingBlockedUsers failed: %v", err)
	}
	if len(pending) != 3 {
		t.Errorf("expected 3 pending, got %d", len(pending))
	}

	// Update user 101 to UNBLOCKED
	if err := mgr.UpdateBlockedUserStatus(ctx, 101, "UNBLOCKED"); err != nil {
		t.Fatalf("UpdateBlockedUserStatus failed: %v", err)
	}

	pending, err = mgr.GetPendingBlockedUsers(ctx)
	if err != nil {
		t.Fatalf("GetPendingBlockedUsers failed: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending after unblocking 101, got %d", len(pending))
	}

	// Re-upsert user 101 along with new user 104 -> verifies ON CONFLICT resets status to PENDING
	if err := mgr.UpsertBlockedUsers(ctx, []int64{101, 104}); err != nil {
		t.Fatalf("re-upsert failed: %v", err)
	}

	pending, err = mgr.GetPendingBlockedUsers(ctx)
	if err != nil {
		t.Fatalf("GetPendingBlockedUsers failed: %v", err)
	}
	if len(pending) != 4 {
		t.Errorf("expected 4 pending after resetting 101 to PENDING, got %d", len(pending))
	}

	// Batch update 101 and 102 to PROCESSED
	if err := mgr.UpdateBlockedUserStatuses(ctx, []int64{101, 102}, "PROCESSED"); err != nil {
		t.Fatalf("UpdateBlockedUserStatuses failed: %v", err)
	}

	pending, err = mgr.GetPendingBlockedUsers(ctx)
	if err != nil {
		t.Fatalf("GetPendingBlockedUsers failed: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending, got %d", len(pending))
	}

	// Clear pending blocked users
	if err := mgr.ClearPendingBlockedUsers(ctx); err != nil {
		t.Fatalf("ClearPendingBlockedUsers failed: %v", err)
	}

	count, err = mgr.GetAllBlockedUsersCount(ctx)
	if err != nil {
		t.Fatalf("GetAllBlockedUsersCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 users remaining after clearing pending, got %d", count)
	}
}

// 7. Test FollowingUsers Operations
func TestFollowingUsers_Operations(t *testing.T) {
	mgr, _ := setupTestDB(t)
	defer mgr.Close()
	ctx := context.Background()

	// Initial sync
	userIDs := []int64{201, 202, 203}
	if err := mgr.SyncFollowing(ctx, userIDs); err != nil {
		t.Fatalf("SyncFollowing failed: %v", err)
	}

	count, err := mgr.GetAllFollowingUsersCount(ctx)
	if err != nil {
		t.Fatalf("GetAllFollowingUsersCount failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}

	// Test INSERT OR IGNORE idempotency
	if err := mgr.SyncFollowing(ctx, []int64{202, 204}); err != nil {
		t.Fatalf("SyncFollowing with duplicate failed: %v", err)
	}

	count, err = mgr.GetAllFollowingUsersCount(ctx)
	if err != nil {
		t.Fatalf("GetAllFollowingUsersCount failed: %v", err)
	}
	if count != 4 {
		t.Errorf("expected count 4, got %d", count)
	}

	// Processed count should be 0 initially
	processed, err := mgr.GetProcessedFollowingCount(ctx)
	if err != nil {
		t.Fatalf("GetProcessedFollowingCount failed: %v", err)
	}
	if processed != 0 {
		t.Errorf("expected 0 processed, got %d", processed)
	}

	// Update user 201 to PROCESSED
	_, err = mgr.DB().ExecContext(ctx, "UPDATE following_users SET status = 'PROCESSED' WHERE user_id = 201")
	if err != nil {
		t.Fatalf("manual update status failed: %v", err)
	}

	processed, err = mgr.GetProcessedFollowingCount(ctx)
	if err != nil {
		t.Fatalf("GetProcessedFollowingCount failed: %v", err)
	}
	if processed != 1 {
		t.Errorf("expected 1 processed, got %d", processed)
	}

	// Clear pending
	if err := mgr.ClearPendingFollowingUsers(ctx); err != nil {
		t.Fatalf("ClearPendingFollowingUsers failed: %v", err)
	}

	count, err = mgr.GetAllFollowingUsersCount(ctx)
	if err != nil {
		t.Fatalf("GetAllFollowingUsersCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 user left, got %d", count)
	}
}

// 8. Test Followers ReplaceAtomic
func TestFollowers_ReplaceAtomic(t *testing.T) {
	mgr, _ := setupTestDB(t)
	defer mgr.Close()
	ctx := context.Background()

	// Initial replacement
	initial := []int64{301, 302, 303}
	if err := mgr.ReplaceFollowers(ctx, initial); err != nil {
		t.Fatalf("ReplaceFollowers failed: %v", err)
	}

	ids, err := mgr.GetFollowerIDs(ctx)
	if err != nil {
		t.Fatalf("GetFollowerIDs failed: %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 followers, got %d", len(ids))
	}

	set, err := mgr.GetFollowerIDSet(ctx)
	if err != nil {
		t.Fatalf("GetFollowerIDSet failed: %v", err)
	}
	if !set[301] || !set[302] || !set[303] {
		t.Errorf("set missing expected follower IDs: %+v", set)
	}

	// Replace with new snapshot
	next := []int64{401, 402}
	if err := mgr.ReplaceFollowers(ctx, next); err != nil {
		t.Fatalf("second ReplaceFollowers failed: %v", err)
	}

	ids, err = mgr.GetFollowerIDs(ctx)
	if err != nil {
		t.Fatalf("GetFollowerIDs failed: %v", err)
	}
	if len(ids) != 2 || ids[0] != 401 || ids[1] != 402 {
		t.Errorf("expected [401, 402], got %+v", ids)
	}
}

// 9. Test Unfollows Audit Log
func TestUnfollows_AuditLog(t *testing.T) {
	mgr, _ := setupTestDB(t)
	defer mgr.Close()
	ctx := context.Background()

	unfollowedIDs := []int64{501, 502, 503}
	if err := mgr.LogUnfollows(ctx, unfollowedIDs); err != nil {
		t.Fatalf("LogUnfollows failed: %v", err)
	}

	count, err := mgr.GetUnfollowsCount(ctx)
	if err != nil {
		t.Fatalf("GetUnfollowsCount failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}

	recent, err := mgr.GetRecentUnfollows(ctx, 2)
	if err != nil {
		t.Fatalf("GetRecentUnfollows failed: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent unfollows, got %d", len(recent))
	}
	for _, u := range recent {
		if u.Timestamp.IsZero() {
			t.Errorf("unfollow timestamp is zero: %+v", u)
		}
	}
}

// 10. Test DeletedTweets Idempotency, Existence check, and Sets
func TestDeletedTweets_Idempotency(t *testing.T) {
	mgr, _ := setupTestDB(t)
	defer mgr.Close()
	ctx := context.Background()

	// Log tweet
	if err := mgr.LogDeletedTweet(ctx, 1001, "Tweet 1", "2023-01-01", 50, false); err != nil {
		t.Fatalf("LogDeletedTweet failed: %v", err)
	}

	// Log duplicate tweet (INSERT OR IGNORE)
	if err := mgr.LogDeletedTweet(ctx, 1001, "Tweet 1 Duplicate", "2023-01-01", 50, false); err != nil {
		t.Fatalf("duplicate LogDeletedTweet failed: %v", err)
	}

	count, err := mgr.GetDeletedCount(ctx)
	if err != nil {
		t.Fatalf("GetDeletedCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1 due to ignore, got %d", count)
	}

	// Check IsTweetDeleted
	deleted, err := mgr.IsTweetDeleted(ctx, 1001)
	if err != nil || !deleted {
		t.Errorf("expected tweet 1001 to be deleted, err: %v", err)
	}

	notDeleted, err := mgr.IsTweetDeleted(ctx, 9999)
	if err != nil || notDeleted {
		t.Errorf("expected tweet 9999 to not be deleted, err: %v", err)
	}

	// Add another using struct helper
	tweet2 := &db.DeletedTweet{
		TweetID:         1002,
		Text:            "Tweet 2",
		CreatedAt:       "2023-01-02",
		EngagementScore: 10,
		IsResponse:      true,
	}
	if err := mgr.AddDeletedTweet(ctx, tweet2); err != nil {
		t.Fatalf("AddDeletedTweet failed: %v", err)
	}

	set, err := mgr.GetDeletedTweetIDSet(ctx)
	if err != nil {
		t.Fatalf("GetDeletedTweetIDSet failed: %v", err)
	}
	if !set[1001] || !set[1002] {
		t.Errorf("set missing expected deleted tweet IDs: %+v", set)
	}
}

// 11. Test Transaction Rollback on error and on panic
func TestTransaction_Rollback(t *testing.T) {
	mgr, _ := setupTestDB(t)
	defer mgr.Close()
	ctx := context.Background()

	// Rollback on error
	injectedErr := errors.New("simulated error")
	err := mgr.WithTx(ctx, func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(ctx, "INSERT INTO insights (followers, following) VALUES (999, 999)")
		if execErr != nil {
			return execErr
		}
		return injectedErr
	})

	if !errors.Is(err, injectedErr) {
		t.Fatalf("expected injected error, got %v", err)
	}

	count := 0
	_ = mgr.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM insights").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 insights after rollback, found %d", count)
	}

	// Rollback on panic
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic, got nil")
			}
		}()

		_ = mgr.WithTx(ctx, func(tx *sql.Tx) error {
			_, execErr := tx.ExecContext(ctx, "INSERT INTO insights (followers, following) VALUES (888, 888)")
			if execErr != nil {
				return execErr
			}
			panic("simulated panic")
		})
	}()

	count = 0
	_ = mgr.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM insights").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 insights after panic rollback, found %d", count)
	}
}

// 12. Test Concurrency Single Writer Safety (10 concurrent goroutines without SQLITE_BUSY)
func TestConcurrency_SingleWriterSafety(t *testing.T) {
	mgr, _ := setupTestDB(t)
	defer mgr.Close()
	ctx := context.Background()

	const numGoroutines = 10
	const iterationsPerGoroutine = 5

	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*iterationsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterationsPerGoroutine; j++ {
				uid := int64(workerID*1000 + j)
				err := mgr.WithTx(ctx, func(tx *sql.Tx) error {
					_, execErr := tx.ExecContext(ctx, "INSERT INTO blocked_users (user_id, status) VALUES (?, 'PENDING')", uid)
					return execErr
				})
				if err != nil {
					errCh <- fmt.Errorf("worker %d iter %d: %w", workerID, j, err)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrency error: %v", err)
	}

	totalCount, err := mgr.GetAllBlockedUsersCount(ctx)
	if err != nil {
		t.Fatalf("GetAllBlockedUsersCount failed: %v", err)
	}
	expectedCount := numGoroutines * iterationsPerGoroutine
	if totalCount != expectedCount {
		t.Errorf("expected %d records, got %d", expectedCount, totalCount)
	}
}

// 13. Test Real Database Compatibility against /home/pi/x/.state/insights.db (safe read copy)
func TestCompatibility_RealDatabase(t *testing.T) {
	ctx := context.Background()
	const prodDBPath = "/home/pi/x/.state/insights.db"

	var targetPath string
	if data, err := os.ReadFile(prodDBPath); err == nil && len(data) > 0 {
		// Production database exists and is readable: copy to isolated temp directory
		targetPath = filepath.Join(t.TempDir(), "insights.db")
		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			t.Fatalf("failed to write test copy of production database: %v", err)
		}
	} else {
		// Fallback for CI or non-Pi environments: seed schema-exact copy
		targetPath = filepath.Join(t.TempDir(), "insights_fallback.db")
		seedProductionSchema(t, targetPath)
	}

	mgr, err := db.NewDBManager(targetPath)
	if err != nil {
		t.Fatalf("NewDBManager failed on production db copy: %v", err)
	}
	defer mgr.Close()

	// Migration should be an idempotent no-op on already-migrated DB
	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed on production db copy: %v", err)
	}

	// Verify read queries
	insight, err := mgr.GetLatestInsight(ctx)
	if err != nil {
		t.Errorf("GetLatestInsight failed: %v", err)
	} else if insight != nil {
		t.Logf("Read real insight: Followers=%d Following=%d Tweets=%d Listed=%d Timestamp=%s",
			insight.Followers, insight.Following, insight.TweetCount, insight.ListedCount, insight.Timestamp)
	}

	followers, err := mgr.GetFollowerIDs(ctx)
	if err != nil {
		t.Errorf("GetFollowerIDs failed: %v", err)
	} else {
		t.Logf("Read %d real followers", len(followers))
	}

	deletedIDs, err := mgr.GetDeletedTweetIDs(ctx)
	if err != nil {
		t.Errorf("GetDeletedTweetIDs failed: %v", err)
	} else {
		t.Logf("Read %d real deleted tweets", len(deletedIDs))
	}

	pendingBlocked, err := mgr.GetPendingBlockedUsers(ctx)
	if err != nil {
		t.Errorf("GetPendingBlockedUsers failed: %v", err)
	} else {
		t.Logf("Read %d pending blocked users", len(pendingBlocked))
	}

	// Verify backup on non-existent file returns ("", nil)
	nonExistentMgr, err := db.NewDBManager(filepath.Join(t.TempDir(), "nonexistent_sub", "missing.db"))
	if err == nil {
		defer nonExistentMgr.Close()
		// Remove file if created by Ping
		_ = os.Remove(nonExistentMgr.DBPath())
		backupPath, err := nonExistentMgr.BackupDatabase()
		if err != nil {
			t.Errorf("expected nil error on missing db backup, got %v", err)
		}
		if backupPath != "" {
			t.Errorf("expected empty string backupPath on missing db, got %q", backupPath)
		}
	}
}

// seedProductionSchema creates a test database identical to production schema at version 4.
func seedProductionSchema(t *testing.T, dbPath string) {
	t.Helper()
	mgr, err := db.NewDBManager(dbPath)
	if err != nil {
		t.Fatalf("seed NewDBManager failed: %v", err)
	}
	defer mgr.Close()

	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("seed RunMigrations failed: %v", err)
	}

	ctx := context.Background()
	_ = mgr.AddInsight(ctx, 150, 75, 42, 5)
	_ = mgr.ReplaceFollowers(ctx, []int64{8001, 8002})
	_ = mgr.LogDeletedTweet(ctx, 9001, "Seeded tweet", "2023-01-01", 100, false)
}
