import sqlite3
import logging
from x_agent.migrations.base import Migration


class AddListedCount(Migration):
    version = 2
    description = "Add listed_count column to insights table."

    def up(self, cursor: sqlite3.Cursor) -> None:
        # Ensure the column exists
        cursor.execute("PRAGMA table_info(insights)")
        columns = [row[1] for row in cursor.fetchall()]
        if "listed_count" not in columns:
            logging.info("Adding missing column 'listed_count' to table 'insights'.")
            cursor.execute(
                "ALTER TABLE insights ADD COLUMN listed_count INTEGER DEFAULT 0"
            )
