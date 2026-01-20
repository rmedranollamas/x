import sqlite3
from x_agent.migrations.base import Migration


class CreateDeletedTweetsTable(Migration):
    version = 3
    description = "Create deleted_tweets table for audit logging."

    def up(self, cursor: sqlite3.Cursor) -> None:
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS deleted_tweets (
                tweet_id INTEGER PRIMARY KEY,
                text TEXT,
                created_at DATETIME,
                views INTEGER,
                is_response BOOLEAN,
                deleted_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
            )
        """)
