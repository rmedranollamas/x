# Test Infrastructure Specification: X-Agent Pure Go Rewrite

## Overview

This document specifies the architecture, execution model, feature inventory, 4-tier test case taxonomy, and verification semantics for the opaque-box End-to-End (E2E) test suite of the `x-agent` Go rewrite.

The E2E test suite validates the behavior of the compiled CLI binary (`x-agent`) strictly through its external interfaces: CLI arguments, environment variables, standard streams (`stdout`, `stderr`), process exit codes, file system side effects, SQLite database state, and mock Twitter API network interactions.

## Test Architecture & Directory Layout

The E2E test suite resides entirely within `tests/e2e/`:

```
tests/e2e/
├── harness_test.go   # Shared test runner, subprocess execution, mock HTTP server, DB helpers
├── tier1_test.go     # Tier 1: Feature Coverage (>= 25 test cases, >= 5 per feature)
├── tier2_test.go     # Tier 2: Boundary & Corner Cases (>= 25 test cases, >= 5 per feature)
├── tier3_test.go     # Tier 3: Cross-Feature Combinations (>= 5 test cases)
└── tier4_test.go     # Tier 4: Real-World Scenarios (>= 5 test cases)
```

### Opaque-Box Testing Model

The test runner operates without importing internal application packages (`internal/xapi`, `internal/agents`, etc.). Instead:

1. It constructs an isolated temporary working directory (`t.TempDir()`) per test case.
1. It provisions a local mock Twitter API server (`httptest.Server`) with configurable responses for both v1.1 and v2 endpoints.
1. It sets up environment variables (`X_API_KEY`, `X_AGENT_ENV`, `TWITTER_API_BASE_URL` or HTTP proxy, `SMTP_*`) pointing to the test harness.
1. It executes the CLI command via `exec.CommandContext`.
1. It inspects process exit codes, stdout/stderr text patterns, and SQLite database tables/rows via pure Go SQLite queries.

## Runner Invocation

### Running the Entire E2E Test Suite

```bash
# Ensure Go binary path is in PATH
export PATH=/usr/local/go/bin:$PATH

# Run all E2E tests with verbose output
go test -v ./tests/e2e/...
```

### Running Specific Tiers

```bash
# Run Tier 1 Feature Coverage tests
go test -v -run "TestTier1_" ./tests/e2e/...

# Run Tier 2 Boundary & Corner Case tests
go test -v -run "TestTier2_" ./tests/e2e/...

# Run Tier 3 Cross-Feature Interaction tests
go test -v -run "TestTier3_" ./tests/e2e/...

# Run Tier 4 Real-World Scenario tests
go test -v -run "TestTier4_" ./tests/e2e/...
```

### Configuration Flags & Environment Variables

- `X_AGENT_BIN`: Path to pre-compiled `x-agent` binary. If unset or binary does not exist, the test harness automatically compiles `./cmd/x-agent` to a temporary binary. If the Go source is not yet ready, the harness gracefully skips execution with clear instructions.
- `E2E_VERBOSE`: When set to `1` or `true`, prints full stdout and stderr from CLI subprocesses during test runs.

## Feature Inventory & Test Mapping

| Feature ID | Command | Scope | Primary CLI Flags | |---|---|---|---| | F1 | `unblock` | Mass unblock, single unblock, zombie recovery, resumption | `--user-id`, `--refresh`, `--dry-run`, `--debug` | | F2 | `insights` | Account metrics snapshot, follower diff, report generation, email | `--email`, `--debug` | | F3 | `blocked-ids` | Stream blocked user IDs to console for piping | `--debug` | | F4 | `unfollow` | Churn detection, unfollow audit logging, email | `--dry-run`, `--email`, `--debug` | | F5 | `delete` | Engagement-based tweet pruning, archive parsing, live timeline | `--archive`, `--protected-id`, `--dry-run`, `--email`, `--debug` | | F6 | `db` | Database administration and info | `backup`, `info` | | F7 | Root / Global | Environment loading, startup header, credential checks | `--help`, `-h`, `--debug` |

## 4-Tier Test Taxonomy

### Tier 1: Feature Coverage (>= 25 Tests, >= 5 per Feature)

Validates the primary functional paths in isolation for every feature:

- **F1 `unblock`**:

  - `TestTier1_Unblock_Help`: Command help displays flags and descriptions.
  - `TestTier1_Unblock_DryRun_SingleUser`: Simulates unblocking single user ID with `--dry-run`.
  - `TestTier1_Unblock_DryRun_All`: Simulates unblocking all pending accounts with `--dry-run`.
  - `TestTier1_Unblock_RefreshFlag`: Clears pending state and re-syncs from API.
  - `TestTier1_Unblock_Execute_Success`: Real execution updates DB status to `UNBLOCKED`.

