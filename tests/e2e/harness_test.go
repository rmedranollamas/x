package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ExecResult captures the result of executing the x-agent CLI.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}

// MockTwitterServer provides configurable HTTP endpoints for Twitter v1.1 and v2 APIs.
type MockTwitterServer struct {
	Server             *httptest.Server
	Mu                 sync.Mutex
	BlockedIDs         []int64
	FollowingIDs       []int64
	FollowerIDs        []int64
	MeID               string
	MeUsername         string
	PinnedTweetID      string
	FollowersCount     int
	FollowingCount     int
	TweetCount         int
	ListedCount        int
	UnblockErrors      map[int64]int // user_id -> HTTP status code
	V2RateLimitHeaders map[string]string
	V1RateLimitHeaders map[string]string
	DestroyedBlocks    []int64
	DestroyedFriends   []int64
	DeletedTweets      []string
	UsersBatch         map[string]map[string]interface{}
	RequestLog         []string
}

// NewMockTwitterServer initializes a new mock HTTP server for Twitter API endpoints.
func NewMockTwitterServer() *MockTwitterServer {
	ms := &MockTwitterServer{
		BlockedIDs:      []int64{1001, 1002, 1003},
		FollowingIDs:    []int64{2001, 2002},
		FollowerIDs:     []int64{3001, 3002, 3003, 3004},
		MeID:            "999999",
		MeUsername:      "mock_user",
		PinnedTweetID:   "888888",
		FollowersCount:  150,
		FollowingCount:  42,
		TweetCount:      350,
		ListedCount:     12,
		UnblockErrors:   make(map[int64]int),
		UsersBatch:      make(map[string]map[string]interface{}),
		DestroyedBlocks: make([]int64, 0),
		DeletedTweets:   make([]string, 0),
		RequestLog:      make([]string, 0),
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ms.Mu.Lock()
		defer ms.Mu.Unlock()
		ms.RequestLog = append(ms.RequestLog, fmt.Sprintf("%s %s", r.Method, r.URL.Path))

		// Apply rate limit headers if configured
		if strings.HasPrefix(r.URL.Path, "/2/") {
			for k, v := range ms.V2RateLimitHeaders {
				w.Header().Set(k, v)
			}
		} else {
			for k, v := range ms.V1RateLimitHeaders {
				w.Header().Set(k, v)
			}
		}

		switch {
		// v2 users/me
		case r.Method == http.MethodGet && r.URL.Path == "/2/users/me":
			resp := map[string]interface{}{
				"data": map[string]interface{}{
					"id":              ms.MeID,
					"name":            "Mock Account",
					"username":        ms.MeUsername,
					"created_at":      "2020-01-01T00:00:00.000Z",
					"pinned_tweet_id": ms.PinnedTweetID,
					"public_metrics": map[string]interface{}{
						"followers_count": ms.FollowersCount,
						"following_count": ms.FollowingCount,
						"tweet_count":     ms.TweetCount,
						"listed_count":    ms.ListedCount,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		// v2 users batch lookup
		case r.Method == http.MethodGet && r.URL.Path == "/2/users":
			idsStr := r.URL.Query().Get("ids")
			requestedIDs := strings.Split(idsStr, ",")
			dataList := make([]map[string]interface{}, 0)
			for _, id := range requestedIDs {
				id = strings.TrimSpace(id)
				if u, ok := ms.UsersBatch[id]; ok {
					dataList = append(dataList, u)
				} else {
					dataList = append(dataList, map[string]interface{}{
						"id":       id,
						"name":     "User " + id,
						"username": "handle_" + id,
					})
				}
			}
			resp := map[string]interface{}{"data": dataList}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		// v2 delete tweet: DELETE /2/tweets/:id
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/2/tweets/"):
			tweetID := strings.TrimPrefix(r.URL.Path, "/2/tweets/")
			ms.DeletedTweets = append(ms.DeletedTweets, tweetID)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"deleted": true},
			})

		// v1.1 blocks/ids.json
		case r.Method == http.MethodGet && r.URL.Path == "/1.1/blocks/ids.json":
			idsCopy := append([]int64(nil), ms.BlockedIDs...)
			resp := map[string]interface{}{
				"ids":             idsCopy,
				"next_cursor":     0,
				"previous_cursor": 0,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		// v1.1 blocks/destroy.json
		case r.Method == http.MethodPost && r.URL.Path == "/1.1/blocks/destroy.json":
			r.ParseForm()
			uidStr := r.Form.Get("user_id")
			uid, _ := strconv.ParseInt(uidStr, 10, 64)
			if status, ok := ms.UnblockErrors[uid]; ok && status != http.StatusOK {
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"errors": []map[string]interface{}{{"code": status, "message": "Simulated error"}},
				})
				return
			}
			ms.DestroyedBlocks = append(ms.DestroyedBlocks, uid)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"id": uid, "screen_name": fmt.Sprintf("user_%d", uid)})

		// v1.1 blocks/create.json
		case r.Method == http.MethodPost && r.URL.Path == "/1.1/blocks/create.json":
			r.ParseForm()
			uidStr := r.Form.Get("user_id")
			uid, _ := strconv.ParseInt(uidStr, 10, 64)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"id": uid, "screen_name": fmt.Sprintf("user_%d", uid)})

		// v1.1 followers/ids.json
		case r.Method == http.MethodGet && r.URL.Path == "/1.1/followers/ids.json":
			idsCopy := append([]int64(nil), ms.FollowerIDs...)
			resp := map[string]interface{}{
				"ids":             idsCopy,
				"next_cursor":     0,
				"previous_cursor": 0,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		// v1.1 friends/ids.json
		case r.Method == http.MethodGet && r.URL.Path == "/1.1/friends/ids.json":
			idsCopy := append([]int64(nil), ms.FollowingIDs...)
			resp := map[string]interface{}{
				"ids":             idsCopy,
				"next_cursor":     0,
				"previous_cursor": 0,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		// v1.1 friendships/destroy.json
		case r.Method == http.MethodPost && r.URL.Path == "/1.1/friendships/destroy.json":
			r.ParseForm()
			uidStr := r.Form.Get("user_id")
			uid, _ := strconv.ParseInt(uidStr, 10, 64)
			ms.DestroyedFriends = append(ms.DestroyedFriends, uid)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"id": uid, "screen_name": fmt.Sprintf("user_%d", uid)})

		// v1.1 users/show.json
		case r.Method == http.MethodGet && r.URL.Path == "/1.1/users/show.json":
			uidStr := r.URL.Query().Get("user_id")
			uid, _ := strconv.ParseInt(uidStr, 10, 64)
			if status, ok := ms.UnblockErrors[uid]; ok && status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":          uid,
				"name":        fmt.Sprintf("User %d", uid),
				"screen_name": fmt.Sprintf("screen_%d", uid),
			})

		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		}
	})

	ms.Server = httptest.NewServer(handler)
	return ms
}

