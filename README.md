# X Agent Framework

This is a command-line tool to manage your X (formerly Twitter) account using a collection of specialized agents. The first available agent is the `unblock` agent, which unblocks every account you have blocked.

The framework is designed to be extensible, allowing for the easy addition of new agents to perform various tasks on your X profile. It uses the X API, automatically handles rate limiting, gracefully skips over accounts that no longer exist, and saves progress for resumable operations.

## Features

*   **Extensible:** Easily add new agents for different tasks.
*   **Resumable:** The `unblock` agent saves its progress. You can stop it at any time and restart it later without losing your place.
*   **Rate Limit Handling:** Automatically pauses and resumes when it hits the X API rate limit.
*   **Robust:** If it encounters an account that has been deleted or suspended, it logs the issue and continues.
*   **Informative Logging:** Provides a running count of actions, the username of the last person interacted with, and the total number remaining.

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
    Install the project in editable mode. This allows you to run the script from anywhere and ensures any changes you make are immediately available.
    ```bash
    uv pip install -e .
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

Once you have completed the setup, you can run the tool using the `x-agent` command followed by the name of the agent you want to use.

To run the unblocker agent:
```bash
uv run x-agent unblock
```

The script will then:
1.  Authenticate with your credentials.
2.  Fetch the complete list of all your blocked accounts and save them to `blocked_ids.txt`.
3.  Begin the unblocking process, saving the ID of each unblocked user to `unblocked_ids.txt`.

If you stop the script and run it again, it will read both files and resume where it left off.

### Important Note on Execution Time

The X API has strict rate limits. The script automatically handles these by pausing when necessary. Please be patient, as the process can take a long time if you have blocked many accounts. For example, unblocking 1,000 accounts will take approximately **5 hours**. The script is designed to run unattended until it completes.