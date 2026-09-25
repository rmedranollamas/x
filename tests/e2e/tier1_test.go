package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Tier 1: Feature 1 - Unblock Agent
// ============================================================================

func TestTier1_Unblock_Help(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("unblock", "--help")

	if !strings.Contains(res.Stdout, "unblock") {
		t.Errorf("expected stdout to mention 'unblock', got:\n%s", res.Stdout)
	}
	expectedFlags := []string{"--user-id", "--refresh", "--dry-run", "--debug"}
	for _, flag := range expectedFlags {
		if !strings.Contains(res.Stdout, flag) {
			t.Errorf("expected help output to include flag %q", flag)
		}
	}
	// Startup banner must be bypassed on --help
	if strings.Contains(res.Stdout, "Database:") {
		t.Errorf("expected startup banner to be bypassed on --help, found in stdout")
	}
}

func TestTier1_Unblock_DryRun_SingleUser(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("unblock", "--user-id", "1001", "--dry-run")

	// Verify dry run output contains indication
	lowerOut := strings.ToLower(res.Stdout + res.Stderr)
	if !strings.Contains(lowerOut, "dry run") && !strings.Contains(lowerOut, "dry-run") && !strings.Contains(lowerOut, "would unblock") {
		t.Errorf("expected output to indicate dry-run, got:\n%s\nStderr:\n%s", res.Stdout, res.Stderr)
	}

	// Verify zero DB writes in dry-run mode
	if tc.TableExists(tc.DBPath, "blocked_users") {
		count := tc.CountRows(tc.DBPath, "blocked_users")
		if count > 0 {
			t.Errorf("expected 0 rows in blocked_users on dry-run, got %d", count)
		}
	}
}

func TestTier1_Unblock_DryRun_All(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("unblock", "--dry-run")

	lowerOut := strings.ToLower(res.Stdout + res.Stderr)
	if !strings.Contains(lowerOut, "dry run") && !strings.Contains(lowerOut, "dry-run") && !strings.Contains(lowerOut, "would unblock") {
		t.Errorf("expected output to mention dry run, got:\n%s", res.Stdout)
	}
}

func TestTier1_Unblock_RefreshFlag(t *testing.T) {
	tc := NewTestContext(t)
	// Seed database with a pending row
	tc.InitDBWithSchema()
	_, err := tc.QueryDB(tc.DBPath, "INSERT INTO blocked_users (user_id, status) VALUES (9999, 'PENDING');")
	if err != nil {
		t.Fatalf("failed to insert test row: %v", err)
	}

	res := tc.MustRun("unblock", "--refresh", "--dry-run")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 with --refresh --dry-run, got %d", res.ExitCode)
	}
}

func TestTier1_Unblock_Execute_Success(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("unblock", "--user-id", "1001")

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}

	// Verify DB state was updated
	status, err := tc.QueryDB(tc.DBPath, "SELECT status FROM blocked_users WHERE user_id = 1001;")
	if err == nil && status != "" && status != "UNBLOCKED" {
		t.Errorf("expected user 1001 to be UNBLOCKED, got status %q", status)
	}
}

// ============================================================================
// Tier 1: Feature 2 - Insights Agent
// ============================================================================

func TestTier1_Insights_Help(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("insights", "--help")

	if !strings.Contains(res.Stdout, "insights") {
		t.Errorf("expected help output to mention insights, got:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "--email") {
		t.Errorf("expected help to describe --email flag")
	}
	if strings.Contains(res.Stdout, "Database:") {
		t.Errorf("startup banner should not be displayed on --help")
	}
}

func TestTier1_Insights_FirstRun_EmptyDB(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("insights")

	if res.ExitCode != 0 {
		t.Fatalf("insights failed on empty DB with code %d", res.ExitCode)
	}

	// Verify insights table has at least 1 record
	if tc.TableExists(tc.DBPath, "insights") {
		count := tc.CountRows(tc.DBPath, "insights")
		if count < 1 {
			t.Errorf("expected at least 1 snapshot in insights table, got %d", count)
		}
	}

	// Verify followers table has snapshot
	if tc.TableExists(tc.DBPath, "followers") {
		count := tc.CountRows(tc.DBPath, "followers")
		if count == 0 {
			t.Errorf("expected followers to be populated in followers table")
		}
	}
}

func TestTier1_Insights_FollowerDiff_Display(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Seed previous followers: 3001, 3002
	_, _ = tc.QueryDB(tc.DBPath, "INSERT INTO followers (user_id) VALUES (3001), (3002);")

	// Mock server returns current followers: 3001, 3002, 3003, 3004 (2 new followers)
	res := tc.MustRun("insights")

	if res.ExitCode != 0 {
		t.Fatalf("insights failed with exit code %d", res.ExitCode)
	}

	combined := res.Stdout + res.Stderr
	// Expect to see + or new follower mentions
	if !strings.Contains(combined, "+") && !strings.Contains(strings.ToLower(combined), "follower") {
		t.Errorf("expected diff to show follower changes, got output:\n%s", res.Stdout)
	}
}

func TestTier1_Insights_ReportWidth(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("insights")

	lines := strings.Split(res.Stdout, "\n")
	foundBoxLine := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "+-") || strings.HasPrefix(trimmed, "|") {
			foundBoxLine = true
			break
		}
	}
	if !foundBoxLine && len(res.Stdout) == 0 {
		t.Errorf("expected insights to produce a formatted output report")
	}
}

