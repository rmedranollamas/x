package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/rmedranollamas/x-agent/internal/logging"
	"github.com/spf13/cobra"
)

// setValidTestEnv configures required credentials and a local mock Twitter API server.
func setValidTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("X_API_KEY", "test_key")
	t.Setenv("X_API_KEY_SECRET", "test_secret")
	t.Setenv("X_ACCESS_TOKEN", "test_token")
	t.Setenv("X_ACCESS_TOKEN_SECRET", "test_token_secret")
	t.Setenv("X_AGENT_ENV", "development")
	t.Setenv("SMTP_USER", "test_user@example.com")
	t.Setenv("SMTP_PASSWORD", "test_password")
	t.Setenv("REPORT_SENDER", "sender@example.com")
	t.Setenv("REPORT_RECIPIENT", "recipient@example.com")

	// Start local mock HTTP server to avoid external network requests during CLI testing
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2/users/me":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"id":         "999999",
					"name":       "Mock Account",
					"username":   "mock_user",
					"created_at": "2020-01-01T00:00:00.000Z",
					"public_metrics": map[string]interface{}{
						"followers_count": 100,
						"following_count": 50,
						"tweet_count":     200,
						"listed_count":    5,
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/1.1/blocks/ids.json":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ids":             []int64{},
				"next_cursor":     0,
				"previous_cursor": 0,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/1.1/followers/ids.json":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ids":             []int64{},
				"next_cursor":     0,
				"previous_cursor": 0,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/1.1/statuses/user_timeline.json":
			json.NewEncoder(w).Encode([]interface{}{})
		default:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(mockServer.Close)

	t.Setenv("TWITTER_API_BASE_URL", mockServer.URL)
}

