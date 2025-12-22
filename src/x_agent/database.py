import sqlite3
import logging
from pathlib import Path

from contextlib import contextmanager

STATE_DIR = Path(".state")
DB_FILE = STATE_DIR / "insights.db"


@contextmanager
def db_transaction():
    """Generator-based context manager for database transactions."""
    conn = None
    try:
        STATE_DIR.mkdir(exist_ok=True)
        conn = sqlite3.connect(DB_FILE)
        conn.row_factory = sqlite3.Row
        cursor = conn.cursor()
        yield cursor
        conn.commit()
    except sqlite3.Error as e:
        logging.error(f"Database error: {e}")
        if conn:
            conn.rollback()
        # Re-raise the exception to be handled by the caller
        raise
    finally:
        if conn:
            conn.close()


def get_db_connection():
    """Establishes a connection to the SQLite database."""
    STATE_DIR.mkdir(exist_ok=True)
    conn = sqlite3.connect(DB_FILE)
    conn.row_factory = sqlite3.Row
    return conn


def initialize_database():
    """Initializes the database with the required schema."""
    with db_transaction() as cursor:
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS insights (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
                followers INTEGER,
                following INTEGER
            )
        """)
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS blocked_users (
                user_id INTEGER PRIMARY KEY,
                unblocked_at DATETIME
            )
        """)
    logging.info("Database initialized successfully.")


def add_insight(followers, following):
    """Adds a new insight record to the database."""
    with db_transaction() as cursor:
        cursor.execute(
            "INSERT INTO insights (followers, following) VALUES (?, ?)",
            (followers, following),
        )
    logging.info(f"Added new insight: {followers} followers, {following} following.")


def get_latest_insight():
    """Retrieves the most recent insight from the database."""
    with db_transaction() as cursor:
        cursor.execute("SELECT * FROM insights ORDER BY timestamp DESC LIMIT 1")
        return cursor.fetchone()


def add_blocked_ids_to_db(user_ids: set[int]) -> None:
    """
    Adds a set of blocked user IDs to the database, ignoring duplicates.
    """
    with db_transaction() as cursor:
        data = [(user_id,) for user_id in user_ids]
        cursor.executemany(
            "INSERT OR IGNORE INTO blocked_users (user_id) VALUES (?)", data
        )
    logging.info(f"Cached {len(user_ids)} blocked IDs to the database.")


def get_all_blocked_ids_from_db() -> set[int]:
    """
    Retrieves the complete set of user IDs from the blocked_users table.
    """
    with db_transaction() as cursor:
        cursor.execute("SELECT user_id FROM blocked_users")
        rows = cursor.fetchall()
        return {row["user_id"] for row in rows}


def mark_user_as_unblocked_in_db(user_id: int) -> None:
    """
    Marks a user as unblocked by setting the unblocked_at timestamp.
    """
    with db_transaction() as cursor:
        cursor.execute(
            "UPDATE blocked_users SET unblocked_at = CURRENT_TIMESTAMP WHERE user_id = ?",
            (user_id,),
        )


def get_unblocked_ids_from_db() -> set[int]:
    """
    Retrieves the set of user IDs that have been marked as unblocked.
    """
    with db_transaction() as cursor:
        cursor.execute(
            "SELECT user_id FROM blocked_users WHERE unblocked_at IS NOT NULL"
        )
        rows = cursor.fetchall()
        return {row["user_id"] for row in rows}


def get_ids_to_unblock_from_db() -> set[int]:
    """
    Retrieves the set of user IDs that have not yet been unblocked.
    """
    with db_transaction() as cursor:
        cursor.execute("SELECT user_id FROM blocked_users WHERE unblocked_at IS NULL")
        rows = cursor.fetchall()
        return {row["user_id"] for row in rows}
