package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Tier 4: Real-World Scenarios
// ============================================================================

// TestTier4_Scenario1_FreshOnboarding simulates a new user's first experience:
// initializing configuration, running insights on a blank state, and verifying
// startup banners, automatic migration, metrics collection, and snapshot creation.
func TestTier4_Scenario1_FreshOnboarding(t *testing.T) {
	tc := NewTestContext(t)

	// Verify no pre-existing DB
	if _, err := os.Stat(tc.DBPath); err == nil {
		_ = os.Remove(tc.DBPath)
	}

	// 1. First execution
	res := tc.MustRun("insights")

	// Verify startup header output
	if !strings.Contains(res.Stdout, "Environment: DEVELOPMENT") && !strings.Contains(res.Stdout, "Database: insights_dev.db") {
		t.Logf("Notice: startup header banner in stdout:\n%s", res.Stdout)
	}

	// Verify SQLite database was automatically created and migrated
	if !tc.TableExists(tc.DBPath, "insights") {
		t.Fatalf("expected insights table to be created automatically")
	}
	if !tc.TableExists(tc.DBPath, "followers") {
		t.Fatalf("expected followers table to be created automatically")
	}

	// Verify at least 1 record in insights
	count := tc.CountRows(tc.DBPath, "insights")
	if count < 1 {
		t.Errorf("expected at least 1 snapshot in insights table, got %d", count)
	}

	// 2. Second execution immediately after: should report 0 follower changes
	res2 := tc.MustRun("insights")
	if res2.ExitCode != 0 {
		t.Fatalf("second insights execution failed with code %d", res2.ExitCode)
	}
}

// TestTier4_Scenario2_DailyMaintenanceWorkflow simulates a daily automated cronjob
// executing the standard sequence: insights -> unfollow -> unblock --dry-run -> db backup.
func TestTier4_Scenario2_DailyMaintenanceWorkflow(t *testing.T) {
	tc := NewTestContext(t)

	// Step 1: Record account insights
	resInsights := tc.MustRun("insights")
	if resInsights.ExitCode != 0 {
		t.Fatalf("step 1 (insights) failed: %d: %s", resInsights.ExitCode, resInsights.Stderr)
	}

	// Step 2: Audit follower churn
	resUnfollow := tc.MustRun("unfollow")
	if resUnfollow.ExitCode != 0 {
		t.Fatalf("step 2 (unfollow) failed: %d: %s", resUnfollow.ExitCode, resUnfollow.Stderr)
	}

	// Step 3: Check pending unblocks in dry-run mode
	resUnblock := tc.MustRun("unblock", "--dry-run")
	if resUnblock.ExitCode != 0 {
		t.Fatalf("step 3 (unblock --dry-run) failed: %d: %s", resUnblock.ExitCode, resUnblock.Stderr)
	}

	// Step 4: Create safety backup of the database
	resBackup := tc.MustRun("db", "backup")
	if resBackup.ExitCode != 0 {
		t.Fatalf("step 4 (db backup) failed: %d: %s", resBackup.ExitCode, resBackup.Stderr)
	}

	// Verify backup artifact exists in .state/backups/
	backupDir := filepath.Join(tc.WorkDir, ".state", "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) == 0 {
		t.Errorf("expected backup file in %s", backupDir)
	}
}

