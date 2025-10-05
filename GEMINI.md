# Gemini Project Context: X Agent Framework

## Project Overview

This project is a Python-based command-line framework for managing an X (formerly Twitter) account through specialized agents. It has been architected to be extensible, allowing for the addition of new agents to handle various social media tasks.

The initial implementation includes an `unblock` agent that unblocks all currently blocked accounts on a user's profile.

The core architecture is composed of:
-   **`XService`**: A centralized service that encapsulates all `tweepy` API interactions, including authentication and rate-limit handling.
-   **Agents**: Modular classes (e.g., `UnblockAgent`) that inherit from a `BaseAgent` and contain the logic for a specific task.
-   **CLI**: A command-line interface (`cli.py`) that serves as the main entry point for selecting and running agents.

The framework persists state for long-running tasks (like the unblocker) to allow for safe resumption.

## Building and Running

This is a Python project managed with `uv`. The primary dependencies are `tweepy` for X API interaction and `python-dotenv` for managing credentials.

**1. Setup:**

```bash
# Install the project and its dependencies in editable mode
uv pip install -e .

# Configure credentials
cp .env.example .env
# TODO: Edit .env and add your X API credentials.
```

**2. Running the Tool:**

Once the setup is complete, the tool can be run using the `x-agent` command, specifying the desired agent.

To run the unblocker:
```bash
uv run x-agent unblock
```

There are no build steps or tests included in this project.

## Development Conventions

*   **Modular Architecture**: The codebase is separated into services (for API interaction) and agents (for specific tasks) to promote code reuse and extensibility.
*   **Credentials Management**: API keys and secrets are managed via a `.env` file.
*   **State Persistence**: Agents can save their progress to local files (e.g., `blocked_ids.txt`) to allow for resumption.
*   **Error Handling**: The `XService` handles common API errors, including rate limits and non-existent users.
*   **User Experience**: The CLI provides clear, single-line progress updates and countdown timers for rate-limit pauses.
*   **Code Style**: The code is formatted with `ruff` and follows a class-based, object-oriented structure.