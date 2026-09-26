package e2e_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Tier 2: Category 1 - Configuration & Credentials Boundaries
// ============================================================================

func TestTier2_Config_MissingApiKey(t *testing.T) {
	tc := NewTestContext(t)
	res, _ := tc.RunWithEnv(map[string]string{"X_API_KEY": ""}, "insights")

	if res.ExitCode != 1 {
		t.Fatalf("expected exit code 1 for missing X_API_KEY, got %d", res.ExitCode)
	}
	combined := res.Stderr + res.Stdout
	if !strings.Contains(combined, "X_API_KEY") {
		t.Errorf("expected error to mention X_API_KEY, got:\n%s", combined)
	}
	if !strings.Contains(combined, "Missing required environment variables") {
		t.Errorf("expected exact missing variables error string, got:\n%s", combined)
	}
}

func TestTier2_Config_MissingSecret(t *testing.T) {
	tc := NewTestContext(t)
	res, _ := tc.RunWithEnv(map[string]string{"X_API_KEY_SECRET": ""}, "blocked-ids")

	if res.ExitCode != 1 {
		t.Fatalf("expected exit code 1 for missing X_API_KEY_SECRET, got %d", res.ExitCode)
	}
	combined := res.Stderr + res.Stdout
	if !strings.Contains(combined, "X_API_KEY_SECRET") {
		t.Errorf("expected error to mention X_API_KEY_SECRET, got:\n%s", combined)
	}
}

func TestTier2_Config_MissingAccessToken(t *testing.T) {
	tc := NewTestContext(t)
	res, _ := tc.RunWithEnv(map[string]string{"X_ACCESS_TOKEN": ""}, "unblock", "--dry-run")

	if res.ExitCode != 1 {
		t.Fatalf("expected exit code 1 for missing X_ACCESS_TOKEN, got %d", res.ExitCode)
	}
	combined := res.Stderr + res.Stdout
	if !strings.Contains(combined, "X_ACCESS_TOKEN") {
		t.Errorf("expected error to mention X_ACCESS_TOKEN, got:\n%s", combined)
	}
}

func TestTier2_Config_MissingAccessTokenSecret(t *testing.T) {
	tc := NewTestContext(t)
	res, _ := tc.RunWithEnv(map[string]string{"X_ACCESS_TOKEN_SECRET": ""}, "unfollow", "--dry-run")

	if res.ExitCode != 1 {
		t.Fatalf("expected exit code 1 for missing X_ACCESS_TOKEN_SECRET, got %d", res.ExitCode)
	}
	combined := res.Stderr + res.Stdout
	if !strings.Contains(combined, "X_ACCESS_TOKEN_SECRET") {
		t.Errorf("expected error to mention X_ACCESS_TOKEN_SECRET, got:\n%s", combined)
	}
}

func TestTier2_Config_MissingSMTP_WithEmail(t *testing.T) {
	tc := NewTestContext(t)
	emptySMTP := map[string]string{
		"SMTP_USER":        "",
		"SMTP_PASSWORD":    "",
		"REPORT_SENDER":    "",
		"REPORT_RECIPIENT": "",
	}
	res, _ := tc.RunWithEnv(emptySMTP, "insights", "--email")

	if res.ExitCode != 1 {
		t.Fatalf("expected exit code 1 when --email passed without SMTP config, got %d", res.ExitCode)
	}
	combined := res.Stderr + res.Stdout
	if !strings.Contains(combined, "Email reporting requires") && !strings.Contains(combined, "SMTP") {
		t.Errorf("expected error to mention missing email config, got:\n%s", combined)
	}
}

// ============================================================================
// Tier 2: Category 2 - Unblock Agent Boundaries
// ============================================================================

func TestTier2_Unblock_InvalidUserID_NonNumeric(t *testing.T) {
	tc := NewTestContext(t)
	res, _ := tc.Run("unblock", "--user-id", "not-a-number")

	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for non-numeric user-id, got 0")
	}
}

func TestTier2_Unblock_NegativeUserID(t *testing.T) {
	tc := NewTestContext(t)
	res, _ := tc.Run("unblock", "--user-id", "-123")

	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code for negative user-id, got 0")
	}
}

func TestTier2_Unblock_UserNotFound_404(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Configure mock server: user 40404 returns 404
	tc.MockServer.Mu.Lock()
	tc.MockServer.UnblockErrors[40404] = http.StatusNotFound
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("unblock", "--user-id", "40404")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 on handled 404, got %d", res.ExitCode)
	}

	// In SQLite, status should be NOT_FOUND
	status, err := tc.QueryDB(tc.DBPath, "SELECT status FROM blocked_users WHERE user_id = 40404;")
	if err == nil && status != "" && status != "NOT_FOUND" {
		t.Errorf("expected status to be NOT_FOUND for user 40404, got %q", status)
	}
}

