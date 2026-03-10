# X Agent Framework

This is a command-line tool to manage your X (formerly Twitter) account using a collection of specialized agents.

The framework is designed to be extensible, allowing for the easy addition of new agents to perform various tasks on your X profile. It uses the X API, automatically handles rate limiting, and saves progress in a local SQLite database for resumable operations.

## Features

*   **Extensible:** Easily add new agents for different tasks.
*   **Asynchronous:** Uses `asyncio` for concurrent API interactions.
*   **Email Reporting:** The `insights` agent can automatically email reports via SMTP.
*   **Resumable:** Progress is saved in an SQLite database.
*   **Environment Aware:** Supports separate development and production databases using `X_AGENT_ENV`.
*   **Rate Limit Handling:** Automatically handles X API rate limits with built-in wait-and-resume logic.
*   **Robust:** Gracefully handles deleted, suspended, or missing accounts.
*   **Resilient:** Includes automatic retries for transient network errors.
*   **Safe:** Validates configuration on startup and offers a `--dry-run` mode.
*   **Modern CLI:** Built with `Typer` with clear visibility into which database/environment is active.

## Requirements

*   Python 3.13+
*   An X Developer Account with an App that has v1.1 and v2 API access.

## Setup Instructions

1.  **Clone the Repository:**
    ```bash
    git clone https://github.com/rmedranollamas/x.git
    cd x
    ```

2.  **Install `uv`:**
    This project uses `uv` for dependency management.
    ```bash
    pip install uv
    ```

3.  **Install Dependencies:**
    ```bash
    uv sync
    ```

4.  **Set Up Your Credentials:**
    *   Copy the example `.env.example` file to a new `.env` file: `cp .env.example .env`
    *   Open the `.env` file and fill in your X API credentials and SMTP settings for email reporting.

    ```env
    # X API
    X_API_KEY="..."
    X_API_KEY_SECRET="..."
    X_ACCESS_TOKEN="..."
    X_ACCESS_TOKEN_SECRET="..."

    # Optional: Environment (defaults to development)
    X_AGENT_ENV=production

    # Email (Required for --email flag)
    SMTP_HOST="smtp.gmail.com"
    SMTP_PORT=587
    SMTP_USER="your-email@example.com"
    SMTP_PASSWORD="your-app-password"
    REPORT_SENDER="sender@example.com"
    REPORT_RECIPIENT="recipient@example.com"
    ```

## How to Run

### Available Agents

*   **Insights:** Gathers and reports account metrics.
    ```bash
    uv run x-agent insights [--email]
    ```

*   **Unblocker:** Mass unblocks accounts.
    ```bash
    uv run x-agent unblock [--user-id ID] [--refresh]
    ```

*   **Unfollow:** Detects who has unfollowed you since the last run.
    ```bash
    uv run x-agent unfollow
    ```

*   **Delete:** Removes old tweets based on age and engagement rules. Supports live API and X archive files.
    ```bash
    uv run x-agent delete [--archive tweets.js] [--protected-id ID] [--dry-run]
    ```

*   **Blocked IDs:** Lists all currently blocked user IDs.
    ```bash
    uv run x-agent blocked-ids
    ```

### Automation

You can set up a daily automated report using the included cron setup helper:
```bash
python3 scripts/setup_cron.py
```
This will install a daily cronjob (default 9:00 AM) that runs the insights agent with email reporting enabled.

### Global Options

Use `--dry-run` to simulate actions without applying them (available for `unblock`, `unfollow`, and `delete`):
```bash
uv run x-agent unblock --dry-run
```

Use `--debug` with any command for detailed logging:
```bash
uv run x-agent insights --debug
```

### Database Management

The framework includes utilities for managing the local SQLite state:

*   **Info:** Show current environment and database path.
    ```bash
    uv run x-agent db info
    ```

*   **Backup:** Create a timestamped backup of the database in `.state/backups/`.
    ```bash
    uv run x-agent db backup
    ```

## Deletion Rules

The `delete` agent uses a multi-tier rule system to decide what to keep:

1.  **Grace Period:** Never deletes anything younger than 7 days.
2.  **Protected Content:** Keeps pinned tweets, threads, and media.
3.  **Engagement Thresholds:**
    *   Older than 30 days: Deletes low-engagement retweets.
    *   Older than 365 days: Deletes unless specifically protected.
    *   In-between: Deletes if likes + retweets fall below a threshold (higher for top-level tweets than replies).

## Architecture

*   **Service Layer:** Robust wrapper around Tweepy that handles both v1.1 and v2 APIs with automated rate-limit recovery and retry logic.
*   **Agent Layer:** Modular architecture where each task is encapsulated in its own agent.
*   **Database Manager:** Centralized state management with SQLite, including an automated migration system for schema updates.

For more information, use the `--help` flag:
```bash
uv run x-agent --help
```