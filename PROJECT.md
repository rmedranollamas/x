# Project: x-agent Pure Go Rewrite

## Architecture

- **`cmd/x-agent/`**: Entry point binary, root Cobra CLI, startup environment & DB visibility headers, subcommand routing.
- **`internal/config/`**: Environment loader using `joho/godotenv` and `caarlos0/env/v11`, fail-fast credential validations, database filename/path resolution.
- **`internal/logging/`**: ANSI color styling, log level configuration, stderr logging with stdout streaming preservation, single-line progress updates for TTYs.
- **`internal/db/`**: Pure Go SQLite manager using `modernc.org/sqlite` (zero CGO), schema migrations m001–m004 tracked in `schema_versions`, pre-migration backup routines, transactional helpers, UTC datetime handling.
- **`internal/xapi/`**: Twitter API client supporting v1.1 and v2 endpoints via `dghubble/oauth1` and `net/http`, 15m vs 24h rate limit handling, 3-tier zombie unblock recovery, exponential backoff retries.
- **`internal/agents/`**: Concrete agent implementations implementing the unified `Agent` interface: `unblock`, `insights`, `blocked-ids`, `unfollow`, `delete`.
- **`tests/e2e/`**: Opaque-box requirement-driven test harness and test suites across Tiers 1–4.

## Feature Inventory

| # | Feature | Description | Milestone | Source | | --- | --- | --- | --- | --- | | 1 | Go Module & Pure Go Foundation | Initialize `github.com/rmedranollamas/x-agent` with zero CGO dependencies | M1 | Survey | | 2 | Configuration Loader | Load config from `.env` and environment variables | M1 | Survey | | 3 | Core API Credential Validation | Fail-fast validation of required Twitter API keys with exact error messages | M1 | Survey | | 4 | Email Credential Validation | Fail-fast validation of SMTP credentials when email delivery requested | M1 | Survey | | 5 | Environment & DB Path Resolution | Case-insensitive environment normalization, `insights_dev.db` vs `insights.db` | M1 | Survey | | 6 | Startup Visibility Headers | Output `Environment: <ENV> | Database: <DB>` with ANSI color coding | M1 | Survey | | 7 | Root Cobra CLI & Flag Parsing | Root command with `--debug`, `-h/--help`, and subcommand wiring | M1 | Survey | | 8 | SQLite Connection Manager | Pure Go SQLite engine using `modernc.org/sqlite`, connection pool with max 1 conn | M2 | Survey | | 9 | Pre-Migration Database Backup | Auto-backup to `.state/backups/{stem}_{YYYYMMDD_HHMMSS}.db` before migrations | M2 | Survey | | 10 | Schema Migration Runner | Version tracking table `schema_versions` with idempotent execution | M2 | Survey | | 11 | Migration m001 (Initial Schema) | Tables `insights`, `blocked_users`, `following_users`, `followers`, `unfollows` | M2 | Survey | | 12 | Migration m002 (Listed Count) | Add column `listed_count` to `insights` | M2 | Survey | | 13 | Migration m003 (Deleted Tweets) | Create `deleted_tweets` table for tweet deletion auditing | M2 | Survey | | 14 | Migration m004 (Engagement Score) | Rename column `views` to `engagement_score` in `deleted_tweets` | M2 | Survey | | 15 | UTC Datetime Conventions | Match SQLite `STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')` format and queries | M2 | Survey | | 16 | Repository Operations | Type-safe CRUD operations and upsert semantics across all 7 tables | M2 | Survey | | 17 | Real Database Backward Compatibility | 100% read/write compatibility with `/home/pi/x/.state/insights.db` | M2 | Survey | | 18 | Dual-Protocol HTTP Client | OAuth 1.0a client for Twitter v1.1 and v2 endpoints via `dghubble/oauth1` | M3 | Survey | | 19 | Twitter API v1.1 Endpoints | Implement 8 v1.1 endpoints (blocks, users, friends, followers, timeline, etc.) | M3 | Survey | | 20 | Twitter API v2 Endpoints | Implement 7 v2 endpoints (users/me, users batch, blocking, delete tweet, etc.) | M3 | Survey | | 21 | Transient Error Retries | Exponential backoff on 5xx, timeout, connection, and transient errors | M3 | Survey | | 22 | 15m Rolling Window Rate Limits | Resets on `x-rate-limit-reset` + 5s buffer, 30m fallback for past timestamps | M3 | Survey | | 23 | 24h Daily Quota Handling | Parse `x-app-limit-24hour-*` headers and wait until daily reset + 60s | M3 | Survey | | 24 | 3-Tier Zombie Block Recovery | Destroy v1 -> V2 unblock -> V1 toggle -> V2 toggle -> Mark NOT_FOUND | M3 | Survey | | 25 | Mock HTTP Transport Test Harness | Unit tests for all API endpoints and resilience routines with mock HTTP | M3 | Survey | | 26 | Unified Agent Interface | Common interface `Run(ctx context.Context) error` for all 5 agents | M4 | Survey | | 27 | UnblockAgent Implementation | Batching (50), concurrency (20), DB state tracking, `--user-id`, `--refresh`, `--dry-run` | M4 | Survey | | 28 | InsightsAgent Implementation | Follower diff, velocity calculation, 42-char report, email delivery | M4 | Survey | | 29 | BlockedIDsAgent Implementation | Fetch and stream blocked IDs directly to stdout for Unix piping | M4 | Survey | | 30 | UnfollowAgent Implementation | Follower churn detection, event logging, email delivery, `--dry-run` | M4 | Survey | | 31 | DeleteAgent Implementation | Archive parser, live timeline pagination, 7 deletion rules, audit log | M4 | Survey | | 32 | Database Management Commands | Implement `x-agent db backup` and `x-agent db info` subcommands | M4 | Survey | | 33 | CLI Subcommand Integration | Wire all 5 agents and db commands into root Cobra application | M4 | Survey | | 34 | Interactive Progress Logging | Single-line progress logging (`\r`) on TTY streams | M4 | Survey | | 35 | E2E Test Suite (Tiers 1–4) | Requirement-driven test suite created by E2E Testing Track | M5 | Testing Track | | 36 | Adversarial Coverage Hardening | Tier 5 white-box coverage hardening with challenger | M5 | Testing Track | | 37 | Pure Go Standalone Binary Build | Single binary compilation via `CGO_ENABLED=0 go build` | M5 | Survey | | 38 | Cross-Compilation | Build cleanly for `GOOS=linux GOARCH=arm64` and `GOOS=linux GOARCH=amd64` | M5 | Survey | | 39 | Static Analysis & Code Formatting | Zero warnings on `go vet ./...` and standard `gofmt -s -w .` | M5 | Survey |

