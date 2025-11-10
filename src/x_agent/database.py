import sqlite3
import logging
from pathlib import Path

STATE_DIR = Path(".state")
DB_FILE = STATE_DIR / "insights.db"


def get_db_connection():
    """Establishes a connection to the SQLite database."""
    STATE_DIR.mkdir(exist_ok=True)
    conn = sqlite3.connect(DB_FILE)
    conn.row_factory = sqlite3.Row
    return conn


def initialize_database():
    """Initializes the database with the required schema."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS insights (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            followers INTEGER,
            following INTEGER
        )
    """)
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS unblocked_users (
            user_id INTEGER PRIMARY KEY,
            unblocked_at DATETIME
        )
    """)
    conn.commit()
    conn.close()
    logging.info("Database initialized successfully.")


def add_insight(followers, following):
    """Adds a new insight record to the database."""
    conn = get_db_connection()
    try:
        cursor = conn.cursor()
        cursor.execute(
            "INSERT INTO insights (followers, following) VALUES (?, ?)",
            (followers, following)
        )
        conn.commit()
    finally:
        conn.close()
    logging.info(f"Added new insight: {followers} followers, {following} following.")


def get_latest_insight():
    """Retrieves the most recent insight from the database."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM insights ORDER BY timestamp DESC LIMIT 1")
    insight = cursor.fetchone()
    conn.close()
    return insight


def add_blocked_ids_to_db(user_ids: set[int]) -> None:
    """
    Adds a set of blocked user IDs to the database, ignoring duplicates.
    """
    conn = get_db_connection()
    cursor = conn.cursor()
    data = [(user_id,) for user_id in user_ids]
    cursor.executemany(
        "INSERT OR IGNORE INTO unblocked_users (user_id) VALUES (?)", data
    )
    conn.commit()
    conn.close()
    logging.info(f"Added {len(user_ids)} blocked IDs to the database.")


def get_all_blocked_ids_from_db() -> set[int]:
    """
    Retrieves the complete set of user IDs from the unblocked_users table.
    """
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("SELECT user_id FROM unblocked_users")
    rows = cursor.fetchall()
    conn.close()
    return {row["user_id"] for row in rows}


def mark_user_as_unblocked_in_db(user_id: int) -> None:
    """
    Marks a user as unblocked by setting the unblocked_at timestamp.
    """
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute(
        "UPDATE unblocked_users SET unblocked_at = CURRENT_TIMESTAMP WHERE user_id = ?",
        (user_id,),
    )
    conn.commit()
    conn.close()


def get_unblocked_ids_from_db() -> set[int]:
    """
    Retrieves the set of user IDs that have been marked as unblocked.
    """
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("SELECT user_id FROM unblocked_users WHERE unblocked_at IS NOT NULL")
    rows = cursor.fetchall()
    conn.close()
    return {row["user_id"] for row in rows}


def get_ids_to_unblock_from_db() -> set[int]:
    """
    Retrieves the set of user IDs that have not yet been unblocked.
    """
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("SELECT user_id FROM unblocked_users WHERE unblocked_at IS NULL")
    rows = cursor.fetchall()
    conn.close()
    return {row["user_id"] for row in rows}
