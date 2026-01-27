import sys
import logging
import typer
import asyncio
from typing import Optional, List
from .services.x_service import XService
from .agents.unblock_agent import UnblockAgent
from .agents.insights_agent import InsightsAgent
from .agents.blocked_ids_agent import BlockedIdsAgent
from .agents.unfollow_agent import UnfollowAgent
from .agents.delete_agent import DeleteAgent
from .utils.email_utils import send_report_email
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
        # Display environment info at startup
        db_manager = DatabaseManager()
        env_str = typer.style(
            settings.environment.upper(),
            fg=typer.colors.GREEN if settings.is_dev else typer.colors.YELLOW,
            bold=True,
        )
        db_str = typer.style(str(db_manager.db_path.name), fg=typer.colors.CYAN)
        typer.echo(f"Environment: {env_str} | Database: {db_str}")
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


async def _execute_agent(agent_class, debug: bool, email: bool = False, **kwargs):
    """
    Shared helper to initialize service, run an agent, and optionally send an email report.
    """
    setup_logging(debug)
    x_service = XService()
    db_manager = DatabaseManager()
    try:
        agent = agent_class(x_service, db_manager, **kwargs)
        report = await agent.execute()
        if email and report:
            await send_report_email(report)
    except Exception:
        raise
    finally:
        await x_service.close()


def _run_agent(agent_class, debug: bool, dry_run: bool = False, **kwargs):
    """
    Legacy helper to run an agent without email support.
    """
    try:
        return asyncio.run(
            _execute_agent(agent_class, debug, dry_run=dry_run, **kwargs)
        )
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
    email: bool = typer.Option(
        False, "--email", help="Send the report via email after generation."
    ),
):
    """
    Run the insights agent to gather and report account metrics.
    """
    try:
        asyncio.run(_execute_agent(InsightsAgent, debug, email=email))
    except Exception as e:
        logging.error(f"An unexpected error occurred: {e}", exc_info=True)
        sys.exit(1)


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


@app.command()
def delete(
    debug: bool = typer.Option(
        False, "--debug", help="Enable debug logging for detailed output."
    ),
    dry_run: bool = typer.Option(
        False, "--dry-run", help="Simulate actions without making changes."
    ),
    email: bool = typer.Option(
        False, "--email", help="Send the report via email after generation."
    ),
    protected_ids: Optional[List[int]] = typer.Option(
        None, "--protected-id", help="Tweet IDs to protect from deletion."
    ),
    archive: Optional[str] = typer.Option(
        None, "--archive", help="Path to X data archive (tweets.js)."
    ),
):
    """
    Run the delete agent to remove old tweets based on engagement rules.
    """
    try:
        asyncio.run(
            _execute_agent(
                DeleteAgent,
                debug,
                email=email,
                dry_run=dry_run,
                protected_ids=protected_ids,
                archive_path=archive,
            )
        )
    except Exception as e:
        logging.error(f"An unexpected error occurred: {e}", exc_info=True)
        sys.exit(1)


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
