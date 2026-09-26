package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config holds all configuration options for the x-agent CLI.
type Config struct {
	// X (Twitter) API OAuth 1.0a Credentials
	XAPIKey            string `env:"X_API_KEY"`
	XAPIKeySecret      string `env:"X_API_KEY_SECRET"`
	XAccessToken       string `env:"X_ACCESS_TOKEN"`
	XAccessTokenSecret string `env:"X_ACCESS_TOKEN_SECRET"`

	// Execution Environment
	Environment string `env:"X_AGENT_ENV" envDefault:"development"`

	// SMTP Settings for Email Reporting
	SMTPHost        string `env:"SMTP_HOST" envDefault:"smtp.gmail.com"`
	SMTPPort        int    `env:"SMTP_PORT" envDefault:"587"`
	SMTPUser        string `env:"SMTP_USER"`
	SMTPPassword    string `env:"SMTP_PASSWORD"`
	ReportSender    string `env:"REPORT_SENDER"`
	ReportRecipient string `env:"REPORT_RECIPIENT"`
	SMTPUseTLS      bool   `env:"SMTP_USE_TLS" envDefault:"false"`
	SMTPStartTLS    bool   `env:"SMTP_START_TLS" envDefault:"true"`
}

// Load reads .env (if present) and parses environment variables into a Config struct.
func Load() (*Config, error) {
	return LoadFile(".env")
}

// LoadFile reads the specified .env files if present, then unmarshals environment variables.
func LoadFile(filenames ...string) (*Config, error) {
	if len(filenames) > 0 {
		err := godotenv.Load(filenames...)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			var pathErr *os.PathError
			if !errors.As(err, &pathErr) || !errors.Is(pathErr.Err, os.ErrNotExist) {
				return nil, fmt.Errorf("error loading env file: %w", err)
			}
		}
	}
	return LoadFromEnv()
}

// LoadFromEnv unmarshals environment variables directly into a Config struct.
func LoadFromEnv() (*Config, error) {
	cfg := &Config{}
	opts := env.Options{}
	if err := env.ParseWithOptions(cfg, opts); err != nil {
		return nil, fmt.Errorf("failed to parse environment variables: %w", err)
	}
	return cfg, nil
}

// NormalizedEnvironment returns the trimmed, lowercase environment string.
func (c *Config) NormalizedEnvironment() string {
	return strings.ToLower(strings.TrimSpace(c.Environment))
}

// IsDev returns true if the normalized environment is "development".
func (c *Config) IsDev() bool {
	return c.NormalizedEnvironment() == "development"
}

// DBName returns the SQLite database file name based on whether IsDev is true.
func (c *Config) DBName() string {
	if c.IsDev() {
		return "insights_dev.db"
	}
	return "insights.db"
}

// DBPath returns the standard relative path to the SQLite database inside .state/
func (c *Config) DBPath() string {
	return filepath.Join(".state", c.DBName())
}

// CheckConfig validates that all required Twitter API keys are set and non-empty.
// Returns an error with the exact missing variables matching the Python CLI error format.
func (c *Config) CheckConfig() error {
	var missing []string
	if strings.TrimSpace(c.XAPIKey) == "" {
		missing = append(missing, "X_API_KEY")
	}
	if strings.TrimSpace(c.XAPIKeySecret) == "" {
		missing = append(missing, "X_API_KEY_SECRET")
	}
	if strings.TrimSpace(c.XAccessToken) == "" {
		missing = append(missing, "X_ACCESS_TOKEN")
	}
	if strings.TrimSpace(c.XAccessTokenSecret) == "" {
		missing = append(missing, "X_ACCESS_TOKEN_SECRET")
	}

	if len(missing) > 0 {
		return fmt.Errorf("Missing required environment variables: %s. Please check your .env file.", strings.Join(missing, ", "))
	}
	return nil
}

// CheckEmailConfig validates that all required SMTP variables are set for email reporting.
// Returns an error with the exact missing variables matching the Python CLI error format.
func (c *Config) CheckEmailConfig() error {
	var missing []string
	if strings.TrimSpace(c.SMTPUser) == "" {
		missing = append(missing, "SMTP_USER")
	}
	if strings.TrimSpace(c.SMTPPassword) == "" {
		missing = append(missing, "SMTP_PASSWORD")
	}
	if strings.TrimSpace(c.ReportSender) == "" {
		missing = append(missing, "REPORT_SENDER")
	}
	if strings.TrimSpace(c.ReportRecipient) == "" {
		missing = append(missing, "REPORT_RECIPIENT")
	}

	if len(missing) > 0 {
		return fmt.Errorf("Email reporting requires: %s. Please add them to your .env file.", strings.Join(missing, ", "))
	}
	return nil
}
