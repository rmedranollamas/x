package agents_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rmedranollamas/x-agent/internal/agents"
	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/db"
	"github.com/rmedranollamas/x-agent/internal/xapi"
)

// ----------------------------------------------------------------------------
// Mock XClient Implementation
// ----------------------------------------------------------------------------

type mockXClient struct {
	getMeFunc             func(ctx context.Context) (*xapi.User, error)
	getBlockedUserIDsFunc func(ctx context.Context) ([]int64, error)
	unblockUserFunc       func(ctx context.Context, userID int64) (string, error)
	unfollowUserFunc      func(ctx context.Context, userID int64) (string, error)
	getFollowerIDsFunc    func(ctx context.Context) ([]int64, error)
	getFriendIDsFunc      func(ctx context.Context) ([]int64, error)
	getUsersBatchFunc     func(ctx context.Context, userIDs []int64) (map[int64]*xapi.User, error)
	getUserTimelineFunc   func(ctx context.Context, userID int64, count int, maxID int64) ([]*xapi.Tweet, error)
	deleteTweetFunc       func(ctx context.Context, tweetID int64) (bool, error)
}

func (m *mockXClient) GetMe(ctx context.Context) (*xapi.User, error) {
	if m.getMeFunc != nil {
		return m.getMeFunc(ctx)
	}
	return &xapi.User{
		ID:             999999,
		Username:       "testuser",
		FollowersCount: 100,
		FollowingCount: 50,
		TweetCount:     200,
		ListedCount:    5,
		CreatedAt:      time.Now().AddDate(-1, 0, 0),
	}, nil
}

func (m *mockXClient) GetBlockedUserIDs(ctx context.Context) ([]int64, error) {
	if m.getBlockedUserIDsFunc != nil {
		return m.getBlockedUserIDsFunc(ctx)
	}
	return []int64{1001, 1002}, nil
}

func (m *mockXClient) UnblockUser(ctx context.Context, userID int64) (string, error) {
	if m.unblockUserFunc != nil {
		return m.unblockUserFunc(ctx, userID)
	}
	return "UNBLOCKED", nil
}

func (m *mockXClient) UnfollowUser(ctx context.Context, userID int64) (string, error) {
	if m.unfollowUserFunc != nil {
		return m.unfollowUserFunc(ctx, userID)
	}
	return "UNFOLLOWED", nil
}

func (m *mockXClient) GetFollowerIDs(ctx context.Context) ([]int64, error) {
	if m.getFollowerIDsFunc != nil {
		return m.getFollowerIDsFunc(ctx)
	}
	return []int64{2001, 2002}, nil
}

func (m *mockXClient) GetFriendIDs(ctx context.Context) ([]int64, error) {
	if m.getFriendIDsFunc != nil {
		return m.getFriendIDsFunc(ctx)
	}
	return []int64{3001, 3002}, nil
}

func (m *mockXClient) GetUsersBatch(ctx context.Context, userIDs []int64) (map[int64]*xapi.User, error) {
	if m.getUsersBatchFunc != nil {
		return m.getUsersBatchFunc(ctx, userIDs)
	}
	res := make(map[int64]*xapi.User, len(userIDs))
	for _, id := range userIDs {
		res[id] = &xapi.User{ID: id, Username: fmt.Sprintf("user_%d", id)}
	}
	return res, nil
}

func (m *mockXClient) GetUserTimeline(ctx context.Context, userID int64, count int, maxID int64) ([]*xapi.Tweet, error) {
	if m.getUserTimelineFunc != nil {
		return m.getUserTimelineFunc(ctx, userID, count, maxID)
	}
	return nil, nil
}

func (m *mockXClient) DeleteTweet(ctx context.Context, tweetID int64) (bool, error) {
	if m.deleteTweetFunc != nil {
		return m.deleteTweetFunc(ctx, tweetID)
	}
	return true, nil
}

// ----------------------------------------------------------------------------
// Mock DB Implementation
// ----------------------------------------------------------------------------

