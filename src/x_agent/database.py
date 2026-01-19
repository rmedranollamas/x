import sqlite3
import logging
import shutil
import time
from pathlib import Path
from typing import List, Optional
from contextlib import contextmanager
from .config import settings

STATE_DIR = Path(".state")


class DatabaseManager:
    def __init__(self, db_path: Optional[Path] = None):
        self.db_path = db_path or (STATE_DIR / settings.db_name)

    @contextmanager
    def transaction(self):
        """
        Context manager for database transactions.
        Ensures the connection is opened, committed, and closed correctly.
        """
        STATE_DIR.mkdir(exist_ok=True)
        conn = sqlite3.connect(self.db_path)
        conn.row_factory = sqlite3.Row
        try:
            yield conn
            conn.commit()
        except Exception:
            conn.rollback()
            raise
        finally:
            conn.close()

    def initialize_database(self) -> None:
        """Initializes the database using the migration runner."""
        from x_agent.migrations.runner import run_migrations

        run_migrations(self)

    def backup_database(self) -> Optional[str]:
        """
        Creates a backup of the current database.
        Returns the path to the backup file if successful, None otherwise.
        """
        if not self.db_path.exists():
            logging.warning("No database found to backup.")
            return None

        backup_dir = STATE_DIR / "backups"
        backup_dir.mkdir(parents=True, exist_ok=True)
        timestamp = time.strftime("%Y%m%d_%H%M%S")
        backup_name = f"{self.db_path.stem}_{timestamp}.db"
        backup_path = backup_dir / backup_name

        try:
            shutil.copy2(self.db_path, backup_path)
            logging.info(f"Database backed up to {backup_path}")
            return str(backup_path)
        except Exception as e:
            logging.error(f"Failed to backup database: {e}")
            return None

    def add_insight(
        self,
        followers: int,
        following: int,
        tweet_count: int = 0,
        listed_count: int = 0,
    ) -> None:
        """Adds a new insight record to the database."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute(
                "INSERT INTO insights (followers, following, tweet_count, listed_count) VALUES (?, ?, ?, ?)",
                (followers, following, tweet_count, listed_count),
            )
        logging.info(
            f"Added new insight: {followers} followers, {following} following, {tweet_count} tweets, {listed_count} listed."
        )

    def get_latest_insight(self) -> Optional[sqlite3.Row]:
        """Retrieves the most recent insight from the database."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute(
                "SELECT * FROM insights ORDER BY timestamp DESC, id DESC LIMIT 1"
            )
            return cursor.fetchone()

    def get_insight_at_offset(self, days_ago: int) -> Optional[sqlite3.Row]:
        """
        Retrieves the insight record closest to the specified number of days ago.
        """
        with self.transaction() as conn:
            cursor = conn.cursor()
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

    def add_blocked_users(self, user_ids: set[int]) -> None:
        """Adds a set of blocked user IDs to the database."""
        if not user_ids:
            return
        with self.transaction() as conn:
            cursor = conn.cursor()
            data = [(uid, "PENDING") for uid in user_ids]
            cursor.executemany(
                "INSERT OR IGNORE INTO blocked_users (user_id, status) VALUES (?, ?)",
                data,
            )

    def get_pending_blocked_users(self) -> List[int]:
        """Retrieves all user IDs with status 'PENDING' or 'FAILED'."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute(
                "SELECT user_id FROM blocked_users WHERE status IN ('PENDING', 'FAILED')"
            )
            rows = cursor.fetchall()
            return [row["user_id"] for row in rows]

    def get_all_blocked_users_count(self) -> int:
        """Returns the total number of users in the blocked_users table."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT COUNT(*) FROM blocked_users")
            return cursor.fetchone()[0]

    def get_processed_users_count(self) -> int:
        """Returns the number of users that have been processed (not PENDING)."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute(
                "SELECT COUNT(*) FROM blocked_users WHERE status != 'PENDING'"
            )
            return cursor.fetchone()[0]

    def clear_pending_blocked_users(self) -> None:
        """Deletes all users with status 'PENDING' from blocked_users table."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute("DELETE FROM blocked_users WHERE status = 'PENDING'")

    def update_user_status(self, user_id: int, status: str) -> None:
        """Updates the status of a specific user."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute(
                "UPDATE blocked_users SET status = ?, updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')) WHERE user_id = ?",
                (status, user_id),
            )

    def update_user_statuses(self, user_ids: List[int], status: str) -> None:
        """Batch updates the status of multiple users."""
        if not user_ids:
            return
        with self.transaction() as conn:
            cursor = conn.cursor()
            data = [(status, uid) for uid in user_ids]
            cursor.executemany(
                "UPDATE blocked_users SET status = ?, updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')) WHERE user_id = ?",
                data,
            )

    def add_following_users(self, user_ids: set[int]) -> None:
        """Adds a set of followed user IDs to the database."""
        if not user_ids:
            return
        with self.transaction() as conn:
            cursor = conn.cursor()
            data = [(uid, "PENDING") for uid in user_ids]
            cursor.executemany(
                "INSERT OR IGNORE INTO following_users (user_id, status) VALUES (?, ?)",
                data,
            )

    def get_pending_following_users(self) -> List[int]:
        """Retrieves all user IDs from following_users with status 'PENDING' or 'FAILED'."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute(
                "SELECT user_id FROM following_users WHERE status IN ('PENDING', 'FAILED')"
            )
            rows = cursor.fetchall()
            return [row["user_id"] for row in rows]

    def get_all_following_users_count(self) -> int:
        """Returns the total number of users in the following_users table."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT COUNT(*) FROM following_users")
            return cursor.fetchone()[0]

    def get_processed_following_count(self) -> int:
        """Returns the number of followed users that have been processed."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute(
                "SELECT COUNT(*) FROM following_users WHERE status != 'PENDING'"
            )
            return cursor.fetchone()[0]

    def clear_pending_following_users(self) -> None:
        """Deletes all users with status 'PENDING' from following_users table."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute("DELETE FROM following_users WHERE status = 'PENDING'")

    def update_following_status(self, user_ids: List[int], status: str) -> None:
        """Batch updates the status of multiple following users."""
        if not user_ids:
            return
        with self.transaction() as conn:
            cursor = conn.cursor()
            data = [(status, uid) for uid in user_ids]
            cursor.executemany(
                "UPDATE following_users SET status = ?, updated_at = (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')) WHERE user_id = ?",
                data,
            )

    def get_all_follower_ids(self) -> set[int]:
        """Retrieves all user IDs from the followers table."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT user_id FROM followers")
            rows = cursor.fetchall()
            return {row["user_id"] for row in rows}

    def replace_followers(self, user_ids: set[int]) -> None:
        """Replaces the entire followers table with the given set of IDs."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute("DELETE FROM followers")
            data = [(uid,) for uid in user_ids]
            cursor.executemany("INSERT INTO followers (user_id) VALUES (?)", data)

    def log_unfollows(self, user_ids: List[int]) -> None:
        """Logs multiple unfollow events into the unfollows table."""
        if not user_ids:
            return
        with self.transaction() as conn:
            cursor = conn.cursor()
            data = [(uid,) for uid in user_ids]
            cursor.executemany("INSERT INTO unfollows (user_id) VALUES (?)", data)

    def log_deleted_tweet(
        self,
        tweet_id: int,
        text: str,
        created_at: str,
        engagement_score: int,
        is_response: bool,
    ) -> None:
        """Logs a deleted tweet for audit purposes."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute(
                """
                INSERT OR IGNORE INTO deleted_tweets (tweet_id, text, created_at, engagement_score, is_response)
                VALUES (?, ?, ?, ?, ?)
                """,
                (tweet_id, text, created_at, engagement_score, is_response),
            )

    def get_deleted_count(self) -> int:
        """Returns the total number of logged deleted tweets."""
        with self.transaction() as conn:
            cursor = conn.cursor()
            cursor.execute("SELECT COUNT(*) FROM deleted_tweets")
            return cursor.fetchone()[0]