func executeCommand(cmd *cobra.Command, args ...string) (stdout string, stderr string, err error) {
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetArgs(args)

	logging.SetOutput(errBuf)
	defer logging.SetOutput(os.Stderr)

	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestRootCmd_Help(t *testing.T) {
	// Help must succeed even when zero environment variables are configured.
	cmd := newRootCmd()
	stdout, stderr, err := executeCommand(cmd, "--help")

	if err != nil {
		t.Fatalf("expected nil error for --help, got: %v", err)
	}
	if stderr != "" {
		t.Errorf("expected empty stderr, got: %s", stderr)
	}

	expectedSubcommands := []string{
		"unblock",
		"insights",
		"blocked-ids",
		"unfollow",
		"delete",
		"db",
	}

	for _, sub := range expectedSubcommands {
		if !strings.Contains(stdout, sub) {
			t.Errorf("expected help output to contain subcommand %q", sub)
		}
	}
}

func TestRootCmd_HelpShort(t *testing.T) {
	cmd := newRootCmd()
	stdout, _, err := executeCommand(cmd, "-h")
	if err != nil {
		t.Fatalf("expected nil error for -h, got: %v", err)
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Errorf("expected help output to contain 'Usage:'")
	}
}

func TestRootCmd_NoArgs(t *testing.T) {
	// Root command with no args displays help without error.
	cmd := newRootCmd()
	stdout, _, err := executeCommand(cmd)
	if err != nil {
		t.Fatalf("expected nil error when running with no args, got: %v", err)
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Errorf("expected output to contain 'Usage:'")
	}
}

func TestRootCmd_ConfigError(t *testing.T) {
	// Clear credentials.
	t.Setenv("X_API_KEY", "")
	t.Setenv("X_API_KEY_SECRET", "")
	t.Setenv("X_ACCESS_TOKEN", "")
	t.Setenv("X_ACCESS_TOKEN_SECRET", "")

	cmd := newRootCmd()
	_, stderr, err := executeCommand(cmd, "insights")

	if err == nil {
		t.Fatal("expected error on missing credentials, got nil")
	}
	if !strings.Contains(stderr, "Configuration Error: Missing required environment variables:") {
		t.Errorf("expected stderr to contain Configuration Error message, got: %s", stderr)
	}
}

func TestRootCmd_StartupHeader_Development(t *testing.T) {
	setValidTestEnv(t)
	t.Setenv("X_AGENT_ENV", "development")

	cmd := newRootCmd()
	stdout, stderr, err := executeCommand(cmd, "unblock")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stdout != "" {
		t.Errorf("expected clean stdout (0 banner bytes), got: %q", stdout)
	}
	if strings.Contains(stdout, "Environment:") || strings.Contains(stdout, "DEVELOPMENT") {
		t.Errorf("expected stdout to contain no banner text, got: %s", stdout)
	}
	if !strings.Contains(stderr, "Environment:") || !strings.Contains(stderr, "DEVELOPMENT") {
		t.Errorf("expected stderr to contain 'Environment:' and 'DEVELOPMENT', got: %s", stderr)
	}
	if !strings.Contains(stderr, "Database:") || !strings.Contains(stderr, "insights_dev.db") {
		t.Errorf("expected stderr to contain 'Database:' and 'insights_dev.db', got: %s", stderr)
	}
	if strings.Contains(stderr, "\033") {
		t.Errorf("expected stderr on non-terminal buffer to contain no ANSI escape codes, got: %q", stderr)
	}
}

func TestRootCmd_StartupHeader_Production(t *testing.T) {
	setValidTestEnv(t)
	t.Setenv("X_AGENT_ENV", "production")

	cmd := newRootCmd()
	stdout, stderr, err := executeCommand(cmd, "unblock")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stdout != "" {
		t.Errorf("expected clean stdout (0 banner bytes), got: %q", stdout)
	}
	if strings.Contains(stdout, "Environment:") || strings.Contains(stdout, "PRODUCTION") {
		t.Errorf("expected stdout to contain no banner text, got: %s", stdout)
	}
	if !strings.Contains(stderr, "Environment:") || !strings.Contains(stderr, "PRODUCTION") {
		t.Errorf("expected stderr to contain 'Environment:' and 'PRODUCTION', got: %s", stderr)
	}
	if !strings.Contains(stderr, "Database:") || !strings.Contains(stderr, "insights.db") {
		t.Errorf("expected stderr to contain 'Database:' and 'insights.db', got: %s", stderr)
	}
	if strings.Contains(stderr, "\033") {
		t.Errorf("expected stderr on non-terminal buffer to contain no ANSI escape codes, got: %q", stderr)
	}
}

func TestUnblockCommand_Flags(t *testing.T) {
	setValidTestEnv(t)

	cmd := newRootCmd()
	_, _, err := executeCommand(cmd, "unblock", "--user-id", "12345", "--refresh", "--dry-run", "--debug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	unblockCmd, _, err := cmd.Find([]string{"unblock"})
	if err != nil {
		t.Fatalf("failed to find unblock command: %v", err)
	}

	uid, err := unblockCmd.Flags().GetInt64("user-id")
	if err != nil || uid != 12345 {
		t.Errorf("expected user-id 12345, got: %d", uid)
	}

	refresh, _ := unblockCmd.Flags().GetBool("refresh")
	if !refresh {
		t.Errorf("expected refresh true")
	}

	dryRun, _ := unblockCmd.Flags().GetBool("dry-run")
	if !dryRun {
		t.Errorf("expected dry-run true")
	}

	debug, _ := unblockCmd.Flags().GetBool("debug")
	if !debug {
		t.Errorf("expected debug true")
	}
}

func TestUnblockCommand_InvalidUserID(t *testing.T) {
	setValidTestEnv(t)

	cmd := newRootCmd()
	_, _, err := executeCommand(cmd, "unblock", "--user-id", "invalid_id")
	if err == nil {
		t.Fatal("expected error for non-integer user-id, got nil")
	}
}

func TestInsightsCommand_Flags(t *testing.T) {
	setValidTestEnv(t)

	cmd := newRootCmd()
	_, _, err := executeCommand(cmd, "insights", "--email", "--debug")
	if err != nil && !strings.Contains(err.Error(), "SMTP") {
		t.Fatalf("unexpected error: %v", err)
	}

	insightsCmd, _, _ := cmd.Find([]string{"insights"})
	email, _ := insightsCmd.Flags().GetBool("email")
	if !email {
		t.Errorf("expected email true")
	}
	debug, _ := insightsCmd.Flags().GetBool("debug")
	if !debug {
		t.Errorf("expected debug true")
	}
}

func TestBlockedIDsCommand_Flags(t *testing.T) {
	setValidTestEnv(t)

	cmd := newRootCmd()
	stdout, stderr, err := executeCommand(cmd, "blocked-ids", "--debug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stdout != "" {
		t.Errorf("expected clean stdout for blocked-ids, got: %q", stdout)
	}
	if !strings.Contains(stderr, "Environment:") {
		t.Errorf("expected stderr to contain startup header, got: %s", stderr)
	}

	blockedCmd, _, _ := cmd.Find([]string{"blocked-ids"})
	debug, _ := blockedCmd.Flags().GetBool("debug")
	if !debug {
		t.Errorf("expected debug true")
	}
}

func TestBlockedIDsCommand_CleanStdout_UnixPipe(t *testing.T) {
	setValidTestEnv(t)

	cmd := newRootCmd()
	stdout, stderr, err := executeCommand(cmd, "blocked-ids")
	if err != nil {
		t.Fatalf("unexpected error executing blocked-ids: %v", err)
	}

	// In M1, placeholder produces 0 stdout bytes (clean for Unix piping)
	if stdout != "" {
		t.Errorf("expected 0 bytes in stdout for blocked-ids pipe, got: %q", stdout)
	}
	if strings.Contains(stdout, "Environment:") || strings.Contains(stdout, "Database:") {
		t.Errorf("expected stdout to contain no banner text, got: %s", stdout)
	}
	if strings.Contains(stdout, "\033") {
		t.Errorf("expected no ANSI color codes in stdout, got: %q", stdout)
	}

	// Stderr must receive the header
	if !strings.Contains(stderr, "Environment:") || !strings.Contains(stderr, "DEVELOPMENT") {
		t.Errorf("expected stderr to contain header environment, got: %s", stderr)
	}
	if !strings.Contains(stderr, "Database:") || !strings.Contains(stderr, "insights_dev.db") {
		t.Errorf("expected stderr to contain header database, got: %s", stderr)
	}
}

func TestAllAgentCommands_CleanStdout(t *testing.T) {
	setValidTestEnv(t)

	agentCommands := []string{"blocked-ids", "unblock", "insights", "unfollow", "delete"}
	for _, name := range agentCommands {
		t.Run(name, func(t *testing.T) {
			cmd := newRootCmd()
			stdout, stderr, err := executeCommand(cmd, name)
			if err != nil {
				t.Fatalf("command %s failed unexpectedly: %v", name, err)
			}
			if strings.Contains(stdout, "Environment:") || strings.Contains(stdout, "Database:") {
				t.Errorf("command %s: expected 0 banner text in stdout, got: %s", name, stdout)
			}
			if strings.Contains(stdout, "\033") {
				t.Errorf("command %s: expected no ANSI color codes in stdout, got: %q", name, stdout)
			}
			if !strings.Contains(stderr, "Environment:") || !strings.Contains(stderr, "DEVELOPMENT") {
				t.Errorf("command %s: expected stderr to contain startup header, got: %s", name, stderr)
			}
		})
	}
}

func TestUnfollowCommand_Flags(t *testing.T) {
	setValidTestEnv(t)

	cmd := newRootCmd()
	_, _, err := executeCommand(cmd, "unfollow", "--dry-run", "--email", "--debug")
	if err != nil && !strings.Contains(err.Error(), "SMTP") {
		t.Fatalf("unexpected error: %v", err)
	}

	unfollowCmd, _, _ := cmd.Find([]string{"unfollow"})
	dryRun, _ := unfollowCmd.Flags().GetBool("dry-run")
	if !dryRun {
		t.Errorf("expected dry-run true")
	}
	email, _ := unfollowCmd.Flags().GetBool("email")
	if !email {
		t.Errorf("expected email true")
	}
}

func TestDeleteCommand_Flags(t *testing.T) {
	setValidTestEnv(t)

	cmd := newRootCmd()
	_, _, err := executeCommand(cmd, "delete",
		"--dry-run",
		"--email",
		"--protected-id", "111",
		"--protected-id", "222",
		"--archive", "tweets.js",
		"--debug",
	)
	if err != nil && !strings.Contains(err.Error(), "SMTP") {
		t.Fatalf("unexpected error: %v", err)
	}

	deleteCmd, _, _ := cmd.Find([]string{"delete"})
	dryRun, _ := deleteCmd.Flags().GetBool("dry-run")
	if !dryRun {
		t.Errorf("expected dry-run true")
	}
	email, _ := deleteCmd.Flags().GetBool("email")
	if !email {
		t.Errorf("expected email true")
	}

	protectedIDs, _ := deleteCmd.Flags().GetInt64Slice("protected-id")
	if len(protectedIDs) != 2 || protectedIDs[0] != 111 || protectedIDs[1] != 222 {
		t.Errorf("expected protected-ids [111, 222], got: %v", protectedIDs)
	}

	archive, _ := deleteCmd.Flags().GetString("archive")
	if archive != "tweets.js" {
		t.Errorf("expected archive 'tweets.js', got: %s", archive)
	}
}

func TestDB_Backup(t *testing.T) {
	setValidTestEnv(t)

	cmd := newRootCmd()
	stdout, stderr, err := executeCommand(cmd, "db", "backup", "--debug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Backup created at:") && !strings.Contains(stdout, "Backup failed or no database exists.") {
		t.Errorf("expected output to contain backup status message, got: %s", stdout)
	}
	if strings.Contains(stdout, "| Database:") {
		t.Errorf("expected stdout not to contain startup banner, got: %s", stdout)
	}
	if !strings.Contains(stderr, "Environment:") || !strings.Contains(stderr, "DEVELOPMENT") {
		t.Errorf("expected stderr to contain startup banner, got: %s", stderr)
	}
}

func TestDB_Info(t *testing.T) {
	setValidTestEnv(t)
	t.Setenv("X_AGENT_ENV", "development")

	cmd := newRootCmd()
	stdout, stderr, err := executeCommand(cmd, "db", "info")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stdout, "Environment: development") {
		t.Errorf("expected Environment: development, got: %s", stdout)
	}
	if !strings.Contains(stdout, "Database File: .state/insights_dev.db") {
		t.Errorf("expected Database File: .state/insights_dev.db, got: %s", stdout)
	}
	if !strings.Contains(stdout, "Is Dev: True") {
		t.Errorf("expected Is Dev: True, got: %s", stdout)
	}
	if strings.Contains(stdout, "| Database:") {
		t.Errorf("expected stdout not to contain startup banner, got: %s", stdout)
	}
	if !strings.Contains(stderr, "Environment:") || !strings.Contains(stderr, "DEVELOPMENT") {
		t.Errorf("expected stderr to contain startup header, got: %s", stderr)
	}
}

func TestInvalidCommand(t *testing.T) {
	cmd := newRootCmd()
	_, _, err := executeCommand(cmd, "invalid")
	if err == nil {
		t.Fatal("expected error for invalid command, got nil")
	}
	if !strings.Contains(err.Error(), `unknown command "invalid" for "x-agent"`) {
		t.Errorf("expected unknown command error, got: %v", err)
	}
}

func TestInvalidFlag(t *testing.T) {
	setValidTestEnv(t)

	cmd := newRootCmd()
	_, _, err := executeCommand(cmd, "unblock", "--nonexistent-flag")
	if err == nil {
		t.Fatal("expected error for nonexistent flag, got nil")
	}
	if !strings.Contains(err.Error(), `unknown flag: --nonexistent-flag`) {
		t.Errorf("expected unknown flag error, got: %v", err)
	}
}