type mockDB struct {
	mu                   sync.Mutex
	allBlockedCount      int
	pendingBlockedIDs    []int64
	clearPendingCalled   bool
	addedBlockedUsers    []int64
	updatedUserStatuses  map[int64]string
	batchUpdatedStatuses map[string][]int64

	followerIDs      []int64
	replacedFollower []int64
	loggedUnfollows  []int64

	latestInsight  *db.Insight
	offsetInsights map[int]*db.Insight
	addedInsights  []*db.Insight

	deletedTweetIDs map[int64]bool
	loggedDeletes   []int64
}

func newMockDB() *mockDB {
	return &mockDB{
		updatedUserStatuses:  make(map[int64]string),
		batchUpdatedStatuses: make(map[string][]int64),
		offsetInsights:       make(map[int]*db.Insight),
		deletedTweetIDs:      make(map[int64]bool),
	}
}

func (m *mockDB) GetAllBlockedUsersCount(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.allBlockedCount, nil
}

func (m *mockDB) ClearPendingBlockedUsers(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearPendingCalled = true
	m.pendingBlockedIDs = nil
	return nil
}

func (m *mockDB) AddBlockedUsers(ctx context.Context, userIDs []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addedBlockedUsers = append(m.addedBlockedUsers, userIDs...)
	m.pendingBlockedIDs = append(m.pendingBlockedIDs, userIDs...)
	m.allBlockedCount = len(m.pendingBlockedIDs)
	return nil
}

func (m *mockDB) UpsertBlockedUsers(ctx context.Context, userIDs []int64) error {
	return m.AddBlockedUsers(ctx, userIDs)
}

func (m *mockDB) GetPendingBlockedUsers(ctx context.Context) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pendingBlockedIDs, nil
}

func (m *mockDB) UpdateBlockedUserStatus(ctx context.Context, userID int64, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updatedUserStatuses[userID] = status
	return nil
}

func (m *mockDB) UpdateBlockedUserStatuses(ctx context.Context, userIDs []int64, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchUpdatedStatuses[status] = append(m.batchUpdatedStatuses[status], userIDs...)
	for _, id := range userIDs {
		m.updatedUserStatuses[id] = status
	}
	return nil
}

func (m *mockDB) GetFollowerIDs(ctx context.Context) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.followerIDs, nil
}

func (m *mockDB) ReplaceFollowers(ctx context.Context, followerIDs []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replacedFollower = followerIDs
	m.followerIDs = followerIDs
	return nil
}

func (m *mockDB) LogUnfollows(ctx context.Context, userIDs []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loggedUnfollows = append(m.loggedUnfollows, userIDs...)
	return nil
}

func (m *mockDB) GetLatestInsight(ctx context.Context) (*db.Insight, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latestInsight, nil
}

func (m *mockDB) GetInsightAtOffset(ctx context.Context, offsetDays int) (*db.Insight, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.offsetInsights[offsetDays], nil
}

func (m *mockDB) AddInsight(ctx context.Context, followers, following, tweetCount, listedCount int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	insight := &db.Insight{
		Followers:   followers,
		Following:   following,
		TweetCount:  tweetCount,
		ListedCount: listedCount,
		Timestamp:   time.Now(),
	}
	m.addedInsights = append(m.addedInsights, insight)
	m.latestInsight = insight
	return nil
}

func (m *mockDB) GetDeletedTweetIDSet(ctx context.Context) (map[int64]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make(map[int64]bool, len(m.deletedTweetIDs))
	for k, v := range m.deletedTweetIDs {
		res[k] = v
	}
	return res, nil
}

func (m *mockDB) LogDeletedTweet(ctx context.Context, tweetID int64, text string, createdAt string, engagementScore int, isResponse bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loggedDeletes = append(m.loggedDeletes, tweetID)
	m.deletedTweetIDs[tweetID] = true
	return nil
}

// ----------------------------------------------------------------------------
// UnblockAgent Tests
// ----------------------------------------------------------------------------

