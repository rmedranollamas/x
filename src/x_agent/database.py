import sqlite3
import logging
import shutil
import time
from pathlib import Path
from typing import List, Optional
from contextlib import contextmanager
from .config import settings

STATE_DIR = Path(".state")


def get_db_path() -> Path:
    return STATE_DIR / settings.db_name


@contextmanager
def db_transaction():
    """
    Context manager for database transactions.
    Ensures the connection is opened, committed, and closed correctly.
    """
    STATE_DIR.mkdir(exist_ok=True)
    db_file = get_db_path()
    conn = sqlite3.connect(db_file)
    conn.row_factory = sqlite3.Row
    try:
        yield conn
        conn.commit()
    except Exception:
        conn.rollback()
        raise
    finally:
        conn.close()


def initialize_database() -> None:
    """Initializes the database using the migration runner."""

    from x_agent.migrations.runner import run_migrations

    run_migrations()


def backup_database() -> Optional[str]:
    """


    Creates a backup of the current database.


    Returns the path to the backup file if successful, None otherwise.


    """

    db_path = get_db_path()

    if not db_path.exists():
        logging.warning("No database found to backup.")

        return None

    backup_dir = STATE_DIR / "backups"

    backup_dir.mkdir(parents=True, exist_ok=True)

    timestamp = time.strftime("%Y%m%d_%H%M%S")

    backup_name = f"{db_path.stem}_{timestamp}.db"

    backup_path = backup_dir / backup_name

    try:
        shutil.copy2(db_path, backup_path)

        logging.info(f"Database backed up to {backup_path}")

        return str(backup_path)

    except Exception as e:
        logging.error(f"Failed to backup database: {e}")

        return None


def add_insight(followers: int, following: int, tweet_count: int = 0) -> None:
    """Adds a new insight record to the database."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute(
            "INSERT INTO insights (followers, following, tweet_count) VALUES (?, ?, ?)",
            (followers, following, tweet_count),
        )
    logging.info(
        f"Added new insight: {followers} followers, {following} following, {tweet_count} tweets."
    )


def get_latest_insight() -> Optional[sqlite3.Row]:
    """Retrieves the most recent insight from the database."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute(
            "SELECT * FROM insights ORDER BY timestamp DESC, id DESC LIMIT 1"
        )
        return cursor.fetchone()


def get_insight_at_offset(days_ago: int) -> Optional[sqlite3.Row]:
    """
    Retrieves the insight record closest to the specified number of days ago.
    """
    with db_transaction() as conn:
        cursor = conn.cursor()
        # We look for the record closest to (NOW - days_ago) but not newer than it
        cursor.execute(
            """
            SELECT * FROM insights
            WHERE timestamp <= STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW', ?)
            ORDER BY timestamp DESC
            LIMIT 1
            """,
            (f"-{days_ago} days",),
        )
        return cursor.fetchone()


def add_blocked_users(user_ids: set[int]) -> None:
    """Adds a set of blocked user IDs to the database."""
    if not user_ids:
        return
    with db_transaction() as conn:
        cursor = conn.cursor()
        data = [(uid, "PENDING") for uid in user_ids]
        cursor.executemany(
            "INSERT OR IGNORE INTO blocked_users (user_id, status) VALUES (?, ?)", data
        )


def get_pending_blocked_users() -> List[int]:
    """Retrieves all user IDs with status 'PENDING' or 'FAILED'."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute(
            "SELECT user_id FROM blocked_users WHERE status IN ('PENDING', 'FAILED')"
        )
        rows = cursor.fetchall()
        return [row["user_id"] for row in rows]


def get_all_blocked_users_count() -> int:
    """Returns the total number of users in the blocked_users table."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("SELECT COUNT(*) FROM blocked_users")
        return cursor.fetchone()[0]


def get_processed_users_count() -> int:
    """Returns the number of users that have been processed (not PENDING)."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("SELECT COUNT(*) FROM blocked_users WHERE status != 'PENDING'")
        return cursor.fetchone()[0]


def clear_pending_blocked_users() -> None:
    """Deletes all users with status 'PENDING' from blocked_users table."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("DELETE FROM blocked_users WHERE status = 'PENDING'")


def update_user_status(user_id: int, status: str) -> None:
    """Updates the status of a specific user."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute(
            "UPDATE blocked_users SET status = ?, updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')) WHERE user_id = ?",
            (status, user_id),
        )


def update_user_statuses(user_ids: List[int], status: str) -> None:
    """Batch updates the status of multiple users."""
    if not user_ids:
        return
    with db_transaction() as conn:
        cursor = conn.cursor()
        data = [(status, uid) for uid in user_ids]
        cursor.executemany(
            "UPDATE blocked_users SET status = ?, updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')) WHERE user_id = ?",
            data,
        )


def add_following_users(user_ids: set[int]) -> None:
    """Adds a set of followed user IDs to the database."""
    if not user_ids:
        return
    with db_transaction() as conn:
        cursor = conn.cursor()
        data = [(uid, "PENDING") for uid in user_ids]
        cursor.executemany(
            "INSERT OR IGNORE INTO following_users (user_id, status) VALUES (?, ?)",
            data,
        )


def get_pending_following_users() -> List[int]:
    """Retrieves all user IDs from following_users with status 'PENDING' or 'FAILED'."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute(
            "SELECT user_id FROM following_users WHERE status IN ('PENDING', 'FAILED')"
        )
        rows = cursor.fetchall()
        return [row["user_id"] for row in rows]


def get_all_following_users_count() -> int:
    """Returns the total number of users in the following_users table."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("SELECT COUNT(*) FROM following_users")
        return cursor.fetchone()[0]


def get_processed_following_count() -> int:
    """Returns the number of followed users that have been processed."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("SELECT COUNT(*) FROM following_users WHERE status != 'PENDING'")
        return cursor.fetchone()[0]


def clear_pending_following_users() -> None:
    """Deletes all users with status 'PENDING' from following_users table."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("DELETE FROM following_users WHERE status = 'PENDING'")


def update_following_status(user_ids: List[int], status: str) -> None:
    """Batch updates the status of multiple following users."""
    if not user_ids:
        return
    with db_transaction() as conn:
        cursor = conn.cursor()
        data = [(status, uid) for uid in user_ids]
        cursor.executemany(
            "UPDATE following_users SET status = ?, updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')) WHERE user_id = ?",
            data,
        )


def get_all_follower_ids() -> set[int]:
    """Retrieves all user IDs from the followers table."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("SELECT user_id FROM followers")
        rows = cursor.fetchall()
        return {row["user_id"] for row in rows}


def replace_followers(user_ids: set[int]) -> None:
    """Replaces the entire followers table with the given set of IDs."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("DELETE FROM followers")
        data = [(uid,) for uid in user_ids]
        cursor.executemany("INSERT INTO followers (user_id) VALUES (?)", data)


def log_unfollows(user_ids: List[int]) -> None:
    """Logs multiple unfollow events into the unfollows table."""
    if not user_ids:
        return
    with db_transaction() as conn:
        cursor = conn.cursor()
        data = [(uid,) for uid in user_ids]
        cursor.executemany("INSERT INTO unfollows (user_id) VALUES (?)", data)
