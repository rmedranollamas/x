package e2e_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Tier 5: Category 1 - Archive Parsing Edge Cases (internal/agents/delete.go)
// ============================================================================

// TestTier5_Delete_Archive_EmptyArray verifies that DeleteAgent cleanly handles
// an archive containing an empty array `[]` without error or panic.
func TestTier5_Delete_Archive_EmptyArray(t *testing.T) {
	tc := NewTestContext(t)

	// Create archive with empty array
	archivePath := filepath.Join(tc.WorkDir, "empty_archive.js")
	if err := os.WriteFile(archivePath, []byte("window.YTD.tweets.part0 = [ ];"), 0644); err != nil {
		t.Fatalf("failed to create archive: %v", err)
	}

	res := tc.MustRun("delete", "--archive", archivePath, "--dry-run")

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 for empty archive, got %d", res.ExitCode)
	}

	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "Found 0 tweets in archive") {
		t.Errorf("expected output to report 0 tweets found, got:\n%s", combined)
	}
	if !strings.Contains(combined, "Tweets Processed: 0") {
		t.Errorf("expected report to show 0 tweets processed, got:\n%s", combined)
	}
}

// TestTier5_Delete_Archive_CorruptUnicodeAndMalformedSyntax tests resilience against
// unparseable / corrupt JSON, missing brackets, and corrupt date layouts.
func TestTier5_Delete_Archive_CorruptUnicodeAndMalformedSyntax(t *testing.T) {
	tc := NewTestContext(t)

	// Case 1: Missing opening bracket
	malformedPath1 := filepath.Join(tc.WorkDir, "malformed1.js")
	if err := os.WriteFile(malformedPath1, []byte("window.YTD.tweets.part0 = { invalid json }"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	res1 := tc.MustRun("delete", "--archive", malformedPath1, "--dry-run")
	combined1 := res1.Stdout + res1.Stderr
	if !strings.Contains(combined1, "malformed archive content") {
		t.Errorf("expected warning about malformed archive content for missing bracket, got:\n%s", combined1)
	}

	// Case 2: Corrupt Unicode / Truncated JSON
	malformedPath2 := filepath.Join(tc.WorkDir, "malformed2.js")
	if err := os.WriteFile(malformedPath2, []byte("window.YTD.tweets.part0 = [ {\"tweet\": {\"id\": 123, \"full_text\": \"bad \xed\xa0\x80\" }"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	res2 := tc.MustRun("delete", "--archive", malformedPath2, "--dry-run")
	combined2 := res2.Stdout + res2.Stderr
	if !strings.Contains(combined2, "Failed to process archive") && !strings.Contains(combined2, "malformed archive content") {
		t.Errorf("expected failure message when unmarshaling corrupt JSON, got:\n%s", combined2)
	}

	// Case 3: Invalid date format inside a tweet entry (should skip corrupted entry without aborting agent)
	validAndCorrupt := filepath.Join(tc.WorkDir, "mixed_date.js")
	oldValidDate := time.Now().AddDate(-2, 0, 0).Format("Mon Jan 02 15:04:05 -0700 2006")
	archiveContent := fmt.Sprintf(`window.YTD.tweets.part0 = [
		{"tweet": {"id": "111111", "created_at": "invalid-unparseable-date", "full_text": "corrupt date tweet"}},
		{"tweet": {"id": "222222", "created_at": "%s", "full_text": "valid date old tweet", "favorite_count": "0", "retweet_count": "0"}}
	];`, oldValidDate)
	if err := os.WriteFile(validAndCorrupt, []byte(archiveContent), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	res3 := tc.MustRun("delete", "--archive", validAndCorrupt, "--dry-run")
	combined3 := res3.Stdout + res3.Stderr
	if !strings.Contains(combined3, "invalid date") {
		t.Errorf("expected skip log for invalid date, got:\n%s", combined3)
	}
	if !strings.Contains(combined3, "222222") {
		t.Errorf("expected valid tweet 222222 to be processed, got:\n%s", combined3)
	}
}

// TestTier5_Delete_Archive_NumberRepresentations verifies flexibleID and flexibleInt
// handling quoted string IDs, unquoted integer IDs, and engagement values.
func TestTier5_Delete_Archive_NumberRepresentations(t *testing.T) {
	tc := NewTestContext(t)

	oldDate := time.Now().AddDate(-2, 0, 0).Format("Mon Jan 02 15:04:05 -0700 2006")
	content := fmt.Sprintf(`window.YTD.tweets.part0 = [
		{"tweet": {"id": 987654321012345678, "created_at": "%s", "full_text": "unquoted int64 id", "favorite_count": 0, "retweet_count": 0}},
		{"tweet": {"id": "987654321012345679", "created_at": "%s", "full_text": "quoted string id", "favorite_count": "1", "retweet_count": "2"}},
		{"tweet": {"id": "987654321012345680", "created_at": "%s", "full_text": "null counts tweet", "favorite_count": null, "retweet_count": null}}
	];`, oldDate, oldDate, oldDate)

	filePath := filepath.Join(tc.WorkDir, "flexible_numbers.js")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write archive: %v", err)
	}

	res := tc.MustRun("delete", "--archive", filePath, "--dry-run")
	combined := res.Stdout + res.Stderr

	if !strings.Contains(combined, "987654321012345678") {
		t.Errorf("expected tweet 987654321012345678 to be processed, got:\n%s", combined)
	}
	if !strings.Contains(combined, "987654321012345679") {
		t.Errorf("expected tweet 987654321012345679 to be processed, got:\n%s", combined)
	}
	if !strings.Contains(combined, "987654321012345680") {
		t.Errorf("expected tweet 987654321012345680 to be processed, got:\n%s", combined)
	}
}

// TestTier5_Delete_Archive_MaxDeleteFlag tests that --max-delete properly limits deletions.
func TestTier5_Delete_Archive_MaxDeleteFlag(t *testing.T) {
	tc := NewTestContext(t)

	oldDate := time.Now().AddDate(-2, 0, 0).Format("Mon Jan 02 15:04:05 -0700 2006")
	var tweets []map[string]interface{}
	for i := 1; i <= 5; i++ {
		tweets = append(tweets, map[string]interface{}{
			"tweet": map[string]interface{}{
				"id":             strconv.Itoa(50000 + i),
				"created_at":     oldDate,
				"full_text":      fmt.Sprintf("Old low engagement tweet %d", i),
				"favorite_count": "0",
				"retweet_count":  "0",
			},
		})
	}
	archivePath := tc.CreateMockTweetArchive("max_delete_test.js", tweets)

	res := tc.MustRun("delete", "--archive", archivePath, "--max-delete", "2", "--dry-run")
	combined := res.Stdout + res.Stderr

	if !strings.Contains(combined, "Reached max-delete limit of 2 tweets") {
		t.Errorf("expected max-delete limit reached message, got:\n%s", combined)
	}
	if !strings.Contains(combined, "Tweets Deleted:   2") {
		t.Errorf("expected exactly 2 deleted tweets, got:\n%s", combined)
	}
}

// ============================================================================
// Tier 5: Category 2 - Stream Separation Under Error Conditions
// ============================================================================

// TestTier5_StreamSeparation_BlockedIDs_OnSuccess verifies that x-agent blocked-ids
// outputs strictly numeric IDs to STDOUT and all logging / headers to STDERR.
func TestTier5_StreamSeparation_BlockedIDs_OnSuccess(t *testing.T) {
	tc := NewTestContext(t)
	tc.MockServer.Mu.Lock()
	tc.MockServer.BlockedIDs = []int64{1001, 1002, 1003}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("blocked-ids")

	// STDOUT must contain ONLY newline-separated IDs
	stdoutTrimmed := strings.TrimSpace(res.Stdout)
	lines := strings.Split(stdoutTrimmed, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected exactly 3 lines in STDOUT, got %d:\n%s", len(lines), res.Stdout)
	}
	for _, line := range lines {
		id, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64)
		if err != nil {
			t.Errorf("expected purely numeric ID in STDOUT, failed parsing line %q: %v", line, err)
		}
		if id < 1000 {
			t.Errorf("unexpected ID %d in STDOUT", id)
		}
	}

	// STDERR must contain startup header banner and info log
	if !strings.Contains(res.Stderr, "Environment:") {
		t.Errorf("expected startup banner in STDERR, got:\n%s", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "Blocked IDs Agent") {
		t.Errorf("expected info log in STDERR, got:\n%s", res.Stderr)
	}
}

// TestTier5_StreamSeparation_BlockedIDs_OnError verifies that on an API failure,
// STDOUT is 100% clean (0 bytes) and errors are confined to STDERR.
func TestTier5_StreamSeparation_BlockedIDs_OnError(t *testing.T) {
	tc := NewTestContext(t)

	// Inject 500 internal server error on blocks endpoint
	tc.MockServer.Mu.Lock()
	tc.MockServer.V1RateLimitHeaders = map[string]string{}
	tc.MockServer.UnblockErrors[999999] = 500
	tc.MockServer.BlockedIDs = nil
	tc.MockServer.Mu.Unlock()

	// Cause error by pointing base url to non-existent route or failing port
	res, _ := tc.RunWithEnv(map[string]string{"TWITTER_API_BASE_URL": "http://127.0.0.1:9"}, "blocked-ids")

	// STDOUT must have zero bytes
	if len(strings.TrimSpace(res.Stdout)) > 0 {
		t.Errorf("expected 0 bytes in STDOUT on error, got:\n%s", res.Stdout)
	}

	// STDERR must describe the error
	if !strings.Contains(res.Stderr, "Error:") && !strings.Contains(res.Stderr, "Failed") {
		t.Errorf("expected error message in STDERR, got:\n%s", res.Stderr)
	}
}

// TestTier5_StreamSeparation_ConfigError verifies that configuration errors
// output strictly to STDERR with empty STDOUT.
func TestTier5_StreamSeparation_ConfigError(t *testing.T) {
	tc := NewTestContext(t)

	res, _ := tc.RunWithEnv(map[string]string{"X_API_KEY": ""}, "insights")

	if res.ExitCode != 1 {
		t.Fatalf("expected exit code 1 for missing config, got %d", res.ExitCode)
	}
	if len(strings.TrimSpace(res.Stdout)) > 0 {
		t.Errorf("expected empty STDOUT on configuration error, got:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "Configuration Error:") {
		t.Errorf("expected 'Configuration Error:' in STDERR, got:\n%s", res.Stderr)
	}
}

// TestTier5_StreamSeparation_SubcommandInvalidFlag verifies that subcommand validation
// errors write to STDERR without STDOUT pollution.
func TestTier5_StreamSeparation_SubcommandInvalidFlag(t *testing.T) {
	tc := NewTestContext(t)

	res, _ := tc.Run("unblock", "--user-id", "0")

	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for --user-id 0")
	}
	if len(strings.TrimSpace(res.Stdout)) > 0 {
		t.Errorf("expected empty STDOUT on invalid flag, got:\n%s", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "user-id must be a positive integer") {
		t.Errorf("expected validation error in STDERR, got:\n%s", res.Stderr)
	}
}

// ============================================================================
// Tier 5: Category 3 - Unblock Concurrency & Zero-Item Slices
// ============================================================================

// TestTier5_Unblock_ZeroPendingAccounts verifies that UnblockAgent exits cleanly
// with nothing to do when no accounts need unblocking.
func TestTier5_Unblock_ZeroPendingAccounts(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Seed DB with already UNBLOCKED user
	_, err := tc.QueryDB(tc.DBPath, "INSERT INTO blocked_users (user_id, status) VALUES (1001, 'UNBLOCKED');")
	if err != nil {
		t.Fatalf("failed to insert user: %v", err)
	}

	res := tc.MustRun("unblock")
	combined := res.Stdout + res.Stderr

	if !strings.Contains(combined, "Remaining accounts to unblock: 0") &&
		!strings.Contains(combined, "All accounts from the list have been unblocked. Nothing to do!") {
		t.Errorf("expected 'Nothing to do!' message, got:\n%s", combined)
	}
}

// TestTier5_Unblock_SingleUser_NotFoundRecovery verifies zombie block handling
// where unblock returns 404 and is recorded as NOT_FOUND in the database.
func TestTier5_Unblock_SingleUser_NotFoundRecovery(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Configure mock server to return 404 for user 1002
	tc.MockServer.Mu.Lock()
	tc.MockServer.UnblockErrors[1002] = http.StatusNotFound
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("unblock", "--user-id", "1002")
	if res.ExitCode != 0 {
		t.Fatalf("expected unblock to handle 404 gracefully, got exit code %d", res.ExitCode)
	}

	// Verify database record status is NOT_FOUND
	status, err := tc.QueryDB(tc.DBPath, "SELECT status FROM blocked_users WHERE user_id = 1002;")
	if err != nil || status != "NOT_FOUND" {
		t.Errorf("expected status NOT_FOUND in database, got %q, err: %v", status, err)
	}
}

// ============================================================================
// Tier 5: Category 4 - Insights 42-Character Formatting & Calculations
// ============================================================================

// TestTier5_Insights_ZeroFollowers verifies that insights handles an account with
// zero followers, zero following, zero tweets, without divide-by-zero errors.
func TestTier5_Insights_ZeroFollowers(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowersCount = 0
	tc.MockServer.FollowingCount = 0
	tc.MockServer.TweetCount = 0
	tc.MockServer.ListedCount = 0
	tc.MockServer.FollowerIDs = []int64{}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("insights")
	combined := res.Stdout + res.Stderr

	if !strings.Contains(combined, "Followers: 0") {
		t.Errorf("expected 'Followers: 0', got:\n%s", combined)
	}
	if !strings.Contains(combined, "Ratio:     0.00") {
		t.Errorf("expected 'Ratio:     0.00', got:\n%s", combined)
	}
}

// TestTier5_Insights_NegativeGrowth_DownwardsVelocity verifies negative growth
// detection, negative delta formatting, and downward velocity indicator.
func TestTier5_Insights_NegativeGrowth_DownwardsVelocity(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Seed previous insight with 500 followers
	yesterday := time.Now().Add(-24 * time.Hour).Format("2006-01-02 15:04:05.000")
	insertSQL := fmt.Sprintf(
		"INSERT INTO insights (timestamp, followers, following, tweet_count, listed_count) VALUES ('%s', 500, 50, 200, 5);",
		yesterday,
	)
	if _, err := tc.QueryDB(tc.DBPath, insertSQL); err != nil {
		t.Fatalf("failed to seed historical insight: %v", err)
	}

	// Current account has 480 followers (-20 drop)
	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowersCount = 480
	tc.MockServer.FollowingCount = 50
	tc.MockServer.TweetCount = 205
	tc.MockServer.ListedCount = 5
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("insights")
	combined := res.Stdout + res.Stderr

	if !strings.Contains(combined, "(Downwards)") {
		t.Errorf("expected '(Downwards)' velocity indicator, got:\n%s", combined)
	}
	if !strings.Contains(combined, "-20") {
		t.Errorf("expected '-20' follower delta in historical comparison, got:\n%s", combined)
	}
}

// TestTier5_Insights_LargeNumberOverflow verifies proper comma formatting
// for figures >= 1,000,000 and 42-character width integrity.
func TestTier5_Insights_LargeNumberOverflow(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowersCount = 10500000
	tc.MockServer.FollowingCount = 2000000
	tc.MockServer.TweetCount = 1500000
	tc.MockServer.ListedCount = 50000
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("insights")
	combined := res.Stdout + res.Stderr

	expectedStrings := []string{
		"Followers: 10,500,000",
		"Following: 2,000,000",
		"Tweets:    1,500,000",
		"Listed:    50,000",
		"Ratio:     5.25",
	}

	for _, s := range expectedStrings {
		if !strings.Contains(combined, s) {
			t.Errorf("expected formatted string %q in insights report, got:\n%s", s, combined)
		}
	}

	// Verify 42-character divider lines
	expectedDivider := strings.Repeat("=", 42)
	if !strings.Contains(combined, expectedDivider) {
		t.Errorf("expected 42-character '=' divider in report, got:\n%s", combined)
	}
}

// ============================================================================
// Tier 5: Category 5 - Unfollow Baseline & Churn Detection
// ============================================================================

// TestTier5_Unfollow_Baseline_FirstRun_Vs_SubsequentRun verifies that the first run
// establishes a follower baseline and subsequent runs accurately detect unfollows.
func TestTier5_Unfollow_Baseline_FirstRun_Vs_SubsequentRun(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowerIDs = []int64{3001, 3002, 3003, 3004}
	tc.MockServer.Mu.Unlock()

	// 1. First run: baseline initialization
	res1 := tc.MustRun("unfollow")
	combined1 := res1.Stdout + res1.Stderr

	if !strings.Contains(combined1, "No previous follower data found. This is likely the first run.") {
		t.Errorf("expected first run notice, got:\n%s", combined1)
	}
	if !strings.Contains(combined1, "New Followers:   4") {
		t.Errorf("expected 4 new followers, got:\n%s", combined1)
	}
	if !strings.Contains(combined1, "Unfollows:       0") {
		t.Errorf("expected 0 unfollows on initial run, got:\n%s", combined1)
	}

	// Verify DB has exactly 4 follower rows
	count1 := tc.CountRows(tc.DBPath, "followers")
	if count1 != 4 {
		t.Fatalf("expected 4 rows in followers table after run 1, got %d", count1)
	}

	// 2. Churn simulation: user 3001 unfollows
	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowerIDs = []int64{3002, 3003, 3004}
	tc.MockServer.Mu.Unlock()

	res2 := tc.MustRun("unfollow")
	combined2 := res2.Stdout + res2.Stderr

	if !strings.Contains(combined2, "Unfollows:       1") {
		t.Errorf("expected 1 unfollow detected on subsequent run, got:\n%s", combined2)
	}
	if !strings.Contains(combined2, "3001") {
		t.Errorf("expected churned user 3001 in report, got:\n%s", combined2)
	}

	// Verify unfollow event was persisted to SQLite
	unfollowCount := tc.CountRows(tc.DBPath, "unfollows")
	if unfollowCount != 1 {
		t.Errorf("expected 1 row in unfollows table, got %d", unfollowCount)
	}

	unfollowedID, _ := tc.QueryDB(tc.DBPath, "SELECT user_id FROM unfollows LIMIT 1;")
	if unfollowedID != "3001" {
		t.Errorf("expected unfollowed user_id 3001, got %q", unfollowedID)
	}
}

// TestTier5_Unfollow_DryRun_PreservesBaseline verifies that running with --dry-run
// does NOT mutate the followers table, leaving the baseline uncommitted.
func TestTier5_Unfollow_DryRun_PreservesBaseline(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowerIDs = []int64{3001, 3002}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("unfollow", "--dry-run")
	combined := res.Stdout + res.Stderr

	if !strings.Contains(combined, "Would update follower list in database") {
		t.Errorf("expected dry run notice, got:\n%s", combined)
	}

	count := tc.CountRows(tc.DBPath, "followers")
	if count != 0 {
		t.Errorf("expected 0 rows in followers table after dry-run, got %d", count)
	}
}

// TestTier5_Unfollow_LookupFailure_GracefulDegradation verifies that when batch user
// resolution fails with an API error, the unfollow report falls back to raw IDs.
func TestTier5_Unfollow_LookupFailure_GracefulDegradation(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Seed previous followers in DB
	_, err := tc.QueryDB(tc.DBPath, "INSERT INTO followers (user_id) VALUES (4001), (4002);")
	if err != nil {
		t.Fatalf("failed to insert followers: %v", err)
	}

	// Current followers: 4001 left (churned)
	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowerIDs = []int64{4002}
	// Inject error for batch lookup
	tc.MockServer.V2RateLimitHeaders = map[string]string{"x-rate-limit-remaining": "0"}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("unfollow")
	combined := res.Stdout + res.Stderr

	if !strings.Contains(combined, "Unfollows:       1") {
		t.Errorf("expected 1 unfollow in report, got:\n%s", combined)
	}
	if !strings.Contains(combined, "4001") {
		t.Errorf("expected raw user ID 4001 in report even if batch resolution failed, got:\n%s", combined)
	}
}