- **F2 `insights`**:

  - `TestTier1_Insights_Help`: Command help displays available options.
  - `TestTier1_Insights_FirstRun_EmptyDB`: Generates initial baseline metrics on fresh database.
  - `TestTier1_Insights_FollowerDiff_Display`: Correctly calculates and prints new and lost followers.
  - `TestTier1_Insights_ReportWidth`: Formats console output within 42-character monospace boundary.
  - `TestTier1_Insights_EmailFlag_Accepted`: Accepts `--email` flag when SMTP settings configured.

- **F3 `blocked-ids`**:

  - `TestTier1_BlockedIDs_Help`: Help displays subcommand usage.
  - `TestTier1_BlockedIDs_Stream_Format`: Outputs one numeric ID per line for Unix piping.
  - `TestTier1_BlockedIDs_EmptyList`: Handles zero blocked accounts with clean output and code 0.
  - `TestTier1_BlockedIDs_DebugFlag`: Emits debug logs to stderr while preserving stdout for IDs.
  - `TestTier1_BlockedIDs_ExitCode`: Returns exit code 0 upon successful enumeration.

- **F4 `unfollow`**:

  - `TestTier1_Unfollow_Help`: Command help displays flags and options.
  - `TestTier1_Unfollow_FirstRun`: Populates baseline followers table without logging unfollows.
  - `TestTier1_Unfollow_DryRun`: Calculates churn without mutating SQLite tables.
  - `TestTier1_Unfollow_DetectChurn`: Detects lost followers and logs to `unfollows` table.
  - `TestTier1_Unfollow_EmailDelivery`: Sends churn report when `--email` flag provided.

- **F5 `delete`**:

  - `TestTier1_Delete_Help`: Command help displays flags (`--archive`, `--protected-id`, etc.).
  - `TestTier1_Delete_Archive_DryRun`: Processes `tweets.js` archive in dry-run mode.
  - `TestTier1_Delete_ProtectedIDs_Flag`: Respects multiple `--protected-id` flags.
  - `TestTier1_Delete_GracePeriod`: Spares tweets younger than 7 days (`KEEP [Recent]`).
  - `TestTier1_Delete_Execution_Audit`: Records deleted tweets in `deleted_tweets` table.

### Tier 2: Boundary & Corner Cases (>= 25 Tests, >= 5 per Feature)

Validates edge cases, invalid inputs, missing configurations, rate limits, and network anomalies:

- **Missing Credentials & Config**:

  - `TestTier2_Config_MissingApiKey`: Exits with code 1 and specifies `X_API_KEY`.
  - `TestTier2_Config_MissingSecret`: Exits with code 1 and specifies `X_API_KEY_SECRET`.
  - `TestTier2_Config_MissingToken`: Exits with code 1 and specifies `X_ACCESS_TOKEN`.
  - `TestTier2_Config_MissingTokenSecret`: Exits with code 1 and specifies `X_ACCESS_TOKEN_SECRET`.
  - `TestTier2_Config_MissingSMTP_WithEmail`: Fails fast when `--email` passed without SMTP credentials.

- **F1 `unblock` Boundaries**:

  - `TestTier2_Unblock_InvalidUserID_String`: Rejects non-integer `--user-id "abc"`.
  - `TestTier2_Unblock_NegativeUserID`: Rejects negative user IDs.
  - `TestTier2_Unblock_UserNotFound_404`: Marks user as `NOT_FOUND` in DB when user deleted on X.
  - `TestTier2_Unblock_ZombieRecovery_Success`: 3-tier fallback succeeds after initial 404.
  - `TestTier2_Unblock_NetworkTimeout_Retry`: Retries transient 5xx / timeout errors.

- **F2 `insights` Boundaries**:

  - `TestTier2_Insights_DeactivatedFollowerHandle`: Handles 404 on handle resolution with `(Deactivated)`.
  - `TestTier2_Insights_SuspendedFollowerHandle`: Handles 403 on handle resolution with `(Suspended)`.
  - `TestTier2_Insights_ZeroFollowers`: Computes velocity gracefully with 0 followers.
  - `TestTier2_Insights_NegativeVelocity`: Displays `(Downwards)` velocity format without crash.
  - `TestTier2_Insights_BatchUsers_Over100`: Chunks follower handle resolution beyond 100 IDs.

