package e2e_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rmedranollamas/x-agent/internal/db"
	"github.com/rmedranollamas/x-agent/internal/xapi"
)

// ============================================================================
// Tier 5 Adversarial Test 1: Clock Skew, Reset Past, & Rate Limits
// ============================================================================

func TestTier5_XAPI_RateLimit_ClockSkewAndPastReset(t *testing.T) {
	mockNow := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cfg := xapi.DefaultResilienceConfig()
	cfg.NowFunc = func() time.Time { return mockNow }

	handler := xapi.NewResilienceHandler(cfg, http.DefaultClient, func(ctx context.Context, d time.Duration) error {
		return nil
	})

	// Scenario 1: Clock skew - Client clock is 5 minutes BEHIND server clock
	// Server Date: 12:05:00 UTC, Server Reset: 12:15:00 UTC (10m in server future)
	// In client's reference frame, reset should be at 12:10:00 UTC (10m in client future)
	{
		hdr := make(http.Header)
		hdr.Set("Date", "Sun, 20 Sep 2026 12:05:00 GMT")
		hdr.Set("x-rate-limit-reset", strconv.FormatInt(mockNow.Add(15*time.Minute).Unix(), 10)) // 12:15:00
		decision := handler.ParseRateLimitHeaders(hdr)

		if !decision.IsRateLimited {
			t.Errorf("expected IsRateLimited=true")
		}
		if decision.IsDailyLimit {
			t.Errorf("expected IsDailyLimit=false for 15m window")
		}
		// Expected wait: (12:10:00 - 12:00:00) + 5s buffer = 10m5s = 605s
		expectedWait := 10*time.Minute + 5*time.Second
		if decision.WaitDuration != expectedWait {
			t.Errorf("expected wait %v with server ahead clock skew, got %v", expectedWait, decision.WaitDuration)
		}
	}

	// Scenario 2: Clock skew - Client clock is 5 minutes AHEAD of server clock
	// Server Date: 11:55:00 UTC, Server Reset: 12:05:00 UTC (10m in server future)
	// In client's reference frame, reset should be at 12:10:00 UTC (10m in client future)
	{
		hdr := make(http.Header)
		hdr.Set("Date", "Sun, 20 Sep 2026 11:55:00 GMT")
		hdr.Set("x-rate-limit-reset", strconv.FormatInt(mockNow.Add(5*time.Minute).Unix(), 10)) // 12:05:00
		decision := handler.ParseRateLimitHeaders(hdr)

		expectedWait := 10*time.Minute + 5*time.Second
		if decision.WaitDuration != expectedWait {
			t.Errorf("expected wait %v with client ahead clock skew, got %v", expectedWait, decision.WaitDuration)
		}
	}

	// Scenario 3: Reset timestamp is in the past! (diff <= 0)
	// Must fall back to 30-minute backoff (1801 seconds)
	{
		hdr := make(http.Header)
		hdr.Set("Date", "Sun, 20 Sep 2026 12:00:00 GMT")
		hdr.Set("x-rate-limit-reset", strconv.FormatInt(mockNow.Add(-5*time.Minute).Unix(), 10)) // 11:55:00
		decision := handler.ParseRateLimitHeaders(hdr)

		if decision.WaitDuration != 1801*time.Second {
			t.Errorf("expected 1801s backoff for past timestamp, got %v", decision.WaitDuration)
		}
		if !strings.Contains(decision.Reason, "past") {
			t.Errorf("expected reason to mention 'past', got: %s", decision.Reason)
		}
	}

	// Scenario 4: 24-hour daily quota detection (x-app-limit-24hour-remaining: 0)
	{
		hdr := make(http.Header)
		tomorrowEpoch := mockNow.Add(24 * time.Hour).Unix()
		hdr.Set("x-app-limit-24hour-remaining", "0")
		hdr.Set("x-app-limit-24hour-reset", strconv.FormatInt(tomorrowEpoch, 10))
		decision := handler.ParseRateLimitHeaders(hdr)

		if !decision.IsDailyLimit {
			t.Errorf("expected IsDailyLimit=true")
		}
		expectedWait := 24*time.Hour + 60*time.Second
		if decision.WaitDuration != expectedWait {
			t.Errorf("expected wait %v for daily quota, got %v", expectedWait, decision.WaitDuration)
		}
	}

	// Scenario 5: Missing headers fallback (NoHeaderSleep: 901s = 15m + 1s)
	{
		hdr := make(http.Header)
		decision := handler.ParseRateLimitHeaders(hdr)

		if decision.WaitDuration != 901*time.Second {
			t.Errorf("expected 901s wait for empty headers fallback, got %v", decision.WaitDuration)
		}
	}

	// Scenario 6: Retry-After header with integer seconds
	{
		hdr := make(http.Header)
		hdr.Set("retry-after", "120")
		decision := handler.ParseRateLimitHeaders(hdr)

		expectedWait := 120*time.Second + 5*time.Second
		if decision.WaitDuration != expectedWait {
			t.Errorf("expected %v wait for Retry-After=120, got %v", expectedWait, decision.WaitDuration)
		}
	}
}

