package xapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rmedranollamas/x-agent/internal/config"
	"github.com/rmedranollamas/x-agent/internal/xapi"
)

// MockResponse represents a pre-configured HTTP response in the mock sequence.
type MockResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
	Err        error
}

// MockTransport intercepts HTTP requests in-memory without network sockets.
type MockTransport struct {
	mu          sync.Mutex
	sequences   map[string][]MockResponse
	requests    []*http.Request
	requestData [][]byte
}

// NewMockTransport creates a new MockTransport.
func NewMockTransport() *MockTransport {
	return &MockTransport{
		sequences:   make(map[string][]MockResponse),
		requests:    make([]*http.Request, 0),
		requestData: make([][]byte, 0),
	}
}

// RoundTrip executes the in-memory response queue matching Method and URL.Path.
func (m *MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var bodyCopy []byte
	if req.Body != nil {
		bodyCopy, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyCopy))
	}
	m.requests = append(m.requests, req)
	m.requestData = append(m.requestData, bodyCopy)

	routeKey := fmt.Sprintf("%s %s", req.Method, req.URL.Path)

	if seq, ok := m.sequences[routeKey]; ok && len(seq) > 0 {
		respDef := seq[0]
		m.sequences[routeKey] = seq[1:]
		if respDef.Err != nil {
			return nil, respDef.Err
		}
		hdr := make(http.Header)
		for k, v := range respDef.Headers {
			hdr.Set(k, v)
		}
		if hdr.Get("Content-Type") == "" {
			hdr.Set("Content-Type", "application/json")
		}
		return &http.Response{
			StatusCode: respDef.StatusCode,
			Status:     fmt.Sprintf("%d %s", respDef.StatusCode, http.StatusText(respDef.StatusCode)),
			Header:     hdr,
			Body:       io.NopCloser(bytes.NewReader(respDef.Body)),
			Request:    req,
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusNotFound,
		Status:     "404 Not Found",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"errors":[{"message":"Route not found in mock"}]}`)),
		Request:    req,
	}, nil
}

// QueueResponse adds a response to the sequence queue for a route.
func (m *MockTransport) QueueResponse(method, path string, statusCode int, body interface{}, headers map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	routeKey := fmt.Sprintf("%s %s", method, path)
	var bodyBytes []byte
	if body != nil {
		switch v := body.(type) {
		case []byte:
			bodyBytes = v
		case string:
			bodyBytes = []byte(v)
		default:
			bodyBytes, _ = json.Marshal(v)
		}
	}

	m.sequences[routeKey] = append(m.sequences[routeKey], MockResponse{
		StatusCode: statusCode,
		Headers:    headers,
		Body:       bodyBytes,
	})
}

// QueueError queues a raw network error for a route.
func (m *MockTransport) QueueError(method, path string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	routeKey := fmt.Sprintf("%s %s", method, path)
	m.sequences[routeKey] = append(m.sequences[routeKey], MockResponse{Err: err})
}

// RequestCount returns the number of requests matching method and path.
func (m *MockTransport) RequestCount(method, path string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, r := range m.requests {
		if r.Method == method && r.URL.Path == path {
			count++
		}
	}
	return count
}

func newTestClient(mock *MockTransport, opts ...xapi.Option) *xapi.Client {
	cfg := &config.Config{
		XAPIKey:            "test_api_key",
		XAPIKeySecret:      "test_api_secret",
		XAccessToken:       "test_token",
		XAccessTokenSecret: "test_token_secret",
	}
	defaultSleep := xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
		return nil
	})
	allOpts := append([]xapi.Option{defaultSleep, xapi.WithRoundTripper(mock)}, opts...)
	c, err := xapi.NewClient(cfg, allOpts...)
	if err != nil {
		panic(err)
	}
	return c
}

// =========================================================================
// 1. OAuth 1.0a & Core Client Tests
// =========================================================================

func TestXClient_OAuth1Header(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{
			"id":       "12345",
			"username": "testuser",
		},
	}, nil)

	client := newTestClient(mock)
	_, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(mock.requests))
	}

	authHeader := mock.requests[0].Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "OAuth ") {
		t.Errorf("expected Authorization header to start with 'OAuth ', got: %s", authHeader)
	}
	for _, field := range []string{"oauth_consumer_key", "oauth_nonce", "oauth_signature", "oauth_timestamp"} {
		if !strings.Contains(authHeader, field) {
			t.Errorf("expected Authorization header to contain %q, got: %s", field, authHeader)
		}
	}
}

func TestXClient_GetMe_Success(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{
			"id":              "999888",
			"username":        "superagent",
			"name":            "Super Agent",
			"description":     "Automated account",
			"pinned_tweet_id": "11223344",
			"created_at":      "2021-05-15T12:00:00Z",
			"public_metrics": map[string]interface{}{
				"followers_count": 520,
				"following_count": 180,
				"tweet_count":     1400,
				"listed_count":    15,
			},
		},
	}, nil)

	client := newTestClient(mock)
	user, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe failed: %v", err)
	}

	if user.ID != 999888 {
		t.Errorf("expected ID 999888, got %d", user.ID)
	}
	if user.Username != "superagent" {
		t.Errorf("expected username 'superagent', got %q", user.Username)
	}
	if user.FollowersCount != 520 || user.ListedCount != 15 {
		t.Errorf("metric mismatch: followers=%d listed=%d", user.FollowersCount, user.ListedCount)
	}
	if user.PinnedTweetID != 11223344 {
		t.Errorf("expected pinned tweet ID 11223344, got %d", user.PinnedTweetID)
	}
}

func TestXClient_GetMe_EmptyDataError(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, `{"data": null}`, nil)

	client := newTestClient(mock)
	_, err := client.GetMe(context.Background())
	if err == nil {
		t.Fatalf("expected error on empty data, got nil")
	}
	if !strings.Contains(err.Error(), "no user data returned") {
		t.Errorf("expected 'no user data returned' error message, got: %v", err)
	}
}

// =========================================================================
// 2. Endpoints & Pagination Tests
// =========================================================================

func TestXClient_GetBlockedUserIDs_SinglePage(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/1.1/blocks/ids.json", http.StatusOK, map[string]interface{}{
		"ids":         []int64{101, 102},
		"next_cursor": 0,
	}, nil)

	client := newTestClient(mock)
	ids, err := client.GetBlockedUserIDs(context.Background())
	if err != nil {
		t.Fatalf("GetBlockedUserIDs failed: %v", err)
	}

	if len(ids) != 2 || ids[0] != 101 || ids[1] != 102 {
		t.Errorf("unexpected IDs: %v", ids)
	}
	if mock.RequestCount("GET", "/1.1/blocks/ids.json") != 1 {
		t.Errorf("expected 1 request, got %d", mock.RequestCount("GET", "/1.1/blocks/ids.json"))
	}
}

func TestXClient_GetBlockedUserIDs_MultiPage(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/1.1/blocks/ids.json", http.StatusOK, map[string]interface{}{
		"ids":         []int64{101, 102},
		"next_cursor": 42,
	}, nil)
	mock.QueueResponse("GET", "/1.1/blocks/ids.json", http.StatusOK, map[string]interface{}{
		"ids":         []int64{103, 104},
		"next_cursor": 0,
	}, nil)

	client := newTestClient(mock)
	ids, err := client.GetBlockedUserIDs(context.Background())
	if err != nil {
		t.Fatalf("GetBlockedUserIDs failed: %v", err)
	}

	expected := []int64{101, 102, 103, 104}
	if len(ids) != len(expected) {
		t.Fatalf("expected %d IDs, got %d: %v", len(expected), len(ids), ids)
	}
	for i, v := range expected {
		if ids[i] != v {
			t.Errorf("at index %d: expected %d, got %d", i, v, ids[i])
		}
	}
	if mock.RequestCount("GET", "/1.1/blocks/ids.json") != 2 {
		t.Errorf("expected 2 requests, got %d", mock.RequestCount("GET", "/1.1/blocks/ids.json"))
	}
}

func TestXClient_GetFollowerIDs_Pagination(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/1.1/followers/ids.json", http.StatusOK, map[string]interface{}{
		"ids":         []int64{301},
		"next_cursor": 555,
	}, nil)
	mock.QueueResponse("GET", "/1.1/followers/ids.json", http.StatusOK, map[string]interface{}{
		"ids":         []int64{302},
		"next_cursor": 0,
	}, nil)

	client := newTestClient(mock)
	ids, err := client.GetFollowerIDs(context.Background())
	if err != nil {
		t.Fatalf("GetFollowerIDs failed: %v", err)
	}

	if len(ids) != 2 || ids[0] != 301 || ids[1] != 302 {
		t.Errorf("unexpected IDs: %v", ids)
	}
}

func TestXClient_GetFriendIDs_Pagination(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/1.1/friends/ids.json", http.StatusOK, map[string]interface{}{
		"ids":         []int64{201},
		"next_cursor": 777,
	}, nil)
	mock.QueueResponse("GET", "/1.1/friends/ids.json", http.StatusOK, map[string]interface{}{
		"ids":         []int64{202},
		"next_cursor": 0,
	}, nil)

	client := newTestClient(mock)
	ids, err := client.GetFriendIDs(context.Background())
	if err != nil {
		t.Fatalf("GetFriendIDs failed: %v", err)
	}

	if len(ids) != 2 || ids[0] != 201 || ids[1] != 202 {
		t.Errorf("unexpected IDs: %v", ids)
	}
}

func TestXClient_UnfollowUser_Success(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("POST", "/1.1/friendships/destroy.json", http.StatusOK, `{"id":888}`, nil)

	client := newTestClient(mock)
	status, err := client.UnfollowUser(context.Background(), 888)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "SUCCESS" {
		t.Errorf("expected 'SUCCESS', got %q", status)
	}
}

func TestXClient_UnfollowUser_Failure(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("POST", "/1.1/friendships/destroy.json", http.StatusForbidden, `{"errors":[{"code":403,"message":"Forbidden"}]}`, nil)

	client := newTestClient(mock)
	status, err := client.UnfollowUser(context.Background(), 888)
	if err == nil {
		t.Fatalf("expected non-nil error, got nil")
	}
	if status != "FAILED" {
		t.Errorf("expected status 'FAILED', got %q", status)
	}
}

func TestXClient_UsersBatch_Under100(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users", http.StatusOK, map[string]interface{}{
		"data": []map[string]interface{}{
			{"id": "101", "username": "u1"},
			{"id": "102", "username": "u2"},
		},
	}, nil)

	client := newTestClient(mock)
	result, err := client.GetUsersBatch(context.Background(), []int64{101, 102})
	if err != nil {
		t.Fatalf("GetUsersBatch failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 users returned, got %d", len(result))
	}
	if result[101] == nil || result[101].Username != "u1" {
		t.Errorf("user 101 not mapped correctly")
	}
	if mock.RequestCount("GET", "/2/users") != 1 {
		t.Errorf("expected 1 request, got %d", mock.RequestCount("GET", "/2/users"))
	}
}

func TestXClient_UsersBatch_ChunkingOver100(t *testing.T) {
	mock := NewMockTransport()
	inputIDs := make([]int64, 120)
	for i := 0; i < 120; i++ {
		inputIDs[i] = int64(1000 + i)
	}

	// Chunk 1 (100 IDs)
	chunk1 := make([]map[string]interface{}, 100)
	for i := 0; i < 100; i++ {
		chunk1[i] = map[string]interface{}{
			"id":       fmt.Sprintf("%d", inputIDs[i]),
			"username": fmt.Sprintf("user_%d", inputIDs[i]),
		}
	}
	mock.QueueResponse("GET", "/2/users", http.StatusOK, map[string]interface{}{"data": chunk1}, nil)

	// Chunk 2 (20 IDs)
	chunk2 := make([]map[string]interface{}, 20)
	for i := 0; i < 20; i++ {
		chunk2[i] = map[string]interface{}{
			"id":       fmt.Sprintf("%d", inputIDs[100+i]),
			"username": fmt.Sprintf("user_%d", inputIDs[100+i]),
		}
	}
	mock.QueueResponse("GET", "/2/users", http.StatusOK, map[string]interface{}{"data": chunk2}, nil)

	client := newTestClient(mock)
	result, err := client.GetUsersBatch(context.Background(), inputIDs)
	if err != nil {
		t.Fatalf("GetUsersBatch failed: %v", err)
	}

	if len(result) != 120 {
		t.Errorf("expected 120 users, got %d", len(result))
	}
	if mock.RequestCount("GET", "/2/users") != 2 {
		t.Errorf("expected 2 batch requests, got %d", mock.RequestCount("GET", "/2/users"))
	}
}

func TestXClient_UsersBatch_PartialErrors(t *testing.T) {
	mock := NewMockTransport()
	// Batch response with 1 found user and 1 missing user
	mock.QueueResponse("GET", "/2/users", http.StatusOK, map[string]interface{}{
		"data": []map[string]interface{}{
			{"id": "101", "username": "active_user"},
		},
		"errors": []map[string]interface{}{
			{"parameter": "id", "value": "999", "detail": "Could not find user with id [999]."},
		},
	}, nil)

	// Fallback check for missing user 999 via v1
	mock.QueueResponse("GET", "/1.1/users/show.json", http.StatusNotFound, `{"errors":[{"code":50}]}`, nil)

	client := newTestClient(mock)
	result, err := client.GetUsersBatch(context.Background(), []int64{101, 999})
	if err != nil {
		t.Fatalf("GetUsersBatch failed: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 user entries, got %d", len(result))
	}
	if result[101].Username != "active_user" {
		t.Errorf("expected username 'active_user', got %q", result[101].Username)
	}
	if result[999].Status != "(Deactivated)" {
		t.Errorf("expected status '(Deactivated)', got %q", result[999].Status)
	}
}

func TestXClient_UserTimeline_MaxIDPagination(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/1.1/statuses/user_timeline.json", http.StatusOK, []map[string]interface{}{
		{
			"id":             int64(987654321),
			"id_str":         "987654321",
			"full_text":      "Sample tweet content",
			"created_at":     "Wed Oct 10 20:19:24 +0000 2018",
			"favorite_count": 15,
			"retweet_count":  3,
		},
	}, nil)

	client := newTestClient(mock)
	tweets, err := client.GetUserTimeline(context.Background(), 12345, 200, 999999)
	if err != nil {
		t.Fatalf("GetUserTimeline failed: %v", err)
	}

	if len(tweets) != 1 {
		t.Fatalf("expected 1 tweet, got %d", len(tweets))
	}
	tw := tweets[0]
	if tw.ID != 987654321 {
		t.Errorf("expected ID 987654321, got %d", tw.ID)
	}
	if tw.FullText != "Sample tweet content" {
		t.Errorf("expected full_text 'Sample tweet content', got %q", tw.FullText)
	}
	if tw.FavoriteCount != 15 || tw.PublicMetrics.LikeCount != 15 {
		t.Errorf("expected like count 15, got %d", tw.PublicMetrics.LikeCount)
	}
	if tw.CreatedAt.Year() != 2018 {
		t.Errorf("expected year 2018, got %d", tw.CreatedAt.Year())
	}
}

func TestXClient_DeleteTweet_Success(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("DELETE", "/2/tweets/987654", http.StatusOK, `{"data":{"deleted":true}}`, nil)

	client := newTestClient(mock)
	deleted, err := client.DeleteTweet(context.Background(), 987654)
	if err != nil {
		t.Fatalf("DeleteTweet failed: %v", err)
	}
	if !deleted {
		t.Errorf("expected deleted == true, got false")
	}
}

func TestXClient_DeleteTweet_NotFound(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("DELETE", "/2/tweets/987654", http.StatusNotFound, `{"errors":[{"message":"Not Found"}]}`, nil)

	client := newTestClient(mock)
	deleted, err := client.DeleteTweet(context.Background(), 987654)
	if err != nil {
		t.Fatalf("expected nil error on 404 idempotent delete, got: %v", err)
	}
	if deleted {
		t.Errorf("expected deleted == false, got true")
	}
}

// =========================================================================
// 3. Resilience & Rate Limits Tests
// =========================================================================

func TestResilience_RateLimit15Min_PauseAndResume(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	resetTime := now.Add(120 * time.Second) // 2 minutes in future

	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusTooManyRequests, `{"title":"Too Many Requests"}`, map[string]string{
		"x-rate-limit-remaining": "0",
		"x-rate-limit-reset":     fmt.Sprintf("%d", resetTime.Unix()),
	})
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "123", "username": "recovered_user"},
	}, nil)

	var sleptDurations []time.Duration
	client := newTestClient(mock,
		xapi.WithNow(func() time.Time { return now }),
		xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
			sleptDurations = append(sleptDurations, d)
			return nil
		}),
	)

	user, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != 123 {
		t.Errorf("expected user ID 123, got %d", user.ID)
	}

	if len(sleptDurations) != 1 {
		t.Fatalf("expected 1 sleep call, got %d", len(sleptDurations))
	}
	// Expected: resetTime - now + 5s = 120s + 5s = 125s
	expectedSleep := 125 * time.Second
	if sleptDurations[0] != expectedSleep {
		t.Errorf("expected sleep duration %v, got %v", expectedSleep, sleptDurations[0])
	}
}

func TestResilience_RateLimitPastTimestamp_Fallback(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	pastReset := now.Add(-30 * time.Second) // 30s in the past

	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusTooManyRequests, `{"title":"Too Many Requests"}`, map[string]string{
		"x-rate-limit-remaining": "0",
		"x-rate-limit-reset":     fmt.Sprintf("%d", pastReset.Unix()),
	})
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "123"},
	}, nil)

	var sleptDurations []time.Duration
	client := newTestClient(mock,
		xapi.WithNow(func() time.Time { return now }),
		xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
			sleptDurations = append(sleptDurations, d)
			return nil
		}),
	)

	_, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sleptDurations) != 1 {
		t.Fatalf("expected 1 sleep call, got %d", len(sleptDurations))
	}
	// Expected fallback: 1801 seconds (30 minutes + 1s)
	expectedSleep := 1801 * time.Second
	if sleptDurations[0] != expectedSleep {
		t.Errorf("expected 30m fallback sleep %v, got %v", expectedSleep, sleptDurations[0])
	}
}

func TestResilience_DailyLimit24Hour_AppQuota(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	dailyReset := now.Add(3600 * time.Second) // 1 hour in future

	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusTooManyRequests, `{"title":"Too Many Requests"}`, map[string]string{
		"x-app-limit-24hour-remaining": "0",
		"x-app-limit-24hour-reset":     fmt.Sprintf("%d", dailyReset.Unix()),
	})
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "123"},
	}, nil)

	var sleptDurations []time.Duration
	client := newTestClient(mock,
		xapi.WithNow(func() time.Time { return now }),
		xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
			sleptDurations = append(sleptDurations, d)
			return nil
		}),
	)

	_, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sleptDurations) != 1 {
		t.Fatalf("expected 1 sleep call, got %d", len(sleptDurations))
	}
	// Daily wait: dailyReset - now + 60s = 3600s + 60s = 3660s
	expectedSleep := 3660 * time.Second
	if sleptDurations[0] != expectedSleep {
		t.Errorf("expected daily quota sleep %v, got %v", expectedSleep, sleptDurations[0])
	}
}

func TestResilience_DailyLimit24Hour_UserQuota(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	dailyReset := now.Add(7200 * time.Second) // 2 hours in future

	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusTooManyRequests, `{"title":"Too Many Requests"}`, map[string]string{
		"x-user-limit-24hour-remaining": "0",
		"x-user-limit-24hour-reset":     fmt.Sprintf("%d", dailyReset.Unix()),
	})
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "123"},
	}, nil)

	var sleptDurations []time.Duration
	client := newTestClient(mock,
		xapi.WithNow(func() time.Time { return now }),
		xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
			sleptDurations = append(sleptDurations, d)
			return nil
		}),
	)

	_, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sleptDurations) != 1 {
		t.Fatalf("expected 1 sleep call, got %d", len(sleptDurations))
	}
	// Daily wait: 7200s + 60s = 7260s
	expectedSleep := 7260 * time.Second
	if sleptDurations[0] != expectedSleep {
		t.Errorf("expected user daily quota sleep %v, got %v", expectedSleep, sleptDurations[0])
	}
}

func TestResilience_RetryAfterHeader(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusTooManyRequests, `{"title":"Too Many Requests"}`, map[string]string{
		"retry-after": "45",
	})
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "123"},
	}, nil)

	var sleptDurations []time.Duration
	client := newTestClient(mock,
		xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
			sleptDurations = append(sleptDurations, d)
			return nil
		}),
	)

	_, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sleptDurations) != 1 {
		t.Fatalf("expected 1 sleep call, got %d", len(sleptDurations))
	}
	// Expected: 45s + 5s = 50s
	expectedSleep := 50 * time.Second
	if sleptDurations[0] != expectedSleep {
		t.Errorf("expected sleep %v, got %v", expectedSleep, sleptDurations[0])
	}
}

func TestResilience_TransientRetries_Success(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/1.1/blocks/ids.json", http.StatusServiceUnavailable, `{"error":"503"}`, nil)
	mock.QueueResponse("GET", "/1.1/blocks/ids.json", http.StatusInternalServerError, `{"error":"500"}`, nil)
	mock.QueueResponse("GET", "/1.1/blocks/ids.json", http.StatusOK, map[string]interface{}{
		"ids":         []int64{999},
		"next_cursor": 0,
	}, nil)

	var retrySleeps []time.Duration
	client := newTestClient(mock,
		xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
			retrySleeps = append(retrySleeps, d)
			return nil
		}),
	)

	ids, err := client.GetBlockedUserIDs(context.Background())
	if err != nil {
		t.Fatalf("GetBlockedUserIDs failed: %v", err)
	}

	if len(ids) != 1 || ids[0] != 999 {
		t.Errorf("expected [999], got %v", ids)
	}
	if len(retrySleeps) != 2 {
		t.Errorf("expected 2 retry backoff sleeps, got %d", len(retrySleeps))
	}
	if mock.RequestCount("GET", "/1.1/blocks/ids.json") != 3 {
		t.Errorf("expected 3 attempts, got %d", mock.RequestCount("GET", "/1.1/blocks/ids.json"))
	}
}

func TestResilience_TransientRetries_Exhaustion(t *testing.T) {
	mock := NewMockTransport()
	for i := 0; i < 4; i++ {
		mock.QueueResponse("GET", "/1.1/blocks/ids.json", http.StatusServiceUnavailable, `{"error":"503"}`, nil)
	}

	var retrySleeps []time.Duration
	client := newTestClient(mock,
		xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
			retrySleeps = append(retrySleeps, d)
			return nil
		}),
	)

	_, err := client.GetBlockedUserIDs(context.Background())
	if err == nil {
		t.Fatalf("expected error after retry exhaustion, got nil")
	}

	if len(retrySleeps) != 3 {
		t.Errorf("expected 3 retry sleeps before exhaustion, got %d", len(retrySleeps))
	}
	if mock.RequestCount("GET", "/1.1/blocks/ids.json") != 4 {
		t.Errorf("expected 4 total attempts, got %d", mock.RequestCount("GET", "/1.1/blocks/ids.json"))
	}
}

func TestResilience_NonTransientFailures_NoRetry(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusBadRequest, `{"errors":[{"message":"Bad Request"}]}`, nil)

	var retrySleeps []time.Duration
	client := newTestClient(mock,
		xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
			retrySleeps = append(retrySleeps, d)
			return nil
		}),
	)

	_, err := client.GetMe(context.Background())
	if err == nil {
		t.Fatalf("expected error on 400 Bad Request, got nil")
	}

	if len(retrySleeps) != 0 {
		t.Errorf("expected 0 retries for 400, got %d", len(retrySleeps))
	}
	if mock.RequestCount("GET", "/2/users/me") != 1 {
		t.Errorf("expected exactly 1 attempt, got %d", mock.RequestCount("GET", "/2/users/me"))
	}
}

func TestResilience_ClockSkewCompensation(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	// Server clock is 10 seconds behind client
	serverDate := now.Add(-10 * time.Second).Format(http.TimeFormat)
	resetTime := now.Add(60 * time.Second)

	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusTooManyRequests, `{"title":"Too Many Requests"}`, map[string]string{
		"Date":               serverDate,
		"x-rate-limit-reset": fmt.Sprintf("%d", resetTime.Unix()),
	})
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{"data": map[string]interface{}{"id": "123"}}, nil)

	var sleptDurations []time.Duration
	client := newTestClient(mock,
		xapi.WithNow(func() time.Time { return now }),
		xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
			sleptDurations = append(sleptDurations, d)
			return nil
		}),
	)

	_, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sleptDurations) != 1 {
		t.Fatalf("expected 1 sleep call, got %d", len(sleptDurations))
	}
	// reset (60s) + skew (10s) + buffer (5s) = 75s
	expectedSleep := 75 * time.Second
	if sleptDurations[0] != expectedSleep {
		t.Errorf("expected skew-adjusted sleep %v, got %v", expectedSleep, sleptDurations[0])
	}
}

func TestResilience_ContextCancellation(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusTooManyRequests, `{"title":"Too Many Requests"}`, map[string]string{
		"retry-after": "60",
	})

	ctx, cancel := context.WithCancel(context.Background())
	client := newTestClient(mock,
		xapi.WithSleep(func(ctx context.Context, d time.Duration) error {
			cancel() // Cancel during sleep
			return ctx.Err()
		}),
	)

	_, err := client.GetMe(ctx)
	if err == nil {
		t.Fatalf("expected error on canceled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

// =========================================================================
// 4. 3-Tier Zombie Block Recovery Tests
// =========================================================================

func TestZombie_Tier1Success(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("POST", "/1.1/blocks/destroy.json", http.StatusOK, `{"id":999}`, nil)

	client := newTestClient(mock)
	status, err := client.UnblockUser(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "UNBLOCKED" && status != "SUCCESS" {
		t.Errorf("expected 'UNBLOCKED', got %q", status)
	}
	if mock.RequestCount("POST", "/1.1/blocks/destroy.json") != 1 {
		t.Errorf("expected 1 call to v1 destroy, got %d", mock.RequestCount("POST", "/1.1/blocks/destroy.json"))
	}
}

func TestZombie_NotFound_DeletedUser(t *testing.T) {
	mock := NewMockTransport()
	// Step 1: v1 destroy returns 404
	mock.QueueResponse("POST", "/1.1/blocks/destroy.json", http.StatusNotFound, `{"errors":[{"code":34,"message":"Not Found"}]}`, nil)
	// Step 2: v2 user existence check returns 404 / errors
	mock.QueueResponse("GET", "/2/users/999", http.StatusOK, map[string]interface{}{
		"data": nil,
		"errors": []map[string]interface{}{
			{"detail": "Could not find user with id [999]."},
		},
	}, nil)

	client := newTestClient(mock)
	status, err := client.UnblockUser(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "NOT_FOUND" {
		t.Errorf("expected 'NOT_FOUND', got %q", status)
	}
	if mock.RequestCount("DELETE", "/2/users/12345/blocking/999") != 0 {
		t.Errorf("should not attempt v2 unblock if user does not exist")
	}
}

func TestZombie_NotFound_SuspendedUser(t *testing.T) {
	mock := NewMockTransport()
	// Step 1: v1 destroy returns 404
	mock.QueueResponse("POST", "/1.1/blocks/destroy.json", http.StatusNotFound, `{"errors":[{"code":34}]}`, nil)
	// Step 2: v2 user check inconclusive
	mock.QueueResponse("GET", "/2/users/999", http.StatusInternalServerError, `{"error":"500"}`, nil)
	// Step 3: v1 user check returns 403 Forbidden (Suspended)
	mock.QueueResponse("GET", "/1.1/users/show.json", http.StatusForbidden, `{"errors":[{"code":63,"message":"User has been suspended"}]}`, nil)

	client := newTestClient(mock)
	status, err := client.UnblockUser(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "NOT_FOUND" {
		t.Errorf("expected 'NOT_FOUND' on suspended user, got %q", status)
	}
}

func TestZombie_Tier2Success(t *testing.T) {
	mock := NewMockTransport()
	// Cached or fetched authenticated user ID
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "12345"},
	}, nil)
	// Step 1: v1 destroy returns 404
	mock.QueueResponse("POST", "/1.1/blocks/destroy.json", http.StatusNotFound, `{"errors":[{"code":34}]}`, nil)
	// Step 2: User exists in v2
	mock.QueueResponse("GET", "/2/users/999", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "999", "username": "active_zombie"},
	}, nil)
	// Step 3: v2 unblock succeeds
	mock.QueueResponse("DELETE", "/2/users/12345/blocking/999", http.StatusOK, `{"data":{"blocking":false}}`, nil)

	client := newTestClient(mock)
	status, err := client.UnblockUser(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "UNBLOCKED" && status != "SUCCESS" {
		t.Errorf("expected 'UNBLOCKED', got %q", status)
	}
	if mock.RequestCount("DELETE", "/2/users/12345/blocking/999") != 1 {
		t.Errorf("expected 1 call to v2 unblock, got %d", mock.RequestCount("DELETE", "/2/users/12345/blocking/999"))
	}
}

func TestZombie_Tier3ToggleSuccess_V1(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "12345"},
	}, nil)
	// Step 1: v1 destroy returns 404
	mock.QueueResponse("POST", "/1.1/blocks/destroy.json", http.StatusNotFound, `{"errors":[{"code":34}]}`, nil)
	// Step 2: User exists
	mock.QueueResponse("GET", "/2/users/999", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "999"},
	}, nil)
	// Step 3: v2 unblock fails with 404
	mock.QueueResponse("DELETE", "/2/users/12345/blocking/999", http.StatusNotFound, `{"errors":[{"code":404}]}`, nil)
	// Step 4: v1 toggle succeeds (create then destroy)
	mock.QueueResponse("POST", "/1.1/blocks/create.json", http.StatusOK, `{"id":999}`, nil)
	mock.QueueResponse("POST", "/1.1/blocks/destroy.json", http.StatusOK, `{"id":999}`, nil)

	client := newTestClient(mock)
	status, err := client.UnblockUser(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "UNBLOCKED" && status != "SUCCESS" {
		t.Errorf("expected 'UNBLOCKED', got %q", status)
	}
	if mock.RequestCount("POST", "/1.1/blocks/create.json") != 1 {
		t.Errorf("expected 1 call to v1 create, got %d", mock.RequestCount("POST", "/1.1/blocks/create.json"))
	}
}

func TestZombie_Tier3ToggleSuccess_V2(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "12345"},
	}, nil)
	// Step 1: v1 destroy returns 404
	mock.QueueResponse("POST", "/1.1/blocks/destroy.json", http.StatusNotFound, `{"errors":[{"code":34}]}`, nil)
	// Step 2: User exists
	mock.QueueResponse("GET", "/2/users/999", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "999"},
	}, nil)
	// Step 3: v2 unblock fails
	mock.QueueResponse("DELETE", "/2/users/12345/blocking/999", http.StatusNotFound, `{"errors":[{"code":404}]}`, nil)
	// Step 4: v1 toggle fails on create
	mock.QueueResponse("POST", "/1.1/blocks/create.json", http.StatusForbidden, `{"errors":[{"code":403,"message":"Forbidden"}]}`, nil)
	// Step 5: v2 toggle succeeds (create then unblock)
	mock.QueueResponse("POST", "/2/users/12345/blocking", http.StatusOK, `{"data":{"blocking":true}}`, nil)
	mock.QueueResponse("DELETE", "/2/users/12345/blocking/999", http.StatusOK, `{"data":{"blocking":false}}`, nil)

	client := newTestClient(mock)
	status, err := client.UnblockUser(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "UNBLOCKED" && status != "SUCCESS" {
		t.Errorf("expected 'UNBLOCKED', got %q", status)
	}
	if mock.RequestCount("POST", "/2/users/12345/blocking") != 1 {
		t.Errorf("expected 1 call to v2 block create, got %d", mock.RequestCount("POST", "/2/users/12345/blocking"))
	}
}

func TestZombie_V2HtmlError_GracefulFallthrough(t *testing.T) {
	mock := NewMockTransport()
	mock.QueueResponse("GET", "/2/users/me", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "12345"},
	}, nil)
	// Step 1: v1 destroy returns 404
	mock.QueueResponse("POST", "/1.1/blocks/destroy.json", http.StatusNotFound, `{"errors":[{"code":34}]}`, nil)
	// Step 2: User exists
	mock.QueueResponse("GET", "/2/users/999", http.StatusOK, map[string]interface{}{
		"data": map[string]interface{}{"id": "999"},
	}, nil)
	// Step 3: v2 unblock returns 502 HTML
	mock.QueueResponse("DELETE", "/2/users/12345/blocking/999", http.StatusBadGateway, "<html><body>502 Bad Gateway</body></html>", map[string]string{
		"Content-Type": "text/html",
	})
	// Step 4: v1 toggle succeeds
	mock.QueueResponse("POST", "/1.1/blocks/create.json", http.StatusOK, `{"id":999}`, nil)
	mock.QueueResponse("POST", "/1.1/blocks/destroy.json", http.StatusOK, `{"id":999}`, nil)

	client := newTestClient(mock)
	status, err := client.UnblockUser(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "UNBLOCKED" && status != "SUCCESS" {
		t.Errorf("expected 'UNBLOCKED', got %q", status)
	}
}

func TestZombie_ExistenceCheckFailed(t *testing.T) {
	mock := NewMockTransport()
	// Step 1: v1 destroy returns 404
	mock.QueueResponse("POST", "/1.1/blocks/destroy.json", http.StatusNotFound, `{"errors":[{"code":34}]}`, nil)
	// Step 2: v2 user check fails with 400 Bad Request
	mock.QueueResponse("GET", "/2/users/999", http.StatusBadRequest, `{"error":"400"}`, nil)
	// Step 3: v1 user check fails with 400 Bad Request
	mock.QueueResponse("GET", "/1.1/users/show.json", http.StatusBadRequest, `{"error":"400"}`, nil)

	client := newTestClient(mock)
	status, err := client.UnblockUser(context.Background(), 999)
	if err == nil {
		t.Fatalf("expected non-nil error when existence check fails, got nil")
	}
	if status != "FAILED" {
		t.Errorf("expected status 'FAILED', got %q", status)
	}
}
