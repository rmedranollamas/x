import sqlite3
import logging
from pathlib import Path
from typing import List

STATE_DIR = Path(".state")
DB_FILE = STATE_DIR / "insights.db"


def get_db_connection():
    """Establishes a connection to the SQLite database."""
    STATE_DIR.mkdir(exist_ok=True)
    conn = sqlite3.connect(DB_FILE)
    conn.row_factory = sqlite3.Row
    return conn


def initialize_database():
    """Initializes the database with the required schema and handles migrations."""
    conn = get_db_connection()
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
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
            "UPDATE blocked_users SET updated_at = CURRENT_TIMESTAMP WHERE updated_at IS NULL"
        )

    if "unblocked_at" in columns:
        logging.info("Migrating legacy data from 'unblocked_at' to 'status'.")
        cursor.execute(
            "UPDATE blocked_users SET status = 'UNBLOCKED' WHERE unblocked_at IS NOT NULL AND status = 'PENDING'"
        )
        cursor.execute("UPDATE blocked_users SET unblocked_at = NULL")

    conn.commit()
    conn.close()
    logging.info("Database initialized successfully.")


def add_insight(followers, following):
    """Adds a new insight record to the database."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute(
        "INSERT INTO insights (followers, following) VALUES (?, ?)",
        (followers, following),
    )
    conn.commit()
    conn.close()
    logging.info(f"Added new insight: {followers} followers, {following} following.")


def get_latest_insight():
    """Retrieves the most recent insight from the database."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM insights ORDER BY timestamp DESC, id DESC LIMIT 1")
    insight = cursor.fetchone()
    conn.close()
    return insight


def add_blocked_users(user_ids: set[int]) -> None:
    """Adds a set of blocked user IDs to the database."""
    if not user_ids:
        return
    conn = get_db_connection()
    cursor = conn.cursor()
    data = [(uid, "PENDING") for uid in user_ids]
    cursor.executemany(
        "INSERT OR IGNORE INTO blocked_users (user_id, status) VALUES (?, ?)", data
    )
    conn.commit()
    conn.close()


def get_pending_blocked_users() -> List[int]:
    """Retrieves all user IDs with status 'PENDING'."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("SELECT user_id FROM blocked_users WHERE status = 'PENDING'")
    rows = cursor.fetchall()
    conn.close()
    return [row["user_id"] for row in rows]


def get_all_blocked_users_count() -> int:
    """Returns the total number of users in the blocked_users table."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("SELECT COUNT(*) FROM blocked_users")
    count = cursor.fetchone()[0]
    conn.close()
    return count


def get_processed_users_count() -> int:
    """Returns the number of users that have been processed (not PENDING)."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute("SELECT COUNT(*) FROM blocked_users WHERE status != 'PENDING'")
    count = cursor.fetchone()[0]
    conn.close()
    return count


def update_user_status(user_id: int, status: str) -> None:
    """Updates the status of a specific user."""
    conn = get_db_connection()
    cursor = conn.cursor()
    cursor.execute(
        "UPDATE blocked_users SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?",
        (status, user_id),
    )
    conn.commit()
    conn.close()
