# X Unblocker Tool

This is a simple command-line tool to unblock every account you have blocked on X (formerly Twitter).

It uses the X API v2 and handles rate limiting automatically.

## Requirements

*   Python 3.6+
*   An X Developer Account with an App that has v2 API access.

## Setup Instructions

1.  **Clone the Repository (or download the files):**
    ```bash
    git clone <repository_url>
    cd <repository_directory>
    ```

2.  **Install Dependencies:**
    It's recommended to use a virtual environment to keep dependencies isolated.
    ```bash
    # Create a virtual environment (optional but recommended)
    python -m venv venv
    source venv/bin/activate  # On Windows, use `venv\Scripts\activate`

    # Install the required libraries
    pip install -r requirements.txt
    ```

3.  **Set Up Your Credentials:**
    You need to provide your X API credentials for the tool to work.
    *   Find the `.env.example` file in the directory.
    *   **Rename** it to `.env`.
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
python unblocker.py
```

The script will then:
1.  Authenticate with your credentials.
2.  Fetch the list of all your blocked accounts.
3.  Begin the unblocking process one by one.

### Important Note on Execution Time

The X API allows a maximum of **50 unblocks every 15 minutes**. The script automatically handles this rate limit by pausing for 15 minutes after every 50 unblocks.

Please be patient, as the process can take a long time if you have blocked many accounts. For example, unblocking 1,000 accounts will take approximately **5 hours**. The script is designed to run unattended until it completes.