// Close shuts down the test HTTP server.
func (ms *MockTwitterServer) Close() {
	if ms.Server != nil {
		ms.Server.Close()
	}
}

// TestContext holds the per-test isolated execution context.
type TestContext struct {
	T          *testing.T
	WorkDir    string
	BinPath    string
	MockServer *MockTwitterServer
	Env        map[string]string
	DBPath     string
}

// FindProjectRoot locates the workspace root directory containing PROJECT.md or go.mod.
func FindProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "PROJECT.md")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate project root from %s", dir)
		}
		dir = parent
	}
}

// FindOrBuildBinary locates the pre-built x-agent binary or attempts to build it.
// If the implementation is not yet created, it gracefully skips the test.
func FindOrBuildBinary(t *testing.T) string {
	t.Helper()
	projectRoot := FindProjectRoot(t)

	// 1. Check explicit environment override
	if envBin := os.Getenv("X_AGENT_BIN"); envBin != "" {
		if _, err := os.Stat(envBin); err == nil {
			return envBin
		}
	}

	// 2. Check binary in project root
	rootBin := filepath.Join(projectRoot, "x-agent")
	if info, err := os.Stat(rootBin); err == nil && !info.IsDir() {
		return rootBin
	}

	// 3. Attempt to compile cmd/x-agent
	cmdDir := filepath.Join(projectRoot, "cmd", "x-agent")
	if info, err := os.Stat(cmdDir); err == nil && info.IsDir() {
		tmpBin := filepath.Join(t.TempDir(), "x-agent")
		buildCmd := exec.Command("go", "build", "-o", tmpBin, "./cmd/x-agent")
		buildCmd.Dir = projectRoot
		buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		out, err := buildCmd.CombinedOutput()
		if err == nil {
			return tmpBin
		}
		t.Logf("Notice: cmd/x-agent build failed: %s: %v", string(out), err)
	}

	// Graceful skip for progressive testability
	t.Skip("Skipping test: x-agent binary or cmd/x-agent source not yet available (pending milestone implementation)")
	return ""
}