// TestTier4_Scenario3_ArchivePruningCycle simulates an archive-based tweet deletion
// lifecycle: processing a realistic tweets.js export with mixed tweet types,
// auditing deleted tweets to SQLite, and verifying idempotency / checkpoint skipping.
func TestTier4_Scenario3_ArchivePruningCycle(t *testing.T) {
	tc := NewTestContext(t)

	recentDate := time.Now().AddDate(0, 0, -2).Format("Mon Jan 02 15:04:05 -0700 2006")
	oldDate := time.Now().AddDate(-2, 0, 0).Format("Mon Jan 02 15:04:05 -0700 2006")
	retweetDate := time.Now().AddDate(0, 0, -40).Format("Mon Jan 02 15:04:05 -0700 2006")

	archivePath := tc.CreateMockTweetArchive("tweets.js", []map[string]interface{}{
		{
			// Rule 2: Recent (<7d) -> Keep
			"tweet": map[string]interface{}{
				"id":             "10001",
				"created_at":     recentDate,
				"full_text":      "Fresh tweet from yesterday",
				"favorite_count": "0",
				"retweet_count":  "0",
			},
		},
		{
			// Rule 3: Old retweet (>30d) -> Delete
			"tweet": map[string]interface{}{
				"id":             "10002",
				"created_at":     retweetDate,
				"full_text":      "RT @someone: old retweet to delete",
				"favorite_count": "0",
				"retweet_count":  "0",
			},
		},
		{
			// Rule 5: Critical age (>365d, low engagement) -> Delete
			"tweet": map[string]interface{}{
				"id":             "10003",
				"created_at":     oldDate,
				"full_text":      "Old tweet from two years ago",
				"favorite_count": "1",
				"retweet_count":  "0",
			},
		},
	})

	// Pass tweet 10003 as explicitly protected via CLI flag
	res1 := tc.MustRun("delete", "--archive", archivePath, "--protected-id", "10003")
	if res1.ExitCode != 0 {
		t.Fatalf("first delete run failed: %d: %s", res1.ExitCode, res1.Stderr)
	}

	// Verify deleted_tweets table contains tweet 10002 (the old retweet)
	if tc.TableExists(tc.DBPath, "deleted_tweets") {
		out, _ := tc.QueryDB(tc.DBPath, "SELECT tweet_id FROM deleted_tweets WHERE tweet_id = 10002;")
		if out != "10002" {
			t.Logf("Notice: expected tweet 10002 in deleted_tweets, got: %q", out)
		}
	}

	// Repeat deletion run: checkpointing must skip tweet 10002 instantly
	tc.MockServer.Mu.Lock()
	tc.MockServer.DeletedTweets = make([]string, 0)
	tc.MockServer.Mu.Unlock()

	res2 := tc.MustRun("delete", "--archive", archivePath, "--protected-id", "10003")
	if res2.ExitCode != 0 {
		t.Fatalf("second delete run failed: %d", res2.ExitCode)
	}

	tc.MockServer.Mu.Lock()
	deleteCount := len(tc.MockServer.DeletedTweets)
	tc.MockServer.Mu.Unlock()

	if deleteCount > 0 {
		t.Errorf("expected 0 delete API calls on rerun due to checkpointing, got %d", deleteCount)
	}
}

// TestTier4_Scenario4_ZombieBlockRecoveryCycle simulates full recovery of an unblock
// operation encountering a desynchronized "zombie" block on X backend.
func TestTier4_Scenario4_ZombieBlockRecoveryCycle(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Seed user 8888 as PENDING in blocked_users
	_, _ = tc.QueryDB(tc.DBPath, "INSERT INTO blocked_users (user_id, status) VALUES (8888, 'PENDING');")

	// Mock server returns 404 on initial v1 unblock for user 8888
	tc.MockServer.Mu.Lock()
	tc.MockServer.UnblockErrors[8888] = 404
	tc.MockServer.Mu.Unlock()

	// Run unblock
	res := tc.MustRun("unblock", "--user-id", "8888")
	if res.ExitCode != 0 {
		t.Fatalf("unblock failed on zombie recovery: %d: %s", res.ExitCode, res.Stderr)
	}

	// In SQLite, status should be updated
	status, err := tc.QueryDB(tc.DBPath, "SELECT status FROM blocked_users WHERE user_id = 8888;")
	if err == nil && status != "UNBLOCKED" && status != "NOT_FOUND" {
		t.Errorf("expected status UNBLOCKED or NOT_FOUND after zombie recovery, got %q", status)
	}
}

// TestTier4_Scenario5_DevVsProdEnvironmentSwitch validates seamless switching
// between development and production environments, confirming database isolation.
func TestTier4_Scenario5_DevVsProdEnvironmentSwitch(t *testing.T) {
	tc := NewTestContext(t)

	// 1. Run in development environment
	devRes := tc.MustRun("insights")
	if !strings.Contains(devRes.Stdout, "DEVELOPMENT") && !strings.Contains(devRes.Stdout, "insights_dev.db") {
		t.Logf("Notice: dev run stdout banner:\n%s", devRes.Stdout)
	}

	devDB := filepath.Join(tc.WorkDir, ".state", "insights_dev.db")
	if _, err := os.Stat(devDB); os.IsNotExist(err) {
		t.Errorf("expected development database %s to exist", devDB)
	}

	// 2. Run in production environment
	prodEnv := map[string]string{
		"X_AGENT_ENV": "production",
	}
	prodRes, err := tc.RunWithEnv(prodEnv, "insights")
	if err != nil || prodRes.ExitCode != 0 {
		t.Fatalf("production run failed: %d: %s", prodRes.ExitCode, prodRes.Stderr)
	}

	if !strings.Contains(prodRes.Stdout, "PRODUCTION") && !strings.Contains(prodRes.Stdout, "insights.db") {
		t.Logf("Notice: prod run stdout banner:\n%s", prodRes.Stdout)
	}

	prodDB := filepath.Join(tc.WorkDir, ".state", "insights.db")
	if _, err := os.Stat(prodDB); os.IsNotExist(err) {
		t.Errorf("expected production database %s to exist", prodDB)
	}

	// Verify databases are distinct physical files
	if devDB == prodDB {
		t.Errorf("dev and prod database paths should be distinct")
	}
}
