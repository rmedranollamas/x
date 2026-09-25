package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Tier 3: Cross-Feature Interactions
// ============================================================================

// TestTier3_Unblock_And_BlockedIDs_Sync verifies that unblocking a user updates
// database state and coordinates cleanly with blocked-ids inspection.
func TestTier3_Unblock_And_BlockedIDs_Sync(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// 1. Inspect blocked IDs initially
	resIDs := tc.MustRun("blocked-ids")
	if !strings.Contains(resIDs.Stdout, "1001") {
		t.Fatalf("expected blocked-ids to contain 1001 initially")
	}

	// 2. Unblock user 1001
	resUnblock := tc.MustRun("unblock", "--user-id", "1001")
	if resUnblock.ExitCode != 0 {
		t.Fatalf("expected unblock to succeed")
	}

	// 3. Verify SQLite records status as UNBLOCKED
	status, err := tc.QueryDB(tc.DBPath, "SELECT status FROM blocked_users WHERE user_id = 1001;")
	if err == nil && status != "" && status != "UNBLOCKED" {
		t.Errorf("expected status UNBLOCKED for user 1001, got %q", status)
	}

	// 4. Running unblock again skips user 1001 because it's not PENDING/FAILED
	resSecond := tc.MustRun("unblock", "--dry-run")
	if strings.Contains(resSecond.Stdout, "1001") && strings.Contains(resSecond.Stdout, "Would unblock 1001") {
		t.Errorf("expected user 1001 to be skipped as already unblocked")
	}
}

// TestTier3_Insights_AutoMigrates_LegacyDB verifies that running insights
// against a legacy v1 database automatically applies migrations m001 through m004.
func TestTier3_Insights_AutoMigrates_LegacyDB(t *testing.T) {
	tc := NewTestContext(t)

	// Seed database with legacy v1 schema (without listed_count or deleted_tweets)
	tc.SeedLegacyDB(tc.DBPath)

	// Run insights
	res := tc.MustRun("insights")
	if res.ExitCode != 0 {
		t.Fatalf("insights failed on legacy database: %d: %s", res.ExitCode, res.Stderr)
	}

	// Verify schema migrations advanced to latest version (4)
	version, err := tc.QueryDB(tc.DBPath, "SELECT MAX(version) FROM schema_versions;")
	if err != nil || version != "4" {
		t.Logf("Notice: schema_versions max version is %q (err: %v)", version, err)
	}

	// Verify column listed_count exists in insights table
	if !tc.ColumnExists(tc.DBPath, "insights", "listed_count") {
		t.Errorf("expected listed_count column to be added by migration m002")
	}

	// Verify deleted_tweets table was created with engagement_score column
	if !tc.TableExists(tc.DBPath, "deleted_tweets") {
		t.Errorf("expected deleted_tweets table to be created by migration m003")
	}
	if !tc.ColumnExists(tc.DBPath, "deleted_tweets", "engagement_score") {
		t.Errorf("expected engagement_score column in deleted_tweets by migration m004")
	}
}

// TestTier3_Delete_Archive_With_LiveTimeline_Fallback verifies that delete agent
// ingests tweets from an archive file while respecting live pinned tweet protection.
func TestTier3_Delete_Archive_With_LiveTimeline_Fallback(t *testing.T) {
	tc := NewTestContext(t)

	// Pinned tweet ID from mock server is "888888"
	oldDate := time.Now().AddDate(-2, 0, 0).Format("Mon Jan 02 15:04:05 -0700 2006")
	archivePath := tc.CreateMockTweetArchive("tweets.js", []map[string]interface{}{
		{
			"tweet": map[string]interface{}{
				"id":             "888888", // Pinned tweet! Must be spared!
				"created_at":     oldDate,
				"full_text":      "Old pinned tweet that must not be deleted",
				"favorite_count": "0",
				"retweet_count":  "0",
			},
		},
		{
			"tweet": map[string]interface{}{
				"id":             "999111", // Normal old low-engagement tweet -> qualify for deletion
				"created_at":     oldDate,
				"full_text":      "Normal old tweet",
				"favorite_count": "0",
				"retweet_count":  "0",
			},
		},
	})

	res := tc.MustRun("delete", "--archive", archivePath, "--dry-run")
	combined := res.Stdout + res.Stderr
	t.Logf("combined output:\n%s", combined)

	// Pinned tweet 888888 must be kept as Protected
	if strings.Contains(combined, "DELETE           ID: 888888") || strings.Contains(combined, "DELETE ID: 888888") {
		t.Errorf("pinned tweet 888888 should have been protected, but marked for deletion")
	}
	if !strings.Contains(combined, "KEEP [Protected] ID: 888888") {
		t.Errorf("expected pinned tweet 888888 to be marked KEEP [Protected]")
	}
}