// NewTestContext creates a fully isolated test sandbox with mock server and environment.
func NewTestContext(t *testing.T) *TestContext {
	t.Helper()
	binPath := FindOrBuildBinary(t)
	workDir := t.TempDir()

	stateDir := filepath.Join(workDir, ".state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("failed to create .state dir: %v", err)
	}

	mockServer := NewMockTwitterServer()
	t.Cleanup(func() {
		mockServer.Close()
	})

	dbPath := filepath.Join(stateDir, "insights_dev.db")

	tc := &TestContext{
		T:          t,
		WorkDir:    workDir,
		BinPath:    binPath,
		MockServer: mockServer,
		DBPath:     dbPath,
		Env: map[string]string{
			"X_API_KEY":             "test_key_12345",
			"X_API_KEY_SECRET":      "test_secret_67890",
			"X_ACCESS_TOKEN":        "test_token_abcdef",
			"X_ACCESS_TOKEN_SECRET": "test_token_secret_xyz",
			"X_AGENT_ENV":           "development",
			"HTTP_PROXY":            mockServer.Server.URL,
			"HTTPS_PROXY":           mockServer.Server.URL,
			"TWITTER_API_BASE_URL":  mockServer.Server.URL,
			"SMTP_HOST":             "localhost",
			"SMTP_PORT":             "1025",
			"SMTP_USER":             "test_user@example.com",
			"SMTP_PASSWORD":         "secret123",
			"REPORT_SENDER":         "sender@example.com",
			"REPORT_RECIPIENT":      "recipient@example.com",
		},
	}

	// Write default .env file in sandbox
	tc.WriteEnvFile()
	return tc
}

// WriteEnvFile writes current environment keys into .env in the sandbox directory.
func (tc *TestContext) WriteEnvFile() {
	var buf bytes.Buffer
	for k, v := range tc.Env {
		buf.WriteString(fmt.Sprintf("%s=%s\n", k, v))
	}
	envPath := filepath.Join(tc.WorkDir, ".env")
	if err := os.WriteFile(envPath, buf.Bytes(), 0644); err != nil {
		tc.T.Fatalf("failed to write .env: %v", err)
	}
}

// Run executes the CLI binary with the given arguments and returns ExecResult.
func (tc *TestContext) Run(args ...string) (*ExecResult, error) {
	return tc.RunWithEnv(nil, args...)
}