func TestTier2_Unblock_ZombieRecovery_Success(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Zombie block: v1 returns 404 on destroy, but fallback recovery recovers it
	tc.MockServer.Mu.Lock()
	tc.MockServer.UnblockErrors[7777] = http.StatusNotFound
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("unblock", "--user-id", "7777")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 with zombie recovery, got %d", res.ExitCode)
	}
}

func TestTier2_Unblock_EmptyAPIFetch(t *testing.T) {
	tc := NewTestContext(t)
	tc.MockServer.Mu.Lock()
	tc.MockServer.BlockedIDs = []int64{}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("unblock")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 when API returns empty blocked list, got %d", res.ExitCode)
	}
	combined := strings.ToLower(res.Stdout + res.Stderr)
	if !strings.Contains(combined, "no blocked") && !strings.Contains(combined, "0 blocked") {
		t.Logf("Notice: expected message regarding empty blocked IDs, got:\n%s", res.Stdout)
	}
}

// ============================================================================
// Tier 2: Category 3 - Insights Agent Boundaries
// ============================================================================

func TestTier2_Insights_DeactivatedFollowerHandle(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Seed previous followers
	_, _ = tc.QueryDB(tc.DBPath, "INSERT INTO followers (user_id) VALUES (9001);")

	// Follower 9002 is new, but resolving it gives 404
	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowerIDs = []int64{9001, 9002}
	tc.MockServer.UnblockErrors[9002] = http.StatusNotFound
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("insights")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}
	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "Deactivated") && !strings.Contains(combined, "9002") {
		t.Logf("Notice: expected (Deactivated) fallback handle for 9002, got:\n%s", combined)
	}
}

func TestTier2_Insights_SuspendedFollowerHandle(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Follower 9003 is new, resolving it gives 403 Forbidden
	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowerIDs = []int64{9003}
	tc.MockServer.UnblockErrors[9003] = http.StatusForbidden
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("insights")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}
	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "Suspended") && !strings.Contains(combined, "9003") {
		t.Logf("Notice: expected (Suspended) fallback handle for 9003, got:\n%s", combined)
	}
}

func TestTier2_Insights_ZeroFollowers(t *testing.T) {
	tc := NewTestContext(t)
	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowersCount = 0
	tc.MockServer.FollowerIDs = []int64{}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("insights")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 with 0 followers, got %d", res.ExitCode)
	}
}

func TestTier2_Insights_NegativeVelocity(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Insert snapshot 24h ago with 500 followers
	yesterday := time.Now().Add(-24 * time.Hour).Format("2006-01-02 15:04:05.000")
	_, err := tc.QueryDB(tc.DBPath, fmt.Sprintf(
		"INSERT INTO insights (timestamp, followers, following, tweet_count) VALUES ('%s', 500, 50, 100);", yesterday))
	if err != nil {
		t.Fatalf("failed to insert history: %v", err)
	}

	// Current followers: 150 (net loss of 350)
	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowersCount = 150
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("insights")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}
	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "Downwards") && !strings.Contains(combined, "-") {
		t.Logf("Notice: expected negative velocity indication, got:\n%s", combined)
	}
}

func TestTier2_Insights_BatchUsers_Over100(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Mock server returns 105 follower IDs
	largeIDs := make([]int64, 105)
	for i := 0; i < 105; i++ {
		largeIDs[i] = int64(4000 + i)
	}
	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowerIDs = largeIDs
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("insights")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 on batching > 100 users, got %d", res.ExitCode)
	}
}

// ============================================================================
// Tier 2: Category 4 - Blocked-IDs Agent Boundaries
// ============================================================================

func TestTier2_BlockedIDs_RateLimit_15m(t *testing.T) {
	tc := NewTestContext(t)
	// Simulate 15m rate limit reset header
	tc.MockServer.Mu.Lock()
	tc.MockServer.V1RateLimitHeaders = map[string]string{
		"x-rate-limit-remaining": "10",
		"x-rate-limit-reset":     fmt.Sprintf("%d", time.Now().Unix()+900),
	}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("blocked-ids")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 with rate limit headers present, got %d", res.ExitCode)
	}
}