// ============================================================================
// Tier 5 Adversarial Test 2: Error Classification & Malformed Payloads
// ============================================================================

func TestTier5_XAPI_ErrorClassification_And_MalformedBodies(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.twitter.com/2/users/me", nil)

	// Sub-test 1: Malformed HTML returned on 502 Bad Gateway
	{
		resp := &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"text/html"}},
		}
		htmlBody := []byte("<html><head><title>502 Bad Gateway</title></head><body><h1>Bad Gateway</h1></body></html>")
		err := xapi.ParseAPIError(resp, htmlBody, req)

		if err == nil {
			t.Fatalf("expected non-nil error on 502")
		}
		if !xapi.IsTransient(err) {
			t.Errorf("expected 502 Bad Gateway with HTML body to be classified as transient")
		}
		var apiErr *xapi.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected error to be *xapi.APIError")
		}
		if apiErr.StatusCode != 502 {
			t.Errorf("expected status code 502, got %d", apiErr.StatusCode)
		}
		if apiErr.Message != "Bad Gateway" {
			t.Errorf("expected fallback message 'Bad Gateway', got %q", apiErr.Message)
		}
	}

	// Sub-test 2: v1.1 Error payload on 403 Forbidden
	{
		resp := &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}
		jsonBody := []byte(`{"errors":[{"code":326,"message":"To protect our users from spam, this account is locked."}]}`)
		err := xapi.ParseAPIError(resp, jsonBody, req)

		if !xapi.IsForbidden(err) {
			t.Errorf("expected IsForbidden=true on 403")
		}
		var fbErr *xapi.ForbiddenError
		if !errors.As(err, &fbErr) {
			t.Fatalf("expected error to wrap *xapi.ForbiddenError")
		}
		if fbErr.ErrorCode != 326 {
			t.Errorf("expected error code 326, got %d", fbErr.ErrorCode)
		}
		if !strings.Contains(fbErr.Message, "protect our users") {
			t.Errorf("expected detailed error message, got %q", fbErr.Message)
		}
	}

	// Sub-test 3: v2 Error payload on 401 Unauthorized
	{
		resp := &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}
		jsonBody := []byte(`{"title":"Unauthorized","detail":"Unauthorized","type":"about:blank","status":401}`)
		err := xapi.ParseAPIError(resp, jsonBody, req)

		if !xapi.IsUnauthorized(err) {
			t.Errorf("expected IsUnauthorized=true on 401")
		}
		if xapi.IsTransient(err) {
			t.Errorf("expected 401 Unauthorized to NOT be transient")
		}
	}

	// Sub-test 4: 404 Not Found payload
	{
		resp := &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}
		jsonBody := []byte(`{"errors":[{"code":34,"message":"Sorry, that page does not exist."}]}`)
		err := xapi.ParseAPIError(resp, jsonBody, req)

		if !xapi.IsNotFound(err) {
			t.Errorf("expected IsNotFound=true on 404")
		}
	}

	// Sub-test 5: Context cancellation must NOT be transient
	{
		if xapi.IsTransientError(context.Canceled, 0) {
			t.Errorf("context.Canceled must NEVER be classified as transient")
		}
		if xapi.IsTransientError(context.DeadlineExceeded, 0) {
			t.Errorf("context.DeadlineExceeded must NEVER be classified as transient")
		}
	}
}

// ============================================================================
// Tier 5 Adversarial Test 3: Resilience Retries & Non-Retry on 429
// ============================================================================