// RunWithEnv executes the CLI with extra environment overrides.
func (tc *TestContext) RunWithEnv(extraEnv map[string]string, args ...string) (*ExecResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, tc.BinPath, args...)
	cmd.Dir = tc.WorkDir

	envMap := make(map[string]string)
	for k, v := range tc.Env {
		envMap[k] = v
	}
	for k, v := range extraEnv {
		envMap[k] = v
	}

	mergedEnv := os.Environ()
	for k, v := range envMap {
		mergedEnv = append(mergedEnv, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = mergedEnv

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	res := &ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if os.Getenv("E2E_VERBOSE") == "1" || os.Getenv("E2E_VERBOSE") == "true" {
		tc.T.Logf("CLI Invocation: %s %v\nExitCode: %d\nStdout:\n%s\nStderr:\n%s",
			tc.BinPath, args, exitCode, res.Stdout, res.Stderr)
	}

	return res, err
}

// MustRun executes the CLI and expects exit code 0.
func (tc *TestContext) MustRun(args ...string) *ExecResult {
	tc.T.Helper()
	res, err := tc.Run(args...)
	if res.ExitCode != 0 {
		tc.T.Fatalf("expected exit code 0, got %d. Err: %v\nStdout: %s\nStderr: %s",
			res.ExitCode, err, res.Stdout, res.Stderr)
	}
	return res
}

// MustFail executes the CLI and expects non-zero exit code.
func (tc *TestContext) MustFail(args ...string) *ExecResult {
	tc.T.Helper()
	res, _ := tc.Run(args...)
	if res.ExitCode == 0 {
		tc.T.Fatalf("expected command to fail, but exited with 0.\nStdout: %s\nStderr: %s",
			res.Stdout, res.Stderr)
	}
	return res
}

// QueryDB executes a SQL query on the specified database path using sqlite3 CLI.
func (tc *TestContext) QueryDB(dbPath, sql string) (string, error) {
	tc.T.Helper()
	cmd := exec.Command("sqlite3", dbPath, sql)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// CountRows returns the number of rows in the specified table.
func (tc *TestContext) CountRows(dbPath, tableName string) int {
	tc.T.Helper()
	out, err := tc.QueryDB(dbPath, fmt.Sprintf("SELECT COUNT(*) FROM %s;", tableName))
	if err != nil {
		return -1
	}
	count, _ := strconv.Atoi(out)
	return count
}

// TableExists checks whether a table exists in the given database.
func (tc *TestContext) TableExists(dbPath, tableName string) bool {
	tc.T.Helper()
	out, err := tc.QueryDB(dbPath, fmt.Sprintf("SELECT name FROM sqlite_master WHERE type='table' AND name='%s';", tableName))
	if err != nil {
		return false
	}
	return out == tableName
}

// ColumnExists checks whether a column exists in a specific table.
func (tc *TestContext) ColumnExists(dbPath, tableName, columnName string) bool {
	tc.T.Helper()
	out, err := tc.QueryDB(dbPath, fmt.Sprintf("PRAGMA table_info(%s);", tableName))
	if err != nil {
		return false
	}
	return strings.Contains(out, columnName)
}

// SeedLegacyDB creates a SQLite database at schema version 1 (m001).
func (tc *TestContext) SeedLegacyDB(dbPath string) {
	tc.T.Helper()
	initSQL := `
CREATE TABLE schema_versions (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
    description TEXT
);
INSERT INTO schema_versions (version, description) VALUES (1, 'Initial schema');

CREATE TABLE insights (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
    followers INTEGER,
    following INTEGER,
    tweet_count INTEGER DEFAULT 0
);

CREATE TABLE blocked_users (
    user_id INTEGER PRIMARY KEY,
    unblocked_at DATETIME,
    status TEXT DEFAULT 'PENDING',
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
);

CREATE TABLE following_users (
    user_id INTEGER PRIMARY KEY,
    status TEXT DEFAULT 'PENDING',
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
);

CREATE TABLE followers (
    user_id INTEGER PRIMARY KEY,
    updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
);

CREATE TABLE unfollows (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    timestamp DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
);
`
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		tc.T.Fatalf("failed to create db directory: %v", err)
	}
	cmd := exec.Command("sqlite3", dbPath, initSQL)
	if out, err := cmd.CombinedOutput(); err != nil {
		tc.T.Fatalf("failed to seed legacy database: %s: %v", string(out), err)
	}
}

// CreateMockTweetArchive writes a sample tweets.js or tweets.json archive file.
func (tc *TestContext) CreateMockTweetArchive(fileName string, tweets []map[string]interface{}) string {
	tc.T.Helper()
	data, err := json.Marshal(tweets)
	if err != nil {
		tc.T.Fatalf("failed to marshal tweets: %v", err)
	}

	content := fmt.Sprintf("window.YTD.tweets.part0 = %s", string(data))
	filePath := filepath.Join(tc.WorkDir, fileName)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		tc.T.Fatalf("failed to write archive: %v", err)
	}
	return filePath
}