func TestUnblockAgent_SingleUser_Success(t *testing.T) {
	ctx := context.Background()
	client := &mockXClient{
		unblockUserFunc: func(ctx context.Context, userID int64) (string, error) {
			if userID != 1001 {
				t.Fatalf("expected user ID 1001, got %d", userID)
			}
			return "UNBLOCKED", nil
		},
	}
	mockDatabase := newMockDB()
	userID := int64(1001)

	agent := agents.NewUnblockAgent(client, mockDatabase, agents.UnblockOptions{
		UserID: &userID,
	})

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	if mockDatabase.updatedUserStatuses[1001] != "UNBLOCKED" {
		t.Errorf("expected user 1001 to have status UNBLOCKED, got: %q", mockDatabase.updatedUserStatuses[1001])
	}
}

func TestUnblockAgent_SingleUser_DryRun(t *testing.T) {
	ctx := context.Background()
	apiCalled := false
	client := &mockXClient{
		unblockUserFunc: func(ctx context.Context, userID int64) (string, error) {
			apiCalled = true
			return "UNBLOCKED", nil
		},
	}
	mockDatabase := newMockDB()
	userID := int64(1001)

	agent := agents.NewUnblockAgent(client, mockDatabase, agents.UnblockOptions{
		UserID: &userID,
		DryRun: true,
	})

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if apiCalled {
		t.Errorf("API should not be called in dry run mode")
	}
	if len(mockDatabase.updatedUserStatuses) > 0 {
		t.Errorf("zero DB writes expected in dry run mode")
	}
}

func TestUnblockAgent_SingleUser_NotFound(t *testing.T) {
	ctx := context.Background()
	client := &mockXClient{
		unblockUserFunc: func(ctx context.Context, userID int64) (string, error) {
			return "NOT_FOUND", nil
		},
	}
	mockDatabase := newMockDB()
	userID := int64(40404)

	agent := agents.NewUnblockAgent(client, mockDatabase, agents.UnblockOptions{
		UserID: &userID,
	})

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("expected nil error on handled NOT_FOUND, got: %v", err)
	}

	if mockDatabase.updatedUserStatuses[40404] != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND status in DB, got: %q", mockDatabase.updatedUserStatuses[40404])
	}
}

func TestUnblockAgent_Batch_Success(t *testing.T) {
	ctx := context.Background()
	unblockedMap := make(map[int64]bool)
	client := &mockXClient{
		getBlockedUserIDsFunc: func(ctx context.Context) ([]int64, error) {
			return []int64{101, 102, 103}, nil
		},
		unblockUserFunc: func(ctx context.Context, userID int64) (string, error) {
			unblockedMap[userID] = true
			return "UNBLOCKED", nil
		},
	}
	mockDatabase := newMockDB()

	agent := agents.NewUnblockAgent(client, mockDatabase, agents.UnblockOptions{})
	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(unblockedMap) != 3 {
		t.Errorf("expected 3 unblocked accounts, got: %d", len(unblockedMap))
	}
	if len(mockDatabase.batchUpdatedStatuses["UNBLOCKED"]) != 3 {
		t.Errorf("expected 3 batch updated users as UNBLOCKED")
	}
}

func TestUnblockAgent_Batch_Refresh(t *testing.T) {
	ctx := context.Background()
	client := &mockXClient{
		getBlockedUserIDsFunc: func(ctx context.Context) ([]int64, error) {
			return []int64{501, 502}, nil
		},
	}
	mockDatabase := newMockDB()
	mockDatabase.allBlockedCount = 5

	agent := agents.NewUnblockAgent(client, mockDatabase, agents.UnblockOptions{
		Refresh: true,
	})
	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !mockDatabase.clearPendingCalled {
		t.Errorf("expected ClearPendingBlockedUsers to be called on refresh")
	}
}

// ----------------------------------------------------------------------------
// BlockedIDsAgent Tests
// ----------------------------------------------------------------------------