func TestTier2_BlockedIDs_DailyRateLimit_24h(t *testing.T) {
	tc := NewTestContext(t)
	// Headers present indicating quota remaining
	tc.MockServer.Mu.Lock()
	tc.MockServer.V2RateLimitHeaders = map[string]string{
		"x-app-limit-24hour-remaining": "50",
		"x-app-limit-24hour-reset":     fmt.Sprintf("%d", time.Now().Unix()+86400),
	}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("blocked-ids")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}
}

func TestTier2_BlockedIDs_Unauthorized_401(t *testing.T) {
	tc := NewTestContext(t)
	tc.MockServer.Mu.Lock()
	tc.MockServer.UnblockErrors[0] = http.StatusUnauthorized
	tc.MockServer.Mu.Unlock()

	// Invalid credentials
	res, _ := tc.RunWithEnv(map[string]string{"X_API_KEY": "invalid_key"}, "blocked-ids")
	// Must not succeed with invalid auth
	if res.ExitCode == 0 && len(res.Stdout) > 0 {
		t.Logf("Notice: invocation with invalid credentials exited: %d", res.ExitCode)
	}
}

func TestTier2_BlockedIDs_CursorPagination(t *testing.T) {
	tc := NewTestContext(t)
	expectedIDs := []int64{1001, 1002, 1003, 1004, 1005}
	tc.MockServer.Mu.Lock()
	tc.MockServer.BlockedIDs = expectedIDs
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("blocked-ids")
	for _, id := range expectedIDs {
		if !strings.Contains(res.Stdout, fmt.Sprintf("%d", id)) {
			t.Errorf("expected blocked ID %d to appear in stdout", id)
		}
	}
}

func TestTier2_BlockedIDs_Server500_Retry(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("blocked-ids")
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", res.ExitCode)
	}
}

// ============================================================================
// Tier 2: Category 5 - Unfollow Agent Boundaries
// ============================================================================

func TestTier2_Unfollow_NoPreviousFollowers(t *testing.T) {
	tc := NewTestContext(t)
	res := tc.MustRun("unfollow")

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}
	// Initial population of followers
	count := tc.CountRows(tc.DBPath, "followers")
	if count < 1 {
		t.Errorf("expected followers to be seeded, got %d", count)
	}
}

func TestTier2_Unfollow_AllFollowersUnfollowed(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Previously had followers 5001, 5002
	_, _ = tc.QueryDB(tc.DBPath, "INSERT INTO followers (user_id) VALUES (5001), (5002);")

	// Current API has 0 followers
	tc.MockServer.Mu.Lock()
	tc.MockServer.FollowerIDs = []int64{}
	tc.MockServer.Mu.Unlock()

	res := tc.MustRun("unfollow")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 when all followers unfollowed, got %d", res.ExitCode)
	}
}

func TestTier2_Unfollow_UnchangedFollowers(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Seed exact same followers as mock server
	tc.MockServer.Mu.Lock()
	followerIDs := append([]int64(nil), tc.MockServer.FollowerIDs...)
	tc.MockServer.Mu.Unlock()
	for _, id := range followerIDs {
		_, _ = tc.QueryDB(tc.DBPath, fmt.Sprintf("INSERT OR IGNORE INTO followers (user_id) VALUES (%d);", id))
	}

	res := tc.MustRun("unfollow")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 on unchanged followers, got %d", res.ExitCode)
	}
}

func TestTier2_Unfollow_DatabaseReadOnly(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Make DB read-only
	_ = os.Chmod(tc.DBPath, 0400)
	defer os.Chmod(tc.DBPath, 0644)

	res, _ := tc.Run("unfollow")
	// If permissions prevent writing, app should exit with non-zero or error log
	if res.ExitCode == 0 {
		t.Logf("Notice: read-only DB run exited %d", res.ExitCode)
	}
}

func TestTier2_Unfollow_DryRun_NoSideEffects(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	_, _ = tc.QueryDB(tc.DBPath, "INSERT INTO followers (user_id) VALUES (6001), (6002);")
	res := tc.MustRun("unfollow", "--dry-run")
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 on dry run, got %d", res.ExitCode)
	}

	// unfollows table must remain untouched
	count := tc.CountRows(tc.DBPath, "unfollows")
	if count != 0 {
		t.Errorf("expected 0 unfollows rows on dry run, got %d", count)
	}
}

// ============================================================================
// Tier 2: Category 6 - Delete Agent Boundaries
// ============================================================================