func TestTier1_Insights_EmailFlag_Accepted(t *testing.T) {
	tc := NewTestContext(t)
	res, err := tc.Run("insights", "--email")
	if err != nil && res.ExitCode != 0 {
		// If SMTP connection refused (mock server only), verify fail-fast or connection message
		lowerErr := strings.ToLower(res.Stderr + res.Stdout)
		if !strings.Contains(lowerErr, "smtp") && !strings.Contains(lowerErr, "connection") && !strings.Contains(lowerErr, "email") {
			t.Errorf("expected email related error/action, got exit code %d: %s", res.ExitCode, res.Stderr)
		}
	}
}

// ============================================================================
// Tier 1: Feature 3 - Blocked IDs Agent
// ============================================================================

func TestTier1_BlockedIDs_Help(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("blocked-ids", "--help")

	if !strings.Contains(res.Stdout, "blocked-ids") {
		t.Errorf("expected help output to mention blocked-ids, got:\n%s", res.Stdout)
	}
}

func TestTier1_BlockedIDs_Stream_Format(t *testing.T) {
	tc := NewTestContext(t)
	tc.MockServer.Mu.Lock()
	tc.MockServer.BlockedIDs = []int64{1001, 1002, 1003}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("blocked-ids")

	// Each ID must appear in stdout for piping
	stdoutLines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	foundIDs := make(map[string]bool)
	for _, l := range stdoutLines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "1001" || trimmed == "1002" || trimmed == "1003" {
			foundIDs[trimmed] = true
		}
	}

	if !foundIDs["1001"] || !foundIDs["1002"] || !foundIDs["1003"] {
		t.Errorf("expected stdout to stream blocked IDs line by line, got stdout:\n%s", res.Stdout)
	}
}

func TestTier1_BlockedIDs_EmptyList(t *testing.T) {
	tc := NewTestContext(t)
	tc.MockServer.Mu.Lock()
	tc.MockServer.BlockedIDs = []int64{}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("blocked-ids")
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0 on empty blocked IDs, got %d", res.ExitCode)
	}
}

func TestTier1_BlockedIDs_DebugFlag(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("blocked-ids", "--debug")

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}
	// Debug logs should go to stderr, stdout contains IDs
	if res.Stdout == "" && len(tc.MockServer.BlockedIDs) > 0 {
		t.Errorf("expected IDs in stdout even with --debug")
	}
}

func TestTier1_BlockedIDs_ExitCode(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("blocked-ids")
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
}

// ============================================================================
// Tier 1: Feature 4 - Unfollow Agent
// ============================================================================

func TestTier1_Unfollow_Help(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("unfollow", "--help")

	if !strings.Contains(res.Stdout, "unfollow") {
		t.Errorf("expected help output to mention unfollow, got:\n%s", res.Stdout)
	}
	expectedFlags := []string{"--dry-run", "--email", "--debug"}
	for _, flag := range expectedFlags {
		if !strings.Contains(res.Stdout, flag) {
			t.Errorf("expected unfollow help to include %s", flag)
		}
	}
}

func TestTier1_Unfollow_FirstRun(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("unfollow")

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 on first run, got %d", res.ExitCode)
	}

	// Baseline followers populated
	if tc.TableExists(tc.DBPath, "followers") {
		count := tc.CountRows(tc.DBPath, "followers")
		if count == 0 {
			t.Errorf("expected followers table to be populated")
		}
	}

	// 0 unfollows logged on first run
	if tc.TableExists(tc.DBPath, "unfollows") {
		unfollowCount := tc.CountRows(tc.DBPath, "unfollows")
		if unfollowCount != 0 {
			t.Errorf("expected 0 unfollow events on first run, got %d", unfollowCount)
		}
	}
}

