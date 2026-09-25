# X Agent Framework

A standalone, high-performance Go command-line tool to manage your X (formerly Twitter) account via modular agents.

Inspired by clean, production-grade Go CLI architecture, `x-agent` requires zero CGO, uses pure Go SQLite persistence, separates stdout/stderr for Unix pipeline composability, and provides resilient API rate-limit recovery.

## Features

- **Pure Go & Zero-CGO**: Single static binary compiled with pure Go SQLite (`modernc.org/sqlite`), eliminating runtime dependencies.
- **Dual API Client**: Lightweight client combining Twitter v1.1 and v2 endpoints with OAuth 1.0a authentication.
- **Intelligent Rate Limiting**: Automatic detection and handling of 15m and 24h rate limit windows with backoff and resume.
- **Zombie Unblock Recovery**: 3-tier recovery state machine (v1.1 destroy -> v2 unblock -> toggle block) to overcome stale block states.
- **Modular Agents**:
  - `insights`: Computes account metrics, follower ratios, vitality scores, and net changes.
  - `unblock`: Mass unblock processing with resilient recovery.
  - `unfollow`: Tracks follower baseline and detects who unfollowed you.
  - `delete`: Multi-tier tweet pruning from live API and X data archives (`tweets.js`).
  - `blocked-ids`: Fast retrieval of blocked account IDs (pipeable to stdout).
  - `db`: Local SQLite schema migrations, inspection, and automated timestamped backups.
- **Email Reporting**: Native Go SMTP reporting for daily insights delivery.
- **Environment Aware**: Toggle between `development` and `production` databases via `X_AGENT_ENV`.
- **Safe**: `--dry-run` simulation mode on state-mutating commands.

## Requirements

- Go 1.23+ (when building from source) or a precompiled binary.
- An X Developer App with Read and Write permissions (v1.1 and v2 access).

## Installation

### From Source

```bash
git clone https://github.com/rmedranollamas/x.git
cd x

# Build local binary (placed at ./x-agent and ./dist/x-agent)
make build

# Or install to your $GOPATH/bin
make install
```

### Multi-Architecture Builds

To build cross-platform binaries for both AMD64 and ARM64:

```bash
make build-all
```

Outputs:

- `dist/x-agent`
- `dist/x-agent-linux-amd64`
- `dist/x-agent-linux-arm64`

## Configuration

Copy `.env.example` to `.env` in the project root:

```bash
cp .env.example .env
```

Configure your credentials:

```env
# X (Twitter) API OAuth 1.0a Credentials
X_API_KEY="your-api-key"
X_API_KEY_SECRET="your-api-secret"
X_ACCESS_TOKEN="your-access-token"
X_ACCESS_TOKEN_SECRET="your-access-token-secret"

# Environment: "development" or "production" (defaults to development)
X_AGENT_ENV=production

# Email Reporting Settings (Required for insights --email)
SMTP_HOST="smtp.gmail.com"
SMTP_PORT=587
SMTP_USER="your-email@example.com"
SMTP_PASSWORD="your-app-password"
REPORT_SENDER="sender@example.com"
REPORT_RECIPIENT="recipient@example.com"
```

## Usage

### Available Agents

#### 1. Account Insights

Gather account metrics and optionally email a summary:

```bash
./x-agent insights
./x-agent insights --email
```

#### 2. Mass Unblock

Unblock all blocked accounts or target a specific user ID:

```bash
./x-agent unblock
./x-agent unblock --user-id 12345678
./x-agent unblock --refresh
./x-agent unblock --dry-run
```

#### 3. Unfollow Detection

Detect accounts that unfollowed you since the baseline was established:

```bash
./x-agent unfollow
./x-agent unfollow --dry-run
```

#### 4. Tweet Deletion & Archive Pruning

Prune tweets using live API scanning or an official X data archive (`tweets.js`):

```bash
./x-agent delete --dry-run
./x-agent delete --archive tweets.js
./x-agent delete --archive tweets.js --max 50
./x-agent delete --protected-id 1234567890
```

#### 5. Blocked IDs

Extract blocked IDs. Designed for Unix stream pipelines (pipeable pure data on stdout):

```bash
./x-agent blocked-ids > blocked_ids.txt
```

#### 6. Database Management

Inspect database paths or trigger automated backups:

```bash
./x-agent db info
./x-agent db backup
```

### Global Flags

- `--dry-run`: Simulate operations without modifying external state (available on `unblock`, `unfollow`, `delete`).
- `--debug`: Enable verbose debug logging to stderr.
- `-h, --help`: Display command documentation.

## Automation

Install a daily 9:00 AM cronjob to automatically generate and email account insights:

```bash
./scripts/setup_cron.sh
```

For automated / non-interactive installation:

```bash
./scripts/setup_cron.sh --yes
```

Logs are appended to `.state/cron.log`.

## Tweet Pruning Rules

The `delete` agent applies multi-tier safety rules:

1. **Grace Period**: Tweets younger than 7 days are never deleted.
1. **Protected Content**: Pinned tweets, threads, and media tweets are preserved.
1. **Age & Engagement Tiers**:
   - **> 30 days**: Prunes low-engagement retweets.
   - **> 365 days**: Prunes unless specifically protected.
   - **Intermediate**: Prunes if engagement falls below dynamic thresholds.

## Development

```bash
# Run static analysis
make vet

# Format code
make fmt

# Run end-to-end test suite
make test-e2e

# Run all quality checks and builds
make all
```