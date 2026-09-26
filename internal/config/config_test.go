package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rmedranollamas/x-agent/internal/config"
)

func TestSettingsDefaultEnv(t *testing.T) {
	t.Setenv("X_AGENT_ENV", "")
	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Environment != "development" {
		t.Errorf("expected default environment 'development', got '%s'", cfg.Environment)
	}
	if !cfg.IsDev() {
		t.Errorf("expected IsDev() to be true for default environment")
	}
	if cfg.DBName() != "insights_dev.db" {
		t.Errorf("expected DBName() to be 'insights_dev.db', got '%s'", cfg.DBName())
	}
	if cfg.DBPath() != filepath.Join(".state", "insights_dev.db") {
		t.Errorf("expected DBPath() to be '.state/insights_dev.db', got '%s'", cfg.DBPath())
	}
}

func TestSettingsProdEnv(t *testing.T) {
	t.Setenv("X_AGENT_ENV", "production")
	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Environment != "production" {
		t.Errorf("expected environment 'production', got '%s'", cfg.Environment)
	}
	if cfg.IsDev() {
		t.Errorf("expected IsDev() to be false for production environment")
	}
	if cfg.DBName() != "insights.db" {
		t.Errorf("expected DBName() to be 'insights.db', got '%s'", cfg.DBName())
	}
	if cfg.DBPath() != filepath.Join(".state", "insights.db") {
		t.Errorf("expected DBPath() to be '.state/insights.db', got '%s'", cfg.DBPath())
	}
}

func TestNormalizedEnvironment(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectedDev bool
	}{
		{"whitespace lowercase", "  development  ", "development", true},
		{"mixed case whitespace", "  Development  ", "development", true},
		{"uppercase", "DEVELOPMENT", "development", true},
		{"production uppercase", "PRODUCTION", "production", false},
		{"production whitespace", "  production  ", "production", false},
		{"staging", "staging", "staging", false},
		{"empty", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Environment: tc.input}
			if got := cfg.NormalizedEnvironment(); got != tc.expected {
				t.Errorf("NormalizedEnvironment() = %q; want %q", got, tc.expected)
			}
			if gotDev := cfg.IsDev(); gotDev != tc.expectedDev {
				t.Errorf("IsDev() = %v; want %v", gotDev, tc.expectedDev)
			}
		})
	}
}

func TestCheckConfigSuccess(t *testing.T) {
	cfg := &config.Config{
		XAPIKey:            "k",
		XAPIKeySecret:      "ks",
		XAccessToken:       "t",
		XAccessTokenSecret: "ts",
		Environment:        "development",
	}

	if err := cfg.CheckConfig(); err != nil {
		t.Errorf("expected CheckConfig() to succeed, got error: %v", err)
	}
}

func TestCheckConfigFailure_Single(t *testing.T) {
	cfg := config.Config{
		XAPIKey:            "",
		XAPIKeySecret:      "ks",
		XAccessToken:       "t",
		XAccessTokenSecret: "ts",
	}
	expected := "Missing required environment variables: X_API_KEY. Please check your .env file."
	err := cfg.CheckConfig()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != expected {
		t.Errorf("CheckConfig() error = %q; want %q", err.Error(), expected)
	}
}

func TestCheckConfigFailure_All(t *testing.T) {
	cfg := config.Config{
		XAPIKey:            "",
		XAPIKeySecret:      "",
		XAccessToken:       "",
		XAccessTokenSecret: "",
	}
	expected := "Missing required environment variables: X_API_KEY, X_API_KEY_SECRET, X_ACCESS_TOKEN, X_ACCESS_TOKEN_SECRET. Please check your .env file."
	err := cfg.CheckConfig()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != expected {
		t.Errorf("CheckConfig() error = %q; want %q", err.Error(), expected)
	}
}

func TestCheckConfigFailure_Whitespace(t *testing.T) {
	cfg := config.Config{
		XAPIKey:            "   ",
		XAPIKeySecret:      "\t",
		XAccessToken:       "\n",
		XAccessTokenSecret: "  ",
	}
	expected := "Missing required environment variables: X_API_KEY, X_API_KEY_SECRET, X_ACCESS_TOKEN, X_ACCESS_TOKEN_SECRET. Please check your .env file."
	err := cfg.CheckConfig()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != expected {
		t.Errorf("CheckConfig() error = %q; want %q", err.Error(), expected)
	}
}

func TestCheckEmailConfigSuccess(t *testing.T) {
	cfg := &config.Config{
		SMTPUser:        "user@example.com",
		SMTPPassword:    "password",
		ReportSender:    "sender@example.com",
		ReportRecipient: "recipient@example.com",
	}

	if err := cfg.CheckEmailConfig(); err != nil {
		t.Errorf("expected CheckEmailConfig() to succeed, got error: %v", err)
	}
}