func TestBlockedIDsAgent_Stream(t *testing.T) {
	ctx := context.Background()
	client := &mockXClient{
		getBlockedUserIDsFunc: func(ctx context.Context) ([]int64, error) {
			return []int64{1001, 1002, 1003}, nil
		},
	}

	var outBuf bytes.Buffer
	agent := agents.NewBlockedIDsAgent(client, &outBuf)

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "1001\n1002\n1003\n"
	if outBuf.String() != expected {
		t.Errorf("expected output %q, got %q", expected, outBuf.String())
	}
}

func TestBlockedIDsAgent_Empty(t *testing.T) {
	ctx := context.Background()
	client := &mockXClient{
		getBlockedUserIDsFunc: func(ctx context.Context) ([]int64, error) {
			return []int64{}, nil
		},
	}

	var outBuf bytes.Buffer
	agent := agents.NewBlockedIDsAgent(client, &outBuf)

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outBuf.String() != "" {
		t.Errorf("expected empty stdout, got %q", outBuf.String())
	}
}

func TestBlockedIDsAgent_APIError(t *testing.T) {
	ctx := context.Background()
	client := &mockXClient{
		getBlockedUserIDsFunc: func(ctx context.Context) ([]int64, error) {
			return nil, errors.New("rate limited")
		},
	}

	var outBuf bytes.Buffer
	agent := agents.NewBlockedIDsAgent(client, &outBuf)

	if err := agent.Run(ctx); err == nil {
		t.Fatalf("expected error on API failure, got nil")
	}
}

// ----------------------------------------------------------------------------
// InsightsAgent Tests
// ----------------------------------------------------------------------------

func TestInsightsAgent_GenerateReport_Formatting(t *testing.T) {
	ctx := context.Background()
	fixedTime := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	accountCreation := fixedTime.AddDate(-2, 0, 0) // 730 days

	client := &mockXClient{
		getMeFunc: func(ctx context.Context) (*xapi.User, error) {
			return &xapi.User{
				ID:             999,
				FollowersCount: 1250,
				FollowingCount: 300,
				TweetCount:     4520,
				ListedCount:    12,
				CreatedAt:      accountCreation,
			}, nil
		},
		getFollowerIDsFunc: func(ctx context.Context) ([]int64, error) {
			return []int64{10, 20, 30}, nil
		},
	}

	mockDatabase := newMockDB()
	mockDatabase.followerIDs = []int64{10, 20} // 30 is new
	mockDatabase.offsetInsights[1] = &db.Insight{
		Followers:   1245,
		Following:   299,
		TweetCount:  4508,
		ListedCount: 11,
		Timestamp:   fixedTime.Add(-24 * time.Hour),
	}

	var outBuf bytes.Buffer
	cfg := &config.Config{Environment: "development"}
	agent := agents.NewInsightsAgent(client, mockDatabase, cfg, agents.InsightsOptions{}, &outBuf)
	agent.SetNowFunc(func() time.Time { return fixedTime })

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	report := agent.Report()

	if !strings.Contains(report, "🚀 X ACCOUNT MASTER INSIGHTS 🚀") {
		t.Errorf("expected header banner in report")
	}
	if !strings.Contains(report, "Followers: 1,250") {
		t.Errorf("expected comma-formatted followers")
	}
	if !strings.Contains(report, "Following: 300") {
		t.Errorf("expected following count")
	}
	if !strings.Contains(report, "Ratio:     4.17") {
		t.Errorf("expected ratio formatted to 2 decimals")
	}
	if !strings.Contains(report, "ACCOUNT VITALITY") {
		t.Errorf("expected vitality section")
	}
	if !strings.Contains(report, "24h Ago   |      +5 |    +12 | +1") {
		t.Errorf("expected 24h comparison row, got:\n%s", report)
	}
	if !strings.Contains(report, "Velocity:") {
		t.Errorf("expected velocity calculation")
	}
}

