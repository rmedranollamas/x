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
from .config import settings
from .database import DatabaseManager

app = typer.Typer(
    name="x-agent",
    help="A command-line tool to manage X interactions with agents.",
    add_completion=False,
)

db_app = typer.Typer(help="Database management commands.")
app.add_typer(db_app, name="db")


@app.callback()
def main_callback():
    """
    Validate configuration before running any command.
    """
    try:
        settings.check_config()
    except ValueError as e:
        typer.echo(f"Configuration Error: {e}", err=True)
        raise typer.Exit(code=1)


@db_app.command("backup")
def db_backup(
    debug: bool = typer.Option(False, "--debug", help="Enable debug logging."),
):
    """
    Create a backup of the database.
    """
    setup_logging(debug)
    db_manager = DatabaseManager()
    backup_path = db_manager.backup_database()
    if backup_path:
        typer.echo(f"Backup created at: {backup_path}")
    else:
        typer.echo("Backup failed or no database exists.")


@db_app.command("info")
def db_info():
    """
    Show database configuration info.
    """
    db_manager = DatabaseManager()
    typer.echo(f"Environment: {settings.environment}")
    typer.echo(f"Database File: {db_manager.db_path}")
    typer.echo(f"Is Dev: {settings.is_dev}")


def _run_agent(agent_class, debug: bool, dry_run: bool = False, **kwargs):
    """
    Helper to initialize service and run an agent.
    """
    setup_logging(debug)
    x_service = XService()
    db_manager = DatabaseManager()
    agent = agent_class(x_service, db_manager, dry_run=dry_run, **kwargs)

    async def _async_run():
        try:
            await agent.execute()
        finally:
            await x_service.close()

    try:
        asyncio.run(_async_run())
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
    dry_run: bool = typer.Option(
        False, "--dry-run", help="Simulate actions without making changes."
    ),
):
    """
    Run the unblock agent to unblock blocked accounts.
    """
    _run_agent(UnblockAgent, debug, dry_run=dry_run, user_id=user_id, refresh=refresh)


@app.command()
def insights(
    debug: bool = typer.Option(
        False, "--debug", help="Enable debug logging for detailed output."
    ),
):
    """
    Run the insights agent to gather and report account metrics.
    """
    # Insights is read-only, so dry_run is implicitly irrelevant but we can support it if needed.
    _run_agent(InsightsAgent, debug)


@app.command()
def unfollow(
    debug: bool = typer.Option(
        False, "--debug", help="Enable debug logging for detailed output."
    ),
    dry_run: bool = typer.Option(
        False, "--dry-run", help="Simulate actions without making changes."
    ),
):
    """
    Run the unfollow agent to detect who has unfollowed you.
    """
    _run_agent(UnfollowAgent, debug, dry_run=dry_run)


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