func TestTier5_XAPI_Resilience_TransientRetriesAnd429NonConsumption(t *testing.T) {
	// Scenario: Server returns 500 twice, then 429 (rate limit), then 200 OK.
	// Invariant 1: 500 triggers exponential backoff transient retries.
	// Invariant 2: 429 breaks out of transient loop without consuming retry quota.
	// Invariant 3: After rate limit sleep, outer loop restarts and succeeds on attempt 0.

	var attemptCount int32
	var rateLimitSlept int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attemptCount, 1)
		switch att {
		case 1, 2:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"errors":[{"message":"Internal error"}]}`))
		case 3:
			w.Header().Set("x-rate-limit-reset", strconv.FormatInt(time.Now().Add(1*time.Second).Unix(), 10))
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"errors":[{"message":"Rate limit exceeded"}]}`))
		case 4:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"id":"123","username":"testuser"}}`))
		default:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"id":"123","username":"testuser"}}`))
		}
	}))
	defer server.Close()

	rCfg := xapi.DefaultResilienceConfig()
	rCfg.InitialBackoff = 1 * time.Millisecond
	rCfg.MaxBackoff = 5 * time.Millisecond
	rCfg.RateLimitBuffer15m = 0

	handler := xapi.NewResilienceHandler(rCfg, server.Client(), func(ctx context.Context, d time.Duration) error {
		if d >= 500*time.Millisecond {
			atomic.AddInt32(&rateLimitSlept, 1)
		}
		return nil
	})

	resp, err := handler.Execute(context.Background(), "GET /test", func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, server.URL, nil)
	})

	if err != nil {
		t.Fatalf("expected eventual success after retries and rate limit sleep, got err: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected final status 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attemptCount) != 4 {
		t.Errorf("expected exactly 4 attempts (2 transient + 1 rate-limit + 1 success), got %d", atomic.LoadInt32(&attemptCount))
	}
	if atomic.LoadInt32(&rateLimitSlept) != 1 {
		t.Errorf("expected rate limit sleep to be invoked exactly once, got %d", atomic.LoadInt32(&rateLimitSlept))
	}
}

// ============================================================================
// Tier 5 Adversarial Test 4: Zombie Block 3-Tier State Machine
// ============================================================================

type mockZombieClient struct {
	v1DestroyStatus int
	userExists      bool
	v2UnblockStatus int
	v1CreateStatus  int
	v1Destroy2Stat  int
	v2BlockStatus   int
	v2Unblock2Stat  int

	calls []string
	mu    sync.Mutex
}

func (m *mockZombieClient) record(call string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, call)
}

func (m *mockZombieClient) GetAuthenticatedUserID(ctx context.Context) (int64, error) {
	return 999999, nil
}

func (m *mockZombieClient) V1DestroyBlock(ctx context.Context, targetUserID int64) (*http.Response, []byte, error) {
	m.record("v1_destroy")
	status := m.v1DestroyStatus
	if len(m.calls) > 3 && m.v1Destroy2Stat != 0 {
		status = m.v1Destroy2Stat
	}
	resp := &http.Response{StatusCode: status}
	if status != 200 {
		return resp, nil, &xapi.APIError{StatusCode: status, Message: http.StatusText(status)}
	}
	return resp, []byte(`{}`), nil
}

func (m *mockZombieClient) V1CreateBlock(ctx context.Context, targetUserID int64) (*http.Response, []byte, error) {
	m.record("v1_create")
	resp := &http.Response{StatusCode: m.v1CreateStatus}
	if m.v1CreateStatus != 200 {
		return resp, nil, &xapi.APIError{StatusCode: m.v1CreateStatus}
	}
	return resp, []byte(`{}`), nil
}

func (m *mockZombieClient) V1GetUser(ctx context.Context, targetUserID int64) (*http.Response, []byte, error) {
	m.record("v1_get_user")
	if m.userExists {
		return &http.Response{StatusCode: 200}, []byte(`{"id":1001,"screen_name":"zombie_target"}`), nil
	}
	return &http.Response{StatusCode: 404}, nil, &xapi.NotFoundError{}
}

