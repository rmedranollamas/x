# Gemini Project Context: X Unblocker

## Project Overview

This project contains a Python command-line tool designed to unblock all blocked accounts on the user's X (formerly Twitter) profile. It interacts directly with the X API v2.

The main logic is encapsulated in `unblocker.py`. The script authenticates using user-provided credentials, fetches a complete list of blocked accounts, and then iterates through that list, unblocking each user one by one.

A key feature of this tool is its built-in handling of X API rate limits. The script will automatically pause for 15 minutes after every 50 unblock operations to avoid being rate-limited, making it suitable for users with a large number of blocked accounts.

## Building and Running

This is a Python project. The primary dependencies are `tweepy` for X API interaction and `python-dotenv` for managing credentials.

**1. Setup:**

It is recommended to use a Python virtual environment, specifically in the `.venv` directory.

```bash
# Create and activate the virtual environment in .venv
python -m venv .venv
source .venv/bin/activate

# Install dependencies
pip install -r requirements.txt

# Configure credentials
cp .env.example .env
# TODO: Edit .env and add your X API credentials.
```

**2. Running the Tool:**

Once the setup is complete, the tool can be run with the following command:

```bash
python unblocker.py
```

There are no build steps or tests included in this project.

## Development Conventions

*   **Credentials Management:** API keys and secrets are managed via a `.env` file and should not be hard-coded. The `.env.example` file serves as a template.
*   **Error Handling:** The script includes basic error handling for missing credentials and API authentication failures, exiting gracefully with informative messages.
*   **User Experience:** The script provides clear feedback to the user, including progress indicators (`.`), countdown timers for rate-limit pauses, and a final summary of unblocked accounts.
*   **Code Style:** The code is procedural, with a clear `main` function as the entry point. It uses standard Python libraries and follows basic PEP 8 conventions.
