# Gemini Project Context: X Agent Framework

## Project Overview

Python CLI framework for X (Twitter) account management via modular agents.

## Architecture

- **XService:** Central service for all X API interactions (v1.1 and v2). Uses `tweepy.asynchronous` where possible.
- **BaseAgent:** Abstract base class for all agents.
- **Agents:**
  - `unblock`: Mass unblocks accounts with ghost/zombie protection.
  - `insights`: Tracks metrics and generates reports. Supports `--email` for automated delivery.
  - `blocked-ids`: Lists all currently blocked IDs.
  - `unfollow`: Mass unfollows accounts (e.g., non-followers).
  - `delete`: Removes old tweets based on age and engagement (views). Supports `--dry-run`.
- **Persistence:** Environment-aware SQLite database (`.state/insights.db` for production, `insights_dev.db` for development).
- **CLI:** `Typer`-based entry point (`x-agent`) with environment/DB visibility headers.
- **Automation:** `scripts/setup_cron.py` helper for installing daily cronjobs.

## Key Technologies

- Python 3.13+
- `tweepy` & `tweepy.asynchronous`
- `aiosmtplib` (Async email reporting)
- `typer` (CLI)
- `pydantic-settings` (Configuration)
- `sqlite3` (Persistence)
- `uv` (Dependency management)

## Development Conventions

- Asynchronous first: all agents and services use `asyncio`.
- Safe DB handling: use `transaction` context manager in `DatabaseManager`.
- Fail-fast config: missing credentials or SMTP settings (when using `--email`) stop the app.
- Environment-aware: `X_AGENT_ENV` toggles between dev and production databases.
- Informative CLI output: Shows active environment and DB file on every run.
- Code style: `ruff` formatted, type-hinted, verified with `ty`.

## Running the Tool

1. `uv sync`
1. `cp .env.example .env` (Add SMTP settings for email reporting)
1. `uv run x-agent [AGENT] [--email] [--debug]`
1. `python3 scripts/setup_cron.py` to automate.