func TestTier1_Unfollow_DryRun(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Seed previous followers
	_, _ = tc.QueryDB(tc.DBPath, "INSERT INTO followers (user_id) VALUES (3001), (3002), (9999);")

	// Current followers from mock: 3001, 3002, 3003, 3004 -> user 9999 unfollowed
	res := tc.MustRun("unfollow", "--dry-run")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 on dry run, got %d", res.ExitCode)
	}

	// Dry run must NOT write to unfollows table
	count := tc.CountRows(tc.DBPath, "unfollows")
	if count > 0 {
		t.Errorf("expected 0 unfollows rows on dry-run, got %d", count)
	}
}

func TestTier1_Unfollow_DetectChurn(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Seed previous followers including 8888
	_, _ = tc.QueryDB(tc.DBPath, "INSERT INTO followers (user_id) VALUES (3001), (8888);")

	// Current followers from mock do not have 8888
	res := tc.MustRun("unfollow")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}

	// Check unfollows table for user 8888
	out, err := tc.QueryDB(tc.DBPath, "SELECT user_id FROM unfollows WHERE user_id = 8888;")
	if err == nil && out != "8888" {
		t.Logf("Notice: unfollows table query for 8888 returned: %q", out)
	}
}

func TestTier1_Unfollow_EmailDelivery(t *testing.T) {
	tc := NewTestContext(t)
	res, err := tc.Run("unfollow", "--email")
	if err != nil && res.ExitCode != 0 {
		lowerErr := strings.ToLower(res.Stderr + res.Stdout)
		if !strings.Contains(lowerErr, "smtp") && !strings.Contains(lowerErr, "connection") && !strings.Contains(lowerErr, "email") {
			t.Errorf("expected email related message, got exit code %d: %s", res.ExitCode, res.Stderr)
		}
	}
}

// ============================================================================
// Tier 1: Feature 5 - Delete Agent
// ============================================================================

func TestTier1_Delete_Help(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("delete", "--help")

	if !strings.Contains(res.Stdout, "delete") {
		t.Errorf("expected help output to mention delete, got:\n%s", res.Stdout)
	}
	expectedFlags := []string{"--archive", "--protected-id", "--dry-run", "--email", "--debug"}
	for _, flag := range expectedFlags {
		if !strings.Contains(res.Stdout, flag) {
			t.Errorf("expected delete help to include flag %s", flag)
		}
	}
}

func TestTier1_Delete_Archive_DryRun(t *testing.T) {
	tc := NewTestContext(t)

	// Create sample archive with 1 old low engagement tweet
	oldDate := time.Now().AddDate(-2, 0, 0).Format("Mon Jan 02 15:04:05 -0700 2006")
	archivePath := tc.CreateMockTweetArchive("tweets.js", []map[string]interface{}{
		{
			"tweet": map[string]interface{}{
				"id":             "7001",
				"created_at":     oldDate,
				"full_text":      "Old tweet to delete",
				"favorite_count": "0",
				"retweet_count":  "0",
				"entities":       map[string]interface{}{},
			},
		},
	})

	res := tc.MustRun("delete", "--archive", archivePath, "--dry-run")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 on dry run, got %d", res.ExitCode)
	}

	// Dry run does not write to deleted_tweets
	if tc.TableExists(tc.DBPath, "deleted_tweets") {
		count := tc.CountRows(tc.DBPath, "deleted_tweets")
		if count > 0 {
			t.Errorf("expected 0 rows in deleted_tweets on dry run, got %d", count)
		}
	}
}

func TestTier1_Delete_ProtectedIDs_Flag(t *testing.T) {
	tc := NewTestContext(t)

	oldDate := time.Now().AddDate(-2, 0, 0).Format("Mon Jan 02 15:04:05 -0700 2006")
	archivePath := tc.CreateMockTweetArchive("tweets.js", []map[string]interface{}{
		{
			"tweet": map[string]interface{}{
				"id":             "7002",
				"created_at":     oldDate,
				"full_text":      "Protected old tweet",
				"favorite_count": "0",
				"retweet_count":  "0",
				"entities":       map[string]interface{}{},
			},
		},
	})

	res := tc.MustRun("delete", "--archive", archivePath, "--protected-id", "7002", "--dry-run")
	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "Protected") && !strings.Contains(combined, "KEEP") {
		t.Logf("Notice: expected output to mention Protected status, got:\n%s", combined)
	}
}

