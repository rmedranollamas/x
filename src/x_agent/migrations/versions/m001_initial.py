import sqlite3
import logging
from x_agent.migrations.base import Migration


class InitialSchema(Migration):
    version = 1
    description = "Initial schema with insights, blocked_users, and followers tables."

    def up(self, cursor: sqlite3.Cursor) -> None:
        # Create tables
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS insights (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                timestamp DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
                followers INTEGER,
                following INTEGER,
                tweet_count INTEGER DEFAULT 0
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
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS followers (
                user_id INTEGER PRIMARY KEY,
                updated_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
            )
        """)
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS unfollows (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                user_id INTEGER,
                timestamp DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
            )
        """)

        # Idempotent column additions (Migration logic from legacy initialize_database)
        self._ensure_column(cursor, "insights", "tweet_count", "INTEGER DEFAULT 0")
        self._ensure_column(cursor, "blocked_users", "status", "TEXT DEFAULT 'PENDING'")
        self._ensure_column(cursor, "blocked_users", "updated_at", "DATETIME")

        # Data cleanup (legacy)
        cursor.execute("PRAGMA table_info(blocked_users)")
        columns = [row[1] for row in cursor.fetchall()]  # row is (cid, name, type, ...)

        if "updated_at" in columns:
            cursor.execute(
                "UPDATE blocked_users SET updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')) WHERE updated_at IS NULL"
            )

        if "unblocked_at" in columns:
            logging.info("Migrating legacy data from 'unblocked_at' to 'status'.")
            cursor.execute(
                "UPDATE blocked_users SET status = 'UNBLOCKED' WHERE unblocked_at IS NOT NULL AND status = 'PENDING'"
            )
            # We don't drop columns in SQLite easily, so we leave unblocked_at

    def _ensure_column(
        self, cursor: sqlite3.Cursor, table: str, column: str, definition: str
    ):
        cursor.execute(f"PRAGMA table_info({table})")
        columns = [row[1] for row in cursor.fetchall()]
        if column not in columns:
            logging.info(f"Adding missing column '{column}' to table '{table}'.")
            cursor.execute(f"ALTER TABLE {table} ADD COLUMN {column} {definition}")
