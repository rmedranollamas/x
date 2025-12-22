# Gemini Project Context: X Agent Framework

## Project Overview
Python CLI framework for X (Twitter) account management via modular agents.

## Architecture
- **XService:** Central service for all X API interactions (v1.1 and v2). Uses `tweepy.asynchronous` where possible.
- **BaseAgent:** Abstract base class for all agents.
- **Agents:**
    - `unblock`: Mass unblocks accounts with ghost/zombie protection.
    - `insights`: Tracks daily follower/following metrics.
    - `blocked-ids`: Lists all currently blocked IDs.
    - `unfollow`: Mass unfollows accounts (e.g., non-followers).
- **Persistence:** SQLite database (`.state/insights.db`) stores metrics and task statuses.
- **CLI:** `Typer`-based entry point (`x-agent`).

## Key Technologies
- Python 3.13+
- `tweepy` & `tweepy.asynchronous`
- `typer` (CLI)
- `pydantic-settings` (Configuration)
- `sqlite3` (Persistence)
- `uv` (Dependency management)

## Development Conventions
- Asynchronous first: all agents and services use `asyncio`.
- Safe DB handling: use `db_transaction` context manager.
- Fail-fast config: missing credentials stop the app at startup.
- Robust error handling: specific Tweepy exceptions caught.
- Informative CLI output with real-time updates.
- Code style: `ruff` formatted, type-hinted.

## Running the Tool
1. `uv sync`
2. `cp .env.example .env`
3. `uv run x-agent [AGENT]`