func TestCheckEmailConfigMissingFields(t *testing.T) {
	tests := []struct {
		name          string
		cfg           config.Config
		expectedError string
	}{
		{
			name: "missing SMTP_USER",
			cfg: config.Config{
				SMTPUser:        "",
				SMTPPassword:    "password",
				ReportSender:    "sender@example.com",
				ReportRecipient: "recipient@example.com",
			},
			expectedError: "Email reporting requires: SMTP_USER. Please add them to your .env file.",
		},
		{
			name: "missing SMTP_PASSWORD",
			cfg: config.Config{
				SMTPUser:        "user@example.com",
				SMTPPassword:    "",
				ReportSender:    "sender@example.com",
				ReportRecipient: "recipient@example.com",
			},
			expectedError: "Email reporting requires: SMTP_PASSWORD. Please add them to your .env file.",
		},
		{
			name: "missing REPORT_SENDER",
			cfg: config.Config{
				SMTPUser:        "user@example.com",
				SMTPPassword:    "password",
				ReportSender:    "",
				ReportRecipient: "recipient@example.com",
			},
			expectedError: "Email reporting requires: REPORT_SENDER. Please add them to your .env file.",
		},
		{
			name: "missing REPORT_RECIPIENT",
			cfg: config.Config{
				SMTPUser:        "user@example.com",
				SMTPPassword:    "password",
				ReportSender:    "sender@example.com",
				ReportRecipient: "",
			},
			expectedError: "Email reporting requires: REPORT_RECIPIENT. Please add them to your .env file.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.CheckEmailConfig()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if err.Error() != tc.expectedError {
				t.Errorf("CheckEmailConfig() error = %q; want %q", err.Error(), tc.expectedError)
			}
		})
	}
}

func TestCheckEmailConfigMultipleMissing(t *testing.T) {
	cfg := config.Config{
		SMTPUser:        "",
		SMTPPassword:    "",
		ReportSender:    "",
		ReportRecipient: "",
	}
	expected := "Email reporting requires: SMTP_USER, SMTP_PASSWORD, REPORT_SENDER, REPORT_RECIPIENT. Please add them to your .env file."
	err := cfg.CheckEmailConfig()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != expected {
		t.Errorf("CheckEmailConfig() error = %q; want %q", err.Error(), expected)
	}
}

func TestCheckEmailConfigWhitespace(t *testing.T) {
	cfg := config.Config{
		SMTPUser:        "   ",
		SMTPPassword:    "  ",
		ReportSender:    "\t",
		ReportRecipient: "\n",
	}
	expected := "Email reporting requires: SMTP_USER, SMTP_PASSWORD, REPORT_SENDER, REPORT_RECIPIENT. Please add them to your .env file."
	err := cfg.CheckEmailConfig()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != expected {
		t.Errorf("CheckEmailConfig() error = %q; want %q", err.Error(), expected)
	}
}

func TestDefaultSMTPValues(t *testing.T) {
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_USE_TLS", "")
	t.Setenv("SMTP_START_TLS", "")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.SMTPHost != "smtp.gmail.com" {
		t.Errorf("expected default SMTPHost 'smtp.gmail.com', got '%s'", cfg.SMTPHost)
	}
	if cfg.SMTPPort != 587 {
		t.Errorf("expected default SMTPPort 587, got %d", cfg.SMTPPort)
	}
	if cfg.SMTPUseTLS != false {
		t.Errorf("expected default SMTPUseTLS false, got %v", cfg.SMTPUseTLS)
	}
	if cfg.SMTPStartTLS != true {
		t.Errorf("expected default SMTPStartTLS true, got %v", cfg.SMTPStartTLS)
	}
}

func TestLoadFile_Success(t *testing.T) {
	tempDir := t.TempDir()
	envFile := filepath.Join(tempDir, "test.env")

	content := `
X_API_KEY="test_key"
X_API_KEY_SECRET="test_secret"
X_ACCESS_TOKEN="test_token"
X_ACCESS_TOKEN_SECRET="test_token_secret"
X_AGENT_ENV="production"
SMTP_PORT=465
SMTP_USE_TLS=True
SMTP_START_TLS=False
`
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write temp env file: %v", err)
	}

	cfg, err := config.LoadFile(envFile)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	if cfg.XAPIKey != "test_key" {
		t.Errorf("expected XAPIKey 'test_key', got '%s'", cfg.XAPIKey)
	}
	if cfg.Environment != "production" {
		t.Errorf("expected Environment 'production', got '%s'", cfg.Environment)
	}
	if cfg.SMTPPort != 465 {
		t.Errorf("expected SMTPPort 465, got %d", cfg.SMTPPort)
	}
	if !cfg.SMTPUseTLS {
		t.Errorf("expected SMTPUseTLS true, got %v", cfg.SMTPUseTLS)
	}
	if cfg.SMTPStartTLS {
		t.Errorf("expected SMTPStartTLS false, got %v", cfg.SMTPStartTLS)
	}
}

func TestLoad_MissingDotEnvGraceful(t *testing.T) {
	// Loading a non-existent file should gracefully fall back to OS environment without error
	_, err := config.LoadFile("/non/existent/path/.env")
	if err != nil {
		t.Errorf("expected graceful handling of missing env file, got error: %v", err)
	}
}
