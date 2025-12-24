import sys
import logging
import typer
import asyncio
from typing import Optional
from .services.x_service import XService
from .agents.unblock_agent import UnblockAgent
from .agents.insights_agent import InsightsAgent
from .agents.blocked_ids_agent import BlockedIdsAgent
from .agents.unfollow_agent import UnfollowAgent
from .logging_setup import setup_logging

app = typer.Typer(
    name="x-agent",
    help="A command-line tool to manage X interactions with agents.",
    add_completion=False,
)


def _run_agent(agent_class, debug: bool, **kwargs):
    """
    Helper to initialize service and run an agent.
    """
    setup_logging(debug)
    try:
        x_service = XService()
        agent = agent_class(x_service, **kwargs)
        asyncio.run(agent.execute())
    except Exception as e:
        logging.error(f"An unexpected error occurred: {e}", exc_info=True)
        sys.exit(1)


@app.command()
def unblock(
    user_id: Optional[int] = typer.Option(
        None, "--user-id", help="Optional: Specify a single user ID to unblock."
    ),
    refresh: bool = typer.Option(
        False, "--refresh", help="Re-fetch blocked IDs from API, ignoring local cache."
    ),
    debug: bool = typer.Option(
        False, "--debug", help="Enable debug logging for detailed output."
    ),
):
    """
    Run the unblock agent to unblock blocked accounts.
    """
    _run_agent(UnblockAgent, debug, user_id=user_id, refresh=refresh)


@app.command()
def insights(
    debug: bool = typer.Option(
        False, "--debug", help="Enable debug logging for detailed output."
    ),
):
    """
    Run the insights agent to gather and report account metrics.
    """
    _run_agent(InsightsAgent, debug)


@app.command()
def unfollow(
    non_followers: bool = typer.Option(
        True, help="Only unfollow users who don't follow you back."
    ),
    refresh: bool = typer.Option(
        False,
        "--refresh",
        help="Re-fetch following/follower IDs from API, ignoring local cache.",
    ),
    debug: bool = typer.Option(
        False, "--debug", help="Enable debug logging for detailed output."
    ),
):
    """
    Run the unfollow agent to clean up your following list.
    """
    _run_agent(UnfollowAgent, debug, non_followers_only=non_followers, refresh=refresh)


@app.command(name="blocked-ids")
def blocked_ids(
    debug: bool = typer.Option(
        False, "--debug", help="Enable debug logging for detailed output."
    ),
):
    """
    Run the blocked-ids agent to fetch and print blocked user IDs.
    """
    _run_agent(BlockedIdsAgent, debug)


def main():
    app()


if __name__ == "__main__":
    main()