func (m *mockZombieClient) V2Unblock(ctx context.Context, sourceUserID, targetUserID int64) (*http.Response, []byte, error) {
	m.record("v2_unblock")
	status := m.v2UnblockStatus
	if len(m.calls) > 4 && m.v2Unblock2Stat != 0 {
		status = m.v2Unblock2Stat
	}
	resp := &http.Response{StatusCode: status}
	if status != 200 {
		return resp, nil, &xapi.APIError{StatusCode: status}
	}
	return resp, []byte(`{}`), nil
}

func (m *mockZombieClient) V2Block(ctx context.Context, sourceUserID, targetUserID int64) (*http.Response, []byte, error) {
	m.record("v2_block")
	resp := &http.Response{StatusCode: m.v2BlockStatus}
	if m.v2BlockStatus != 200 {
		return resp, nil, &xapi.APIError{StatusCode: m.v2BlockStatus}
	}
	return resp, []byte(`{}`), nil
}

func (m *mockZombieClient) V2GetUser(ctx context.Context, targetUserID int64) (*http.Response, []byte, error) {
	m.record("v2_get_user")
	if m.userExists {
		return &http.Response{StatusCode: 200}, []byte(`{"data":{"id":"1001","username":"zombie_target"}}`), nil
	}
	return &http.Response{StatusCode: 404}, nil, &xapi.NotFoundError{}
}

func TestTier5_XAPI_ZombieUnblock_RecoveryStateMachine(t *testing.T) {
	// Case A: Fast Path - v1 unblock succeeds immediately (HTTP 200)
	{
		mock := &mockZombieClient{v1DestroyStatus: 200}
		zm := xapi.NewZombieManager(mock)
		status, err := zm.UnblockUser(context.Background(), 1001)
		if err != nil || status != xapi.StatusUnblocked {
			t.Errorf("expected StatusUnblocked on fast path, got %s, err: %v", status, err)
		}
		if len(mock.calls) != 1 || mock.calls[0] != "v1_destroy" {
			t.Errorf("expected only v1_destroy call, got: %v", mock.calls)
		}
	}

	// Case B: v1 unblock 404, user does NOT exist -> returns StatusNotFound
	{
		mock := &mockZombieClient{v1DestroyStatus: 404, userExists: false}
		zm := xapi.NewZombieManager(mock)
		status, err := zm.UnblockUser(context.Background(), 1002)
		if err != nil || status != xapi.StatusNotFound {
			t.Errorf("expected StatusNotFound for non-existent user, got %s, err: %v", status, err)
		}
	}

	// Case C: Zombie Tier 2 - v1 unblock 404, user EXISTS, v2 unblock succeeds (HTTP 200)
	{
		mock := &mockZombieClient{
			v1DestroyStatus: 404,
			userExists:      true,
			v2UnblockStatus: 200,
		}
		zm := xapi.NewZombieManager(mock)
		status, err := zm.UnblockUser(context.Background(), 1003)
		if err != nil || status != xapi.StatusUnblocked {
			t.Errorf("expected StatusUnblocked on Tier 2 recovery, got %s, err: %v", status, err)
		}
	}

	// Case D: Zombie Tier 3a - v1 unblock 404, v2 unblock 404, v1 toggle block (create -> destroy) succeeds
	{
		mock := &mockZombieClient{
			v1DestroyStatus: 404,
			userExists:      true,
			v2UnblockStatus: 404,
			v1CreateStatus:  200,
			v1Destroy2Stat:  200,
		}
		zm := xapi.NewZombieManager(mock)
		status, err := zm.UnblockUser(context.Background(), 1004)
		if err != nil || status != xapi.StatusUnblocked {
			t.Errorf("expected StatusUnblocked on Tier 3a recovery, got %s, err: %v", status, err)
		}
	}

	// Case E: Terminal Zombie - all tiers fail -> returns StatusNotFound to terminate gracefully
	{
		mock := &mockZombieClient{
			v1DestroyStatus: 404,
			userExists:      true,
			v2UnblockStatus: 404,
			v1CreateStatus:  500,
			v2BlockStatus:   500,
		}
		zm := xapi.NewZombieManager(mock)
		status, err := zm.UnblockUser(context.Background(), 1005)
		if err != nil || status != xapi.StatusNotFound {
			t.Errorf("expected StatusNotFound on terminal zombie exhaustion, got %s, err: %v", status, err)
		}
	}
}

