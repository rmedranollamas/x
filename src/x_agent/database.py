import sqlite3
import logging
from pathlib import Path
from typing import List, Optional
from contextlib import contextmanager

STATE_DIR = Path(".state")
DB_FILE = STATE_DIR / "insights.db"


@contextmanager
def db_transaction():
    """
    Context manager for database transactions.
    Ensures the connection is opened, committed, and closed correctly.
    """
    STATE_DIR.mkdir(exist_ok=True)
    conn = sqlite3.connect(DB_FILE)
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
    """Initializes the database with the required schema and handles migrations."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS insights (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                timestamp DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
                followers INTEGER,
                following INTEGER
            )
        """)
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS blocked_users (
                user_id INTEGER PRIMARY KEY,
                status TEXT DEFAULT 'PENDING',
                updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
            )
        """)
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS following_users (
                user_id INTEGER PRIMARY KEY,
                status TEXT DEFAULT 'PENDING',
                updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
            )
        """)

        # Migration logic
        cursor.execute("PRAGMA table_info(blocked_users)")
        columns = [row["name"] for row in cursor.fetchall()]
        if "status" not in columns:
            logging.info("Migrating blocked_users table: adding 'status' column.")
            cursor.execute(
                "ALTER TABLE blocked_users ADD COLUMN status TEXT DEFAULT 'PENDING'"
            )
        if "updated_at" not in columns:
            logging.info("Migrating blocked_users table: adding 'updated_at' column.")
            cursor.execute("ALTER TABLE blocked_users ADD COLUMN updated_at DATETIME")
            cursor.execute(
                "UPDATE blocked_users SET updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')) WHERE updated_at IS NULL"
            )

        if "unblocked_at" in columns:
            logging.info("Migrating legacy data from 'unblocked_at' to 'status'.")
            cursor.execute(
                "UPDATE blocked_users SET status = 'UNBLOCKED' WHERE unblocked_at IS NOT NULL AND status = 'PENDING'"
            )
            cursor.execute("UPDATE blocked_users SET unblocked_at = NULL")

    logging.info("Database initialized successfully.")


def add_insight(followers: int, following: int) -> None:
    """Adds a new insight record to the database."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute(
            "INSERT INTO insights (followers, following) VALUES (?, ?)",
            (followers, following),
        )
    logging.info(f"Added new insight: {followers} followers, {following} following.")


def get_latest_insight() -> Optional[sqlite3.Row]:
    """Retrieves the most recent insight from the database."""
    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute(
            "SELECT * FROM insights ORDER BY timestamp DESC, id DESC LIMIT 1"
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