## Milestones

| # | Name | Scope | Dependencies | Status | | --- | --- | --- | --- | --- | | M1 | Architecture, CLI Foundation & Config | `cmd/x-agent/`, `internal/config/`, `internal/logging/` | None | DONE | | M2 | SQLite Persistence & Migrations | `internal/db/` (schema, migrations m001-m004, compatibility) | M1 | DONE | | M3 | Dual-API Service (xapi) | `internal/xapi/` (v1.1 & v2 endpoints, rate limits, zombie recovery) | M1 | DONE | | M4 | Concrete Agents & CLI Wiring | `internal/agents/`, `cmd/x-agent/` subcommands, agent integration | M1, M2, M3 | DONE | | M5 | E2E Tests, Quality Gate & Cross-Compilation | 100% E2E tests, Tier 5 hardening, `go vet`, `gofmt`, ARM64/AMD64 cross-compile | M4, TEST_READY.md | DONE |

## Interface Contracts

### `internal/config` ↔ All Components

```go
type Config struct {
    XAPIKey            string
    XAPIKeySecret      string
    XAccessToken       string
    XAccessTokenSecret string
    Environment        string
    SMTPHost           string
    SMTPPort           int
    SMTPUser           string
    SMTPPassword       string
    ReportSender       string
    ReportRecipient    string
    SMTPUseTLS         bool
    SMTPStartTLS       bool
}

func Load() (*Config, error)
func (c *Config) NormalizedEnvironment() string
func (c *Config) IsDev() bool
func (c *Config) DBName() string
func (c *Config) DBPath() string
func (c *Config) CheckConfig() error
func (c *Config) CheckEmailConfig() error
```

### `internal/db` ↔ Agents

```go
type DBManager struct { ... }

func NewDBManager(dbPath string) (*DBManager, error)
func (m *DBManager) Close() error
func (m *DBManager) RunMigrations() error
func (m *DBManager) BackupDatabase() (string, error)

// Repository methods for all 7 tables:
// Insights: AddInsight, GetLatestInsight, GetInsightAtOffset
// BlockedUsers: UpsertBlockedUsers, GetPendingBlockedUsers, UpdateBlockedUserStatus, ClearPendingBlockedUsers
// Followers & Following: ReplaceFollowers, GetFollowerIDs, SyncFollowing
// Unfollows: LogUnfollows, GetRecentUnfollows
// DeletedTweets: AddDeletedTweet, GetDeletedTweetIDs
```

### `internal/xapi` ↔ Agents

```go
type XClient interface {
    GetMe(ctx context.Context) (*User, error)
    GetBlockedUserIDs(ctx context.Context) ([]int64, error)
    UnblockUser(ctx context.Context, userID int64) (string, error)
    UnfollowUser(ctx context.Context, userID int64) (string, error)
    GetFollowerIDs(ctx context.Context) ([]int64, error)
    GetFriendIDs(ctx context.Context) ([]int64, error)
    GetUsersBatch(ctx context.Context, userIDs []int64) (map[int64]*User, error)
    GetUserTimeline(ctx context.Context, userID int64, count int, maxID int64) ([]*Tweet, error)
    DeleteTweet(ctx context.Context, tweetID int64) (bool, error)
}
```

### `internal/agents` ↔ `cmd/x-agent`

```go
type Agent interface {
    Run(ctx context.Context) error
}
```

## Code Layout

```
.
├── cmd/
│   └── x-agent/
│       ├── main.go
│       ├── root.go
│       ├── unblock.go
│       ├── insights.go
│       ├── blocked_ids.go
│       ├── unfollow.go
│       ├── delete.go
│       └── db.go
├── internal/
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── logging/
│   │   ├── logger.go
│   │   └── header.go
│   ├── db/
│   │   ├── manager.go
│   │   ├── migrations.go
│   │   ├── operations.go
│   │   └── db_test.go
│   ├── xapi/
│   │   ├── client.go
│   │   ├── v1.go
│   │   ├── v2.go
│   │   ├── resilience.go
│   │   ├── zombie.go
│   │   └── xapi_test.go
│   └── agents/
│       ├── agent.go
│       ├── unblock.go
│       ├── insights.go
│       ├── blocked_ids.go
│       ├── unfollow.go
│       ├── delete.go
│       └── agents_test.go
├── tests/
│   └── e2e/
│       ├── harness_test.go
│       ├── tier1_test.go
│       ├── tier2_test.go
│       ├── tier3_test.go
│       └── tier4_test.go
├── go.mod
├── go.sum
├── Makefile
└── PROJECT.md
```
