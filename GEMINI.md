# Gemini Project Context: X Unblocker

## Project Overview

This project contains a Python command-line tool designed to unblock all blocked accounts on the user's X (formerly Twitter) profile. It uses the X API v1.1 to fetch blocked user IDs and perform the unblock operations, and the v2 API for an initial authentication check.

The main logic is encapsulated in `unblocker.py`. The script authenticates using user-provided credentials, fetches a complete list of blocked accounts, and then iterates through that list, unblocking each user one by one.

A key feature of this tool is its built-in handling of X API rate limits. The script will automatically pause when it hits a rate limit and resume when the window resets.

The script also persists its state in `blocked_ids.txt` (the full list of blocked accounts) and `unblocked_ids.txt` (a running list of accounts that have been successfully unblocked). This allows the script to be stopped and restarted without losing progress.

## Building and Running

This is a Python project managed with `uv`. The primary dependencies are `tweepy` for X API interaction and `python-dotenv` for managing credentials.

**1. Setup:**

```bash
# Install dependencies
uv pip install -r requirements.txt

# Configure credentials
cp .env.example .env
# TODO: Edit .env and add your X API credentials.
```

**2. Running the Tool:**

Once the setup is complete, the tool can be run with the following command:

```bash
uv run python unblocker.py
```

There are no build steps or tests included in this project.

## Development Conventions

*   **Credentials Management:** API keys and secrets are managed via a `.env` file and should not be hard-coded. The `.env.example` file serves as a template.
*   **State Persistence:** The script saves the full list of blocked IDs and the list of successfully unblocked IDs to local text files (`blocked_ids.txt`, `unblocked_ids.txt`) to allow for resumption.
*   **Error Handling:** The script includes error handling for missing credentials, API authentication failures, and cases where a blocked user no longer exists.
*   **User Experience:** The script provides clear, single-line progress updates, including the username of the unblocked account and an accurate count of remaining users. It also displays countdown timers for rate-limit pauses.
*   **Code Style:** The code is procedural, with a clear `main` function as the entry point. It uses standard Python libraries and is formatted with `ruff`.