- **F3 `blocked-ids` Boundaries**:

  - `TestTier2_BlockedIDs_RateLimit_15m`: Honors `x-rate-limit-reset` header.
  - `TestTier2_BlockedIDs_DailyRateLimit_24h`: Honors `x-app-limit-24hour-reset` header.
  - `TestTier2_BlockedIDs_Pagination_LargeSet`: Paginates through cursor-based results.
  - `TestTier2_BlockedIDs_Unauthorized_401`: Exits with code 1 on 401 Unauthorized.
  - `TestTier2_BlockedIDs_CorruptResponse`: Gracefully reports API JSON decode errors.

- **F4 `unfollow` Boundaries**:

  - `TestTier2_Unfollow_NoPreviousFollowers`: Treats fresh start as empty delta.
  - `TestTier2_Unfollow_AllFollowersUnfollowed`: Handles complete follower drop without panic.
  - `TestTier2_Unfollow_UnchangedFollowers`: Handles 0 churn with informative message.
  - `TestTier2_Unfollow_DatabaseReadOnly`: Handles SQLite disk I/O / permissions errors gracefully.
  - `TestTier2_Unfollow_EmailFailure`: Logs SMTP failure without corrupting SQLite transactions.

- **F5 `delete` Boundaries**:

  - `TestTier2_Delete_MalformedArchive_MissingBracket`: Handles archive without `[` gracefully.
  - `TestTier2_Delete_Archive_InvalidDates`: Skips tweets with unparseable date strings.
  - `TestTier2_Delete_TweetOver365Days_NoProtection`: Deletes tweets older than 365 days regardless of media.
  - `TestTier2_Delete_OldRetweetOver30Days`: Deletes retweets older than 30 days.
  - `TestTier2_Delete_AlreadyDeleted_Skip`: Skips tweets already recorded in `deleted_tweets`.

### Tier 3: Cross-Feature Interactions (>= 5 Tests)

Validates system cohesion across multiple components and multi-step workflows:

- `TestTier3_Unblock_And_BlockedIDs_Sync`: Running `unblock` successfully updates DB state; subsequent `blocked-ids` queries reflect updated list.
- `TestTier3_Insights_AutoMigrates_LegacyDB`: Running `insights` against a legacy v1 SQLite database applies migrations m001 through m004 and records `listed_count`.
- `TestTier3_Delete_Archive_With_LiveTimeline_Fallback`: Runs `delete` on an archive file, spares pinned tweet discovered via live v2 `users/me`.
- `TestTier3_Insights_And_Unfollow_StateConsistency`: Running `insights` and then `unfollow` in succession shares identical follower snapshot state in SQLite.
- `TestTier3_DryRun_ZeroMutationsAcrossCommands`: Executing `unblock --dry-run`, `unfollow --dry-run`, and `delete --dry-run` guarantees zero rows inserted into `blocked_users`, `unfollows`, or `deleted_tweets`.

### Tier 4: Real-World Scenarios (>= 5 Scenarios)

Validates complete operational scenarios:

- `TestTier4_Scenario1_FreshOnboarding`: First-time setup with new database, running `insights` creates tables, executes migrations, stores snapshot, and outputs startup banner.
- `TestTier4_Scenario2_DailyMaintenanceWorkflow`: Sequential execution of `insights` -> `unfollow` -> `unblock --dry-run` -> `db backup` with audit trail verification.
- `TestTier4_Scenario3_ArchivePruningCycle`: Archive cleanup with `tweets.js`, `--protected-id`, verifying checkpointing on repeat run.
- `TestTier4_Scenario4_ZombieBlockRecoveryCycle`: Complete zombie unblock recovery loop verifying 3-tier fallback and DB transition to `UNBLOCKED`.
- `TestTier4_Scenario5_DevVsProdEnvironmentSwitch`: Toggling `X_AGENT_ENV=development` vs `X_AGENT_ENV=production` switches SQLite targets (`insights_dev.db` vs `insights.db`) with corresponding colored banner outputs.

## Pass/Fail Semantics

A test case is marked **PASS** if and only if all of the following conditions are satisfied:

1. **Exit Code**: The subprocess exit code matches expected (0 for success, 1 for fatal errors).
1. **Standard Error (`stderr`)**: Error messages appear on stderr; fatal errors contain expected diagnostic substrings.
1. **Standard Output (`stdout`)**:
   - Startup header `Environment: ... | Database: ...` is printed when credentials are valid and `--help` is not passed.
   - Subcommand output (e.g. 42-char wide report, blocked IDs list) matches specification.
1. **Database State**:
   - SQLite tables and columns match migration schema.
   - For `--dry-run`, no rows are inserted or modified.
   - For real operations, expected rows are inserted with valid UTC timestamps.
1. **No Panics or Leaks**: Subprocess completes within timeout (default 30 seconds) without segmentation faults, panics, or unhandled exceptions.
