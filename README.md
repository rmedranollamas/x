# X Unblocker Tool

This is a simple command-line tool to unblock every account you have blocked on X (formerly Twitter).

It uses the X API v1.1 for unblocking and fetching blocked accounts, and the v2 API for authentication. It automatically handles rate limiting, gracefully skips over accounts that no longer exist, and saves your progress.

## Features

*   **Resumable:** The script saves its progress. You can stop it at any time and restart it later without losing your place.
*   **Rate Limit Handling:** Automatically pauses and resumes when it hits the X API rate limit.
*   **Robust:** If it encounters an account that has been deleted or suspended, it logs the issue and continues.
*   **Informative Logging:** Provides a running count of unblocked accounts, the username of the last person unblocked, and the total number remaining.

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
    ```bash
    uv pip sync
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

Once you have completed the setup, you can run the tool with the following command:

```bash
uv run python unblocker.py
```

The script will then:
1.  Authenticate with your credentials.
2.  Fetch the complete list of all your blocked accounts and save them to `blocked_ids.txt`.
3.  Begin the unblocking process, saving the ID of each unblocked user to `unblocked_ids.txt`.

If you stop the script and run it again, it will read both files and resume where it left off.

### Important Note on Execution Time

The X API has strict rate limits. The script automatically handles these by pausing when necessary. Please be patient, as the process can take a long time if you have blocked many accounts. For example, unblocking 1,000 accounts will take approximately **5 hours**. The script is designed to run unattended until it completes.