// ============================================================================
// Tier 5 Adversarial Test 5: Deeply Nested Subdirectories & Pre-Migration Backups
// ============================================================================

func TestTier5_DB_NestedSubdirectoryAndMissingBackup(t *testing.T) {
	tempDir := t.TempDir()
	nestedPath := filepath.Join(tempDir, "deep", "nested", "sub", "dir", "test_insights.db")

	// 1. Initializing DBManager in non-existent deep hierarchy must auto-create parent dirs
	mgr, err := db.NewDBManager(nestedPath)
	if err != nil {
		t.Fatalf("failed to initialize DB in nested hierarchy: %v", err)
	}
	defer mgr.Close()

	if _, err := os.Stat(filepath.Dir(nestedPath)); os.IsNotExist(err) {
		t.Errorf("expected parent directory of nested DB to be created")
	}

	// 2. Backup on fresh 0-byte or uncreated DB must return ("", nil) without error
	backupPath, err := mgr.BackupDatabase()
	if err != nil {
		t.Fatalf("expected no error on backing up empty DB, got: %v", err)
	}
	if backupPath != "" {
		t.Errorf("expected empty string backupPath on empty DB, got %q", backupPath)
	}

	// 3. Run migrations - must create tables and initialize schema
	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// 4. Now that DB has content, explicit backup must generate timestamped file in <dir>/backups/
	backupPath, err = mgr.BackupDatabase()
	if err != nil {
		t.Fatalf("failed to backup populated DB: %v", err)
	}
	if backupPath == "" {
		t.Fatalf("expected non-empty backup path")
	}

	expectedBackupDir := filepath.Join(filepath.Dir(nestedPath), "backups")
	if filepath.Dir(backupPath) != expectedBackupDir {
		t.Errorf("expected backup in %s, got %s", expectedBackupDir, filepath.Dir(backupPath))
	}
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Errorf("expected backup file %s to exist on disk", backupPath)
	}
}

// ============================================================================
// Tier 5 Adversarial Test 6: Concurrency & Panic Rollback Under MaxOpenConns(1)
// ============================================================================

func TestTier5_DB_Concurrency_And_PanicRollback(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "concurrency_test.db")

	mgr, err := db.NewDBManager(dbPath)
	if err != nil {
		t.Fatalf("failed to create db manager: %v", err)
	}
	defer mgr.Close()

	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Sub-test A: Transaction Panic Rollback
	// Ensure that if a transaction panics, WithTx recovers, rolls back the transaction,
	// and re-panics without corrupting the DB or leaving the connection locked.
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatalf("expected panic to be propagated by WithTx")
			}
			if r != "simulated crash" {
				t.Errorf("expected 'simulated crash' panic, got: %v", r)
			}
		}()

		_ = mgr.WithTx(context.Background(), func(tx *sql.Tx) error {
			_, err := tx.Exec("INSERT INTO blocked_users (user_id, status) VALUES (9999, 'PENDING')")
			if err != nil {
				return err
			}
			panic("simulated crash")
		})
	}()

	// Verify that user 9999 was NOT persisted (atomic rollback on panic verified)
	pending, err := mgr.GetPendingBlockedUsers(context.Background())
	if err != nil {
		t.Fatalf("failed to query pending users after panic: %v", err)
	}
	for _, id := range pending {
		if id == 9999 {
			t.Errorf("expected user 9999 to be rolled back after panic, but found in database")
		}
	}

	// Sub-test B: High-concurrency serialized reads and writes
	const numGoroutines = 15
	const opsPerGoroutine = 10
	var wg sync.WaitGroup

	errChan := make(chan error, numGoroutines*opsPerGoroutine)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				uid := int64(routineID*1000 + i)
				// Write op
				if err := mgr.UpsertBlockedUsers(context.Background(), []int64{uid}); err != nil {
					errChan <- fmt.Errorf("routine %d write %d: %w", routineID, uid, err)
					return
				}
				// Read op
				if _, err := mgr.GetPendingBlockedUsers(context.Background()); err != nil {
					errChan <- fmt.Errorf("routine %d read: %w", routineID, err)
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrency error: %v", err)
	}

	count, err := mgr.GetAllBlockedUsersCount(context.Background())
	if err != nil {
		t.Fatalf("failed to get blocked users count: %v", err)
	}
	expectedCount := numGoroutines * opsPerGoroutine
	if count != expectedCount {
		t.Errorf("expected %d rows in blocked_users, got %d", expectedCount, count)
	}
}

