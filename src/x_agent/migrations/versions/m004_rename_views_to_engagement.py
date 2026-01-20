import sqlite3
import logging
from x_agent.migrations.base import Migration


class RenameViewsToEngagement(Migration):
    version = 4
    description = "Rename views column to engagement_score in deleted_tweets table."

    def up(self, cursor: sqlite3.Cursor) -> None:
        # SQLite doesn't support RENAME COLUMN in older versions, but 3.25.0+ does.
        # However, to be safe, we'll check if the column exists and rename it.
        cursor.execute("PRAGMA table_info(deleted_tweets)")
        columns = [row[1] for row in cursor.fetchall()]

        if "views" in columns and "engagement_score" not in columns:
            logging.info("Renaming 'views' to 'engagement_score' in 'deleted_tweets'.")
            # Using the safe RENAME COLUMN syntax supported by modern SQLite
            try:
                cursor.execute(
                    "ALTER TABLE deleted_tweets RENAME COLUMN views TO engagement_score"
                )
            except sqlite3.OperationalError:
                # Fallback for very old SQLite versions: recreate table (unlikely needed here)
                logging.warning(
                    "RENAME COLUMN failed, attempting table recreation fallback."
                )
                cursor.execute(
                    "ALTER TABLE deleted_tweets RENAME TO deleted_tweets_old"
                )
                cursor.execute("""
                    CREATE TABLE deleted_tweets (
                        tweet_id INTEGER PRIMARY KEY,
                        text TEXT,
                        created_at DATETIME,
                        engagement_score INTEGER,
                        is_response BOOLEAN,
                        deleted_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW'))
                    )
                """)
                cursor.execute("""
                    INSERT INTO deleted_tweets (tweet_id, text, created_at, engagement_score, is_response, deleted_at)
                    SELECT tweet_id, text, created_at, views, is_response, deleted_at FROM deleted_tweets_old
                """)
                cursor.execute("DROP TABLE deleted_tweets_old")