func TestInsightsAgent_EmailDelivery(t *testing.T) {
	ctx := context.Background()
	client := &mockXClient{}
	mockDatabase := newMockDB()
	emailDelivered := false

	cfg := &config.Config{
		Environment:  "development",
		SMTPUser:     "user",
		SMTPPassword: "pass",
	}

	var outBuf bytes.Buffer
	agent := agents.NewInsightsAgent(client, mockDatabase, cfg, agents.InsightsOptions{
		Email: true,
	}, &outBuf)

	agent.SetEmailSender(func(ctx context.Context, cfg *config.Config, subject, reportText string) error {
		emailDelivered = true
		if !strings.Contains(subject, "DEVELOPMENT") {
			t.Errorf("expected subject to contain environment, got: %s", subject)
		}
		return nil
	})

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !emailDelivered {
		t.Errorf("expected email to be delivered when --email is enabled")
	}
}

// ----------------------------------------------------------------------------
// UnfollowAgent Tests
// ----------------------------------------------------------------------------

func TestUnfollowAgent_DetectChurn(t *testing.T) {
	ctx := context.Background()
	client := &mockXClient{
		getFollowerIDsFunc: func(ctx context.Context) ([]int64, error) {
			// User 3003 gained, user 9999 lost
			return []int64{3001, 3002, 3003}, nil
		},
		getUsersBatchFunc: func(ctx context.Context, userIDs []int64) (map[int64]*xapi.User, error) {
			res := make(map[int64]*xapi.User)
			for _, id := range userIDs {
				res[id] = &xapi.User{ID: id, Username: fmt.Sprintf("churned_%d", id)}
			}
			return res, nil
		},
	}

	mockDatabase := newMockDB()
	mockDatabase.followerIDs = []int64{3001, 3002, 9999}

	var outBuf bytes.Buffer
	agent := agents.NewUnfollowAgent(client, mockDatabase, nil, agents.UnfollowOptions{}, &outBuf)

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	report := agent.Report()
	if !strings.Contains(report, "--- Unfollow Detection Report ---") {
		t.Errorf("expected report header")
	}
	if !strings.Contains(report, "Total Followers: 3") {
		t.Errorf("expected total followers 3")
	}
	if !strings.Contains(report, "New Followers:   1") {
		t.Errorf("expected new followers 1")
	}
	if !strings.Contains(report, "Unfollows:       1") {
		t.Errorf("expected unfollows 1")
	}
	if !strings.Contains(report, "@churned_9999 (ID: 9999)") {
		t.Errorf("expected resolved unfollower handle, got:\n%s", report)
	}

	// Verify logged unfollow
	if len(mockDatabase.loggedUnfollows) != 1 || mockDatabase.loggedUnfollows[0] != 9999 {
		t.Errorf("expected user 9999 in logged unfollows, got: %v", mockDatabase.loggedUnfollows)
	}
}

func TestUnfollowAgent_DryRun(t *testing.T) {
	ctx := context.Background()
	client := &mockXClient{
		getFollowerIDsFunc: func(ctx context.Context) ([]int64, error) {
			return []int64{10}, nil
		},
	}
	mockDatabase := newMockDB()
	mockDatabase.followerIDs = []int64{10, 20} // 20 unfollowed

	var outBuf bytes.Buffer
	agent := agents.NewUnfollowAgent(client, mockDatabase, nil, agents.UnfollowOptions{
		DryRun: true,
	}, &outBuf)

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mockDatabase.loggedUnfollows) > 0 {
		t.Errorf("zero DB writes expected in dry run mode")
	}
	if len(mockDatabase.replacedFollower) > 0 {
		t.Errorf("follower list should not be updated in dry run mode")
	}
}

// ----------------------------------------------------------------------------
// DeleteAgent Tests
// ----------------------------------------------------------------------------