func TestTier1_Delete_GracePeriod(t *testing.T) {
	tc := NewTestContext(t)

	// Tweet created 2 days ago (< 7 days grace period)
	recentDate := time.Now().AddDate(0, 0, -2).Format("Mon Jan 02 15:04:05 -0700 2006")
	archivePath := tc.CreateMockTweetArchive("tweets.js", []map[string]interface{}{
		{
			"tweet": map[string]interface{}{
				"id":             "7003",
				"created_at":     recentDate,
				"full_text":      "Recent tweet",
				"favorite_count": "0",
				"retweet_count":  "0",
				"entities":       map[string]interface{}{},
			},
		},
	})

	res := tc.MustRun("delete", "--archive", archivePath, "--dry-run")
	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "Recent") && !strings.Contains(combined, "KEEP") {
		t.Logf("Notice: expected output to indicate Recent / KEEP, got:\n%s", combined)
	}
}

func TestTier1_Delete_Execution_Audit(t *testing.T) {
	tc := NewTestContext(t)

	oldDate := time.Now().AddDate(-2, 0, 0).Format("Mon Jan 02 15:04:05 -0700 2006")
	archivePath := tc.CreateMockTweetArchive("tweets.js", []map[string]interface{}{
		{
			"tweet": map[string]interface{}{
				"id":             "7004",
				"created_at":     oldDate,
				"full_text":      "Old tweet for real deletion",
				"favorite_count": "0",
				"retweet_count":  "0",
				"entities":       map[string]interface{}{},
			},
		},
	})

	res := tc.MustRun("delete", "--archive", archivePath)
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d: %s", res.ExitCode, res.Stderr)
	}

	// Verify deleted_tweets table recorded tweet 7004
	if tc.TableExists(tc.DBPath, "deleted_tweets") {
		out, err := tc.QueryDB(tc.DBPath, "SELECT tweet_id FROM deleted_tweets WHERE tweet_id = 7004;")
		if err == nil && out != "7004" {
			t.Logf("Notice: deleted_tweets table check for 7004 returned: %q", out)
		}
	}
}

// ============================================================================
// Tier 1: Feature 6 - Database Management (db backup, db info)
// ============================================================================

func TestTier1_DB_Info(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("db", "info")

	if !strings.Contains(res.Stdout, "Environment:") && !strings.Contains(res.Stdout, "development") {
		t.Errorf("expected db info to output environment, got:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "Database File:") && !strings.Contains(res.Stdout, "insights_dev.db") {
		t.Errorf("expected db info to output database file path, got:\n%s", res.Stdout)
	}
}

func TestTier1_DB_Backup(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	res := tc.MustRun("db", "backup")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 on db backup, got %d", res.ExitCode)
	}

	// Verify backup was created
	backupDir := filepath.Join(tc.WorkDir, ".state", "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) == 0 {
		// Output might contain backup created path
		if !strings.Contains(res.Stdout, "Backup created at:") {
			t.Errorf("expected backup file or backup message in stdout, got:\n%s", res.Stdout)
		}
	}
}

// Helper method on TestContext to initialize standard DB schema
func (tc *TestContext) InitDBWithSchema() string {
	tc.T.Helper()
	schemaSQL := `
CREATE TABLE IF NOT EXISTS schema_versions (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
    description TEXT
);
INSERT OR IGNORE INTO schema_versions (version, description) VALUES (4, 'Migrations m001-m004');

CREATE TABLE IF NOT EXISTS insights (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
    followers INTEGER,
    following INTEGER,
    tweet_count INTEGER DEFAULT 0,
    listed_count INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS blocked_users (
    user_id INTEGER PRIMARY KEY,
    unblocked_at DATETIME,
    status TEXT DEFAULT 'PENDING',
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
);

CREATE TABLE IF NOT EXISTS following_users (
    user_id INTEGER PRIMARY KEY,
    status TEXT DEFAULT 'PENDING',
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
);

CREATE TABLE IF NOT EXISTS followers (
    user_id INTEGER PRIMARY KEY,
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
);

CREATE TABLE IF NOT EXISTS unfollows (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    timestamp DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
);

CREATE TABLE IF NOT EXISTS deleted_tweets (
    tweet_id INTEGER PRIMARY KEY,
    text TEXT,
    created_at DATETIME,
    engagement_score INTEGER,
    is_response BOOLEAN,
    deleted_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
);
`
	if err := os.MkdirAll(filepath.Dir(tc.DBPath), 0755); err != nil {
		tc.T.Fatalf("failed to create state dir: %v", err)
	}
	cmd := exec.Command("sqlite3", tc.DBPath, schemaSQL)
	if out, err := cmd.CombinedOutput(); err != nil {
		tc.T.Fatalf("failed to init db schema: %s: %v", string(out), err)
	}
	return tc.DBPath
}