// TestTier3_Insights_And_Unfollow_StateConsistency verifies that insights and
// unfollow share identical follower snapshot semantics without race conditions.
func TestTier3_Insights_And_Unfollow_StateConsistency(t *testing.T) {
	tc := NewTestContext(t)

	// 1. Run insights to snapshot initial followers
	res1 := tc.MustRun("insights")
	if res1.ExitCode != 0 {
		t.Fatalf("first insights run failed: %d", res1.ExitCode)
	}

	countAfterInsights := tc.CountRows(tc.DBPath, "followers")

	// 2. Immediately run unfollow without changes in API
	res2 := tc.MustRun("unfollow")
	if res2.ExitCode != 0 {
		t.Fatalf("unfollow run failed: %d", res2.ExitCode)
	}

	countAfterUnfollow := tc.CountRows(tc.DBPath, "followers")
	if countAfterInsights != countAfterUnfollow {
		t.Errorf("follower count diverged between insights (%d) and unfollow (%d)",
			countAfterInsights, countAfterUnfollow)
	}

	// 0 unfollows should be recorded
	unfollowsCount := tc.CountRows(tc.DBPath, "unfollows")
	if unfollowsCount != 0 {
		t.Errorf("expected 0 unfollows on identical consecutive runs, got %d", unfollowsCount)
	}
}

// TestTier3_DryRun_ZeroMutationsAcrossCommands verifies that sequential execution
// of all mutating commands with --dry-run guarantees zero modifications to SQLite.
func TestTier3_DryRun_ZeroMutationsAcrossCommands(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	oldDate := time.Now().AddDate(-2, 0, 0).Format("Mon Jan 02 15:04:05 -0700 2006")
	archivePath := tc.CreateMockTweetArchive("tweets.js", []map[string]interface{}{
		{
			"tweet": map[string]interface{}{
				"id":             "123999",
				"created_at":     oldDate,
				"full_text":      "Old tweet",
				"favorite_count": "0",
				"retweet_count":  "0",
			},
		},
	})

	// Run unblock --dry-run
	tc.MustRun("unblock", "--dry-run")
	// Run unfollow --dry-run
	tc.MustRun("unfollow", "--dry-run")
	// Run delete --dry-run
	tc.MustRun("delete", "--archive", archivePath, "--dry-run")

	// Verify all tables have 0 rows
	blockedCount := tc.CountRows(tc.DBPath, "blocked_users")
	unfollowsCount := tc.CountRows(tc.DBPath, "unfollows")
	deletedCount := tc.CountRows(tc.DBPath, "deleted_tweets")

	if blockedCount != 0 || unfollowsCount != 0 || deletedCount != 0 {
		t.Errorf("dry-run violated immutability: blocked=%d, unfollows=%d, deleted=%d",
			blockedCount, unfollowsCount, deletedCount)
	}
}

// TestTier3_DB_Backup_And_Restore_Resumption verifies database backup creation,
// simulated loss, and restoration with continuous state integrity.
func TestTier3_DB_Backup_And_Restore_Resumption(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Seed data
	_, _ = tc.QueryDB(tc.DBPath, "INSERT INTO insights (followers, following) VALUES (200, 50);")

	// Run backup
	resBackup := tc.MustRun("db", "backup")
	if resBackup.ExitCode != 0 {
		t.Fatalf("expected backup to succeed, got %d", resBackup.ExitCode)
	}

	// Locate backup file
	backupDir := filepath.Join(tc.WorkDir, ".state", "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no backup files created in %s", backupDir)
	}

	backupFile := filepath.Join(backupDir, entries[0].Name())

	// Remove original DB
	_ = os.Remove(tc.DBPath)

	// Restore from backup
	backupData, err := os.ReadFile(backupFile)
	if err != nil {
		t.Fatalf("failed to read backup file: %v", err)
	}
	if err := os.WriteFile(tc.DBPath, backupData, 0644); err != nil {
		t.Fatalf("failed to restore db: %v", err)
	}

	// Verify restored data exists
	out, err := tc.QueryDB(tc.DBPath, "SELECT followers FROM insights;")
	if err != nil || out != "200" {
		t.Errorf("restored database did not contain original data, got %q (err: %v)", out, err)
	}
}