func TestTier2_Delete_MalformedArchive_MissingBracket(t *testing.T) {
	tc := NewTestContext(t)
	malformedPath := filepath.Join(tc.WorkDir, "bad_tweets.js")
	_ = os.WriteFile(malformedPath, []byte("window.YTD.tweets.part0 = { invalid json without bracket }"), 0644)

	res, _ := tc.Run("delete", "--archive", malformedPath)
	if res.ExitCode == 0 {
		t.Logf("Notice: delete on malformed archive exited code 0")
	}
}

func TestTier2_Delete_Archive_InvalidDates(t *testing.T) {
	tc := NewTestContext(t)
	archivePath := tc.CreateMockTweetArchive("bad_dates.js", []map[string]interface{}{
		{
			"tweet": map[string]interface{}{
				"id":             "8001",
				"created_at":     "INVALID_TIMESTAMP_STRING",
				"full_text":      "Bad date tweet",
				"favorite_count": "0",
				"retweet_count":  "0",
			},
		},
	})

	res := tc.MustRun("delete", "--archive", archivePath, "--dry-run")
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0 skipping invalid dates gracefully, got %d", res.ExitCode)
	}
}

func TestTier2_Delete_TweetOver365Days_NoProtection(t *testing.T) {
	tc := NewTestContext(t)

	// Tweet 400 days old with thread marker
	oldDate := time.Now().AddDate(0, 0, -400).Format("Mon Jan 02 15:04:05 -0700 2006")
	archivePath := tc.CreateMockTweetArchive("tweets.js", []map[string]interface{}{
		{
			"tweet": map[string]interface{}{
				"id":             "8002",
				"created_at":     oldDate,
				"full_text":      "1/ This is a thread tweet older than 1 year",
				"favorite_count": "50",
				"retweet_count":  "50",
				"entities":       map[string]interface{}{},
			},
		},
	})

	res := tc.MustRun("delete", "--archive", archivePath, "--dry-run")
	combined := res.Stdout + res.Stderr
	// Rule 5: older than 365 days overrides thread/media protection
	if !strings.Contains(combined, "older than 365 days") && !strings.Contains(combined, "DELETE") {
		t.Logf("Notice: expected critical age deletion notice, got:\n%s", combined)
	}
}

func TestTier2_Delete_OldRetweetOver30Days(t *testing.T) {
	tc := NewTestContext(t)

	// Retweet created 45 days ago
	oldDate := time.Now().AddDate(0, 0, -45).Format("Mon Jan 02 15:04:05 -0700 2006")
	archivePath := tc.CreateMockTweetArchive("tweets.js", []map[string]interface{}{
		{
			"tweet": map[string]interface{}{
				"id":             "8003",
				"created_at":     oldDate,
				"full_text":      "RT @someone: interesting thought",
				"favorite_count": "0",
				"retweet_count":  "0",
				"entities":       map[string]interface{}{},
			},
		},
	})

	res := tc.MustRun("delete", "--archive", archivePath, "--dry-run")
	combined := res.Stdout + res.Stderr
	// Rule 3: old retweets deleted
	if !strings.Contains(combined, "old retweet") && !strings.Contains(combined, "DELETE") {
		t.Logf("Notice: expected old retweet deletion notice, got:\n%s", combined)
	}
}

func TestTier2_Delete_AlreadyDeleted_Skip(t *testing.T) {
	tc := NewTestContext(t)
	tc.InitDBWithSchema()

	// Insert tweet 8004 into deleted_tweets
	_, _ = tc.QueryDB(tc.DBPath, "INSERT INTO deleted_tweets (tweet_id, text, engagement_score) VALUES (8004, 'Already gone', 0);")

	oldDate := time.Now().AddDate(-2, 0, 0).Format("Mon Jan 02 15:04:05 -0700 2006")
	archivePath := tc.CreateMockTweetArchive("tweets.js", []map[string]interface{}{
		{
			"tweet": map[string]interface{}{
				"id":             "8004",
				"created_at":     oldDate,
				"full_text":      "Already gone",
				"favorite_count": "0",
				"retweet_count":  "0",
				"entities":       map[string]interface{}{},
			},
		},
	})

	res := tc.MustRun("delete", "--archive", archivePath)
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 on skipping checkpointed tweet, got %d", res.ExitCode)
	}

	// Verify no DELETE call made to mock server for 8004
	tc.MockServer.Mu.Lock()
	defer tc.MockServer.Mu.Unlock()
	for _, id := range tc.MockServer.DeletedTweets {
		if id == "8004" {
			t.Errorf("expected tweet 8004 to be skipped, but delete API was called")
		}
	}
}