func TestDeleteAgent_Archive_AllRules(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tempDir := t.TempDir()
	archiveFile := filepath.Join(tempDir, "tweets.js")

	recentDate := now.AddDate(0, 0, -2).Format("Mon Jan 02 15:04:05 -0700 2006")
	retweetOldDate := now.AddDate(0, 0, -45).Format("Mon Jan 02 15:04:05 -0700 2006")
	threadOldDate := now.AddDate(0, 0, -400).Format("Mon Jan 02 15:04:05 -0700 2006")
	criticalAgeDate := now.AddDate(0, 0, -400).Format("Mon Jan 02 15:04:05 -0700 2006")
	popularDate := now.AddDate(0, 0, -60).Format("Mon Jan 02 15:04:05 -0700 2006")
	lowEngDate := now.AddDate(0, 0, -60).Format("Mon Jan 02 15:04:05 -0700 2006")

	jsContent := fmt.Sprintf(`window.YTD.tweets.part0 = [
  {"tweet": {"id": "1", "created_at": "%s", "full_text": "Protected tweet", "favorite_count": "0", "retweet_count": "0"}},
  {"tweet": {"id": "2", "created_at": "%s", "full_text": "Recent tweet within grace", "favorite_count": "0", "retweet_count": "0"}},
  {"tweet": {"id": "3", "created_at": "%s", "full_text": "RT @friend: old retweet", "favorite_count": "0", "retweet_count": "0"}},
  {"tweet": {"id": "4", "created_at": "%s", "full_text": "1/ Thread tweet old", "favorite_count": "0", "retweet_count": "0"}},
  {"tweet": {"id": "5", "created_at": "%s", "full_text": "Very old tweet no engagement", "favorite_count": "0", "retweet_count": "0"}},
  {"tweet": {"id": "6", "created_at": "%s", "full_text": "Popular tweet with likes", "favorite_count": "25", "retweet_count": "5"}},
  {"tweet": {"id": "7", "created_at": "%s", "full_text": "Low engagement tweet to delete", "favorite_count": "1", "retweet_count": "0"}}
];`, recentDate, recentDate, retweetOldDate, threadOldDate, criticalAgeDate, popularDate, lowEngDate)

	if err := os.WriteFile(archiveFile, []byte(jsContent), 0644); err != nil {
		t.Fatalf("failed to write archive: %v", err)
	}

	deletedTweets := make(map[int64]bool)
	client := &mockXClient{
		deleteTweetFunc: func(ctx context.Context, tweetID int64) (bool, error) {
			deletedTweets[tweetID] = true
			return true, nil
		},
	}
	mockDatabase := newMockDB()

	agent := agents.NewDeleteAgent(client, mockDatabase, nil, agents.DeleteOptions{
		ArchivePath:  archiveFile,
		ProtectedIDs: []int64{1},
		KeepPinned:   false,
	})
	agent.SetTimeFunc(func() time.Time { return now })
	agent.SetSleepFunc(func(d time.Duration) {})

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats := agent.Stats()
	// Tweet 1: Protected -> KEEP
	// Tweet 2: Grace period -> KEEP
	// Tweet 3: Old retweet -> DELETE
	// Tweet 4: Thread -> KEEP
	// Tweet 5: Critical age -> DELETE
	// Tweet 6: Popular (30 >= 20) -> KEEP
	// Tweet 7: Low engagement (1 < 20) -> DELETE
	// Expected deleted: 3, 5, 7 = 3 tweets
	// Expected skipped: 1, 2, 4, 6 = 4 tweets
	if stats.Deleted != 3 {
		t.Errorf("expected 3 deleted tweets, got: %d", stats.Deleted)
	}
	if stats.Skipped != 4 {
		t.Errorf("expected 4 skipped tweets, got: %d", stats.Skipped)
	}
	if !deletedTweets[3] || !deletedTweets[5] || !deletedTweets[7] {
		t.Errorf("expected tweets 3, 5, 7 to be deleted, got: %v", deletedTweets)
	}
}

