# X Agent Framework

This is a command-line tool to manage your X (formerly Twitter) account using a collection of specialized agents.

The framework is designed to be extensible, allowing for the easy addition of new agents to perform various tasks on your X profile. It uses the X API, automatically handles rate limiting, and saves progress in a local SQLite database for resumable operations.

## Features

*   **Extensible:** Easily add new agents for different tasks.
*   **Asynchronous:** Uses `asyncio` for concurrent API interactions.
*   **Resumable:** Progress is saved in an SQLite database. You can stop and restart at any time without losing your place.
*   **Rate Limit Handling:** Automatically handles X API rate limits with built-in wait-and-resume logic.
*   **Robust:** Gracefully handles deleted, suspended, or missing accounts.
*   **Modern CLI:** Built with `Typer` for an intuitive command-line experience.

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
    This project uses `uv` for dependency management. You can install it with:
    ```bash
    pip install uv
    ```

3.  **Install Dependencies:**
    Install the project and synchronize the environment:
    ```bash
    uv sync
    ```

4.  **Set Up Your Credentials:**
    *   Copy the example `.env.example` file to a new `.env` file: `cp .env.example .env`
    *   Open the `.env` file and replace the placeholder values with your actual credentials from your X Developer App.

    Your `.env` file should look like this:
    ```
    X_API_KEY="YOUR_REAL_API_KEY"
    X_API_KEY_SECRET="YOUR_REAL_API_KEY_SECRET"
    X_ACCESS_TOKEN="YOUR_REAL_ACCESS_TOKEN"
    X_ACCESS_TOKEN_SECRET="YOUR_REAL_ACCESS_TOKEN_SECRET"
    ```

## How to Run

Use the `x-agent` command followed by the agent you want to run.

### Available Agents

*   **Unblocker:** Unblocks all blocked accounts.
    ```bash
    uv run x-agent unblock
    ```
    To unblock a specific user ID:
    ```bash
    uv run x-agent unblock --user-id 123456789
    ```

*   **Unfollow:** Manages your following list. By default, it targets accounts that do not follow you back.
    ```bash
    uv run x-agent unfollow
    ```

*   **Insights:** Gathers and reports daily follower/following metrics.
    ```bash
    uv run x-agent insights
    ```

*   **Blocked IDs:** Lists all currently blocked user IDs.
    ```bash
    uv run x-agent blocked-ids
    ```

### Global Options

Use `--debug` with any command for detailed logging:
```bash
uv run x-agent unblock --debug
```

For more information, use the `--help` flag:
```bash
uv run x-agent --help
```

### Important Note on Execution Time

The X API has strict rate limits. The script automatically handles these by pausing when necessary. For mass unblocking, please be patient as the process can take several hours depending on the number of accounts. The script is designed to run unattended until completion.