// ============================================================================
// Tier 5 Adversarial Test 7: Migration Idempotency & Schema Introspection
// ============================================================================

func TestTier5_DB_Migration_IdempotencyAndPartialUpgrades(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "migration_idempotent.db")

	mgr, err := db.NewDBManager(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer mgr.Close()

	// First run: executes all 4 migrations
	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("first migration run failed: %v", err)
	}

	appliedFirst, err := mgr.GetAppliedVersions(context.Background())
	if err != nil {
		t.Fatalf("failed to get applied versions: %v", err)
	}
	if len(appliedFirst) != 4 {
		t.Errorf("expected 4 applied migrations, got %d", len(appliedFirst))
	}

	// Count backups created so far
	backupDir := filepath.Join(tempDir, "backups")
	initialBackups, _ := os.ReadDir(backupDir)

	// Second run: must be a pure no-op (zero new backups, zero new transactions)
	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}

	appliedSecond, err := mgr.GetAppliedVersions(context.Background())
	if err != nil {
		t.Fatalf("failed to get applied versions on second run: %v", err)
	}
	if len(appliedSecond) != 4 {
		t.Errorf("expected still 4 applied migrations, got %d", len(appliedSecond))
	}

	secondBackups, _ := os.ReadDir(backupDir)
	if len(secondBackups) != len(initialBackups) {
		t.Errorf("expected 0 new backups on idempotent second migration, got %d new files", len(secondBackups)-len(initialBackups))
	}
}

// ============================================================================
// Tier 5 Adversarial Test 8: Repository Edge Cases & Duplicate Inputs
// ============================================================================

func TestTier5_DB_EdgeCases_DuplicateFollowersAndOffsets(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "edge_cases.db")

	mgr, err := db.NewDBManager(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer mgr.Close()

	if err := mgr.RunMigrations(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Edge Case 1: UpsertBlockedUsers with duplicate IDs in the same call
	// ON CONFLICT must handle duplicates cleanly without failing
	duplicateIDs := []int64{5001, 5002, 5001, 5003, 5002}
	if err := mgr.UpsertBlockedUsers(context.Background(), duplicateIDs); err != nil {
		t.Errorf("UpsertBlockedUsers failed on duplicate input slice: %v", err)
	}

	pending, err := mgr.GetPendingBlockedUsers(context.Background())
	if err != nil {
		t.Fatalf("failed to get pending users: %v", err)
	}
	if len(pending) != 3 {
		t.Errorf("expected 3 unique blocked users, got %d", len(pending))
	}

	// Edge Case 2: SyncFollowing with duplicate IDs in the same call
	// INSERT OR IGNORE must handle duplicates cleanly
	followIDs := []int64{6001, 6002, 6001, 6003}
	if err := mgr.SyncFollowing(context.Background(), followIDs); err != nil {
		t.Errorf("SyncFollowing failed on duplicate input: %v", err)
	}
	fIDs, err := mgr.GetFollowingIDs(context.Background())
	if err != nil {
		t.Fatalf("failed to get following IDs: %v", err)
	}
	if len(fIDs) != 3 {
		t.Errorf("expected 3 unique following users, got %d", len(fIDs))
	}

	// Edge Case 3: GetInsightAtOffset on empty insights table
	// Must return (nil, nil) rather than sql.ErrNoRows error
	insight, err := mgr.GetInsightAtOffset(context.Background(), 7)
	if err != nil {
		t.Errorf("expected nil error on empty table offset query, got: %v", err)
	}
	if insight != nil {
		t.Errorf("expected nil insight on empty table, got: %+v", insight)
	}

	// Edge Case 4: AddInsight with 0-values and retrieve
	if err := mgr.AddInsight(context.Background(), 0, 0, 0, 0); err != nil {
		t.Fatalf("failed to add zero insight: %v", err)
	}
	latest, err := mgr.GetLatestInsight(context.Background())
	if err != nil {
		t.Fatalf("failed to get latest insight: %v", err)
	}
	if latest == nil || latest.Followers != 0 || latest.Following != 0 {
		t.Errorf("expected zero-insight record, got: %+v", latest)
	}
}
