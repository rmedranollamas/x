# Gemini Project Context: X Agent Framework

## Project Overview
Python CLI framework for X (Twitter) account management via modular agents.
Current agent: `unblock` (unblocks blocked accounts, resumable, rate-limit aware).
Architecture: `XService` (API interaction), `BaseAgent` (interface), concrete agents (task logic), `cli.py` (entry point).

## Key Technologies
- Python (uv for dependency management)
- tweepy (X API interaction)
- python-dotenv (credential management)

## Development Conventions
- Modular architecture (services, agents).
- Credentials via `.env`.
- State persistence for resumable tasks.
- Robust error handling (rate limits, non-existent users).
- Informative CLI output.
- Code style: `ruff` formatted, OOP.

## Running the Tool
1. `uv pip install -e .`
2. `cp .env.example .env` (configure credentials)
3. `uv run x-agent unblock` (to run unblock agent)