func TestDeleteAgent_CheckpointSkip(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tempDir := t.TempDir()
	archiveFile := filepath.Join(tempDir, "tweets.js")

	oldDate := now.AddDate(0, 0, -60).Format("Mon Jan 02 15:04:05 -0700 2006")
	jsContent := fmt.Sprintf(`[{"tweet": {"id": "888", "created_at": "%s", "full_text": "Already deleted", "favorite_count": "0", "retweet_count": "0"}}]`, oldDate)

	_ = os.WriteFile(archiveFile, []byte(jsContent), 0644)

	clientCalled := false
	client := &mockXClient{
		deleteTweetFunc: func(ctx context.Context, tweetID int64) (bool, error) {
			clientCalled = true
			return true, nil
		},
	}
	mockDatabase := newMockDB()
	mockDatabase.deletedTweetIDs[888] = true // Checkpointed

	agent := agents.NewDeleteAgent(client, mockDatabase, nil, agents.DeleteOptions{
		ArchivePath: archiveFile,
	})
	agent.SetTimeFunc(func() time.Time { return now })
	agent.SetSleepFunc(func(d time.Duration) {})

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clientCalled {
		t.Errorf("checkpointed tweet should not invoke DeleteTweet API")
	}
	stats := agent.Stats()
	if stats.Deleted != 1 {
		t.Errorf("checkpointed tweet increments Deleted counter for progress tracking")
	}
}

func TestDeleteAgent_DryRun(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tempDir := t.TempDir()
	archiveFile := filepath.Join(tempDir, "tweets.js")
	oldDate := now.AddDate(0, 0, -60).Format("Mon Jan 02 15:04:05 -0700 2006")
	jsContent := fmt.Sprintf(`[{"tweet": {"id": "999", "created_at": "%s", "full_text": "Low eng", "favorite_count": "0", "retweet_count": "0"}}]`, oldDate)
	_ = os.WriteFile(archiveFile, []byte(jsContent), 0644)

	clientCalled := false
	client := &mockXClient{
		deleteTweetFunc: func(ctx context.Context, tweetID int64) (bool, error) {
			clientCalled = true
			return true, nil
		},
	}
	mockDatabase := newMockDB()

	agent := agents.NewDeleteAgent(client, mockDatabase, nil, agents.DeleteOptions{
		ArchivePath: archiveFile,
		DryRun:      true,
	})
	agent.SetTimeFunc(func() time.Time { return now })

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clientCalled {
		t.Errorf("DeleteTweet API should not be called in dry run")
	}
	if len(mockDatabase.loggedDeletes) > 0 {
		t.Errorf("zero DB writes expected in dry run mode")
	}
}

func TestDeleteAgent_MaxDelete(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tempDir := t.TempDir()
	archiveFile := filepath.Join(tempDir, "tweets.js")
	oldDate := now.AddDate(0, 0, -60).Format("Mon Jan 02 15:04:05 -0700 2006")
	jsContent := fmt.Sprintf(`[
  {"tweet": {"id": "101", "created_at": "%s", "full_text": "Low eng 1", "favorite_count": "0", "retweet_count": "0"}},
  {"tweet": {"id": "102", "created_at": "%s", "full_text": "Low eng 2", "favorite_count": "0", "retweet_count": "0"}},
  {"tweet": {"id": "103", "created_at": "%s", "full_text": "Low eng 3", "favorite_count": "0", "retweet_count": "0"}}
]`, oldDate, oldDate, oldDate)
	_ = os.WriteFile(archiveFile, []byte(jsContent), 0644)

	deletedCount := 0
	client := &mockXClient{
		deleteTweetFunc: func(ctx context.Context, tweetID int64) (bool, error) {
			deletedCount++
			return true, nil
		},
	}
	mockDatabase := newMockDB()

	agent := agents.NewDeleteAgent(client, mockDatabase, nil, agents.DeleteOptions{
		ArchivePath: archiveFile,
		MaxDelete:   2,
	})
	agent.SetTimeFunc(func() time.Time { return now })
	agent.SetSleepFunc(func(d time.Duration) {})

	if err := agent.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deletedCount != 2 {
		t.Errorf("expected exactly 2 deleted tweets with MaxDelete=2, got: %d", deletedCount)
	}
}
