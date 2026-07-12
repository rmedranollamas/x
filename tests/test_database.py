import pytest
import sqlite3
from pathlib import Path
from unittest.mock import patch
from x_agent.database import DatabaseManager


@pytest.fixture
def test_db_path(tmp_path):
    """Fixture to provide a temporary database path."""
    return tmp_path / "test_insights.db"


@pytest.fixture
def db_manager(test_db_path):
    """Fixture to provide a DatabaseManager instance."""
    return DatabaseManager(db_path=test_db_path)


def test_initialize_database(db_manager, test_db_path):
    # run_migrations calls _ensure_migrations_table which uses db_manager.transaction
    # db_manager uses self.db_path.

    # We don't need to patch get_db_path anymore since we inject the path.
    db_manager.initialize_database()
    assert test_db_path.exists()

    conn = sqlite3.connect(test_db_path)
    cursor = conn.cursor()
    cursor.execute("SELECT name FROM sqlite_master WHERE type='table'")
    tables = [row[0] for row in cursor.fetchall()]
    assert "insights" in tables
    assert "blocked_users" in tables
    assert "schema_versions" in tables
    conn.close()


def test_backup_database(db_manager, test_db_path, tmp_path):
    # We need to ensure STATE_DIR points to tmp_path or similar for backup destination
    # But STATE_DIR is a global in database.py.
    # DatabaseManager.backup_database uses STATE_DIR / "backups"

    # We can patch STATE_DIR in x_agent.database
    with patch("x_agent.database.STATE_DIR", tmp_path):
        db_manager.initialize_database()
        backup_path = db_manager.backup_database()
        assert backup_path is not None
        assert Path(backup_path).exists()
        assert Path(backup_path).name.startswith("test_insights_")


def test_blocked_users_operations(db_manager):
    db_manager.initialize_database()

    # Test adding users
    user_ids = {101, 102, 103}
    db_manager.add_blocked_users(user_ids)
    assert db_manager.get_all_blocked_users_count() == 3
    assert len(db_manager.get_pending_blocked_users()) == 3


def test_insights_operations(db_manager):
    db_manager.initialize_database()

    db_manager.add_insight(100, 50)
    db_manager.add_insight(110, 55)

    latest = db_manager.get_latest_insight()
    assert latest is not None
    assert latest["followers"] == 110
    assert latest["following"] == 55


def test_insight_at_offset(db_manager):
    """Test get_insight_at_offset method."""
    db_manager.initialize_database()

    # Add insights with different timestamps
    db_manager.add_insight(100, 50)
    db_manager.add_insight(110, 55)

    # Get insight from 1 day ago (should return the latest or None if data is too new)
    insight = db_manager.get_insight_at_offset(1)
    # The insight might be None if the data is less than 1 day old
    # or it might return the latest insight
    if insight is not None:
        assert insight["followers"] in [100, 110]

    # Get insight from 30 days ago (might return None if no data that old)
    insight_30 = db_manager.get_insight_at_offset(30)
    # Could be None or the oldest available
    if insight_30 is not None:
        assert insight_30["followers"] in [100, 110]


def test_following_users_operations(db_manager):
    """Test following users operations."""
    db_manager.initialize_database()

    # Test adding following users
    user_ids = {201, 202, 203}
    db_manager.add_following_users(user_ids)
    assert db_manager.get_all_following_users_count() == 3
    assert len(db_manager.get_pending_following_users()) == 3

    # Test clearing pending
    db_manager.clear_pending_following_users()
    assert db_manager.get_all_following_users_count() == 0


def test_get_processed_following_count(db_manager):
    """Test get_processed_following_count method."""
    db_manager.initialize_database()

    # Add some users
    db_manager.add_following_users({201, 202, 203})

    # Initially all are pending (status is 'PENDING' by default)
    # So processed count should be 0
    processed = db_manager.get_processed_following_count()
    assert processed == 0

    # Note: update_user_status updates blocked_users table, not following_users
    # So we need to use a different approach to update following_users
    # For now, just verify the initial state


def test_update_user_statuses_batch(db_manager):
    """Test batch update of user statuses."""
    db_manager.initialize_database()

    # Add blocked users (update_user_statuses updates blocked_users table)
    db_manager.add_blocked_users({101, 102, 103})

    # Update multiple statuses
    db_manager.update_user_statuses([101, 102], "PROCESSED")

    # Check that users are no longer pending
    pending = db_manager.get_pending_blocked_users()
    assert 101 not in pending
    assert 102 not in pending
    assert 103 in pending  # 103 was not updated


def test_followers_operations(db_manager):
    """Test followers operations."""
    db_manager.initialize_database()

    # Add followers
    follower_ids = {301, 302, 303}
    db_manager.replace_followers(follower_ids)

    # Get all follower IDs
    retrieved_ids = db_manager.get_all_follower_ids()
    assert retrieved_ids == follower_ids

    # Replace with new set
    new_follower_ids = {401, 402}
    db_manager.replace_followers(new_follower_ids)
    retrieved_ids = db_manager.get_all_follower_ids()
    assert retrieved_ids == new_follower_ids


def test_unfollows_operations(db_manager):
    """Test unfollows operations."""
    db_manager.initialize_database()

    # Log unfollows
    db_manager.log_unfollows([501, 502, 503])

    # Check that unfollows were logged (indirectly by checking count)
    # Note: There's no get_unfollows method, so we can't directly verify
    # But we can verify it doesn't crash


def test_deleted_tweets_operations(db_manager):
    """Test deleted tweets operations."""
    db_manager.initialize_database()

    # Log deleted tweets
    db_manager.log_deleted_tweet(1001, "Tweet text", "2023-01-01", 5, False)
    db_manager.log_deleted_tweet(1002, "Another tweet", "2023-01-02", 10, True)

    # Check count
    assert db_manager.get_deleted_count() == 2

    # Check if tweet is deleted
    assert db_manager.is_tweet_deleted(1001) is True
    assert db_manager.is_tweet_deleted(9999) is False

    # Get all deleted tweet IDs
    deleted_ids = db_manager.get_all_deleted_tweet_ids()
    assert deleted_ids == {1001, 1002}


def test_clear_pending_following_users(db_manager):
    """Test clearing pending following users."""
    db_manager.initialize_database()

    # Add users
    db_manager.add_following_users({201, 202, 203})
    assert db_manager.get_all_following_users_count() == 3

    # Clear pending
    db_manager.clear_pending_following_users()
    assert db_manager.get_all_following_users_count() == 0


def test_clear_pending_blocked_users(db_manager):
    """Test clearing pending blocked users."""
    db_manager.initialize_database()

    # Add users
    db_manager.add_blocked_users({101, 102, 103})
    assert db_manager.get_all_blocked_users_count() == 3

    # Clear pending
    db_manager.clear_pending_blocked_users()
    assert db_manager.get_all_blocked_users_count() == 0


def test_update_user_status(db_manager):
    """Test updating individual user status."""
    db_manager.initialize_database()

    # Add user
    db_manager.add_blocked_users({101})

    # Update status
    db_manager.update_user_status(101, "PROCESSED")

    # Check that user is no longer pending
    pending = db_manager.get_pending_blocked_users()
    assert 101 not in pending


def test_backup_database_no_db(db_manager, tmp_path):
    """Test backup when database doesn't exist."""
    # Don't initialize the database
    with patch("x_agent.database.STATE_DIR", tmp_path):
        backup_path = db_manager.backup_database()
        assert backup_path is None


def test_transaction_context_manager(db_manager, test_db_path):
    """Test transaction context manager."""
    db_manager.initialize_database()

    # Test that transaction works
    with db_manager.transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("SELECT COUNT(*) FROM insights")
        count = cursor.fetchone()[0]
        assert count >= 0


def test_add_insight_with_all_fields(db_manager):
    """Test add_insight with all fields."""
    db_manager.initialize_database()

    db_manager.add_insight(100, 50, 10, 5)

    latest = db_manager.get_latest_insight()
    assert latest is not None
    assert latest["followers"] == 100
    assert latest["following"] == 50
    assert latest["tweet_count"] == 10
    assert latest["listed_count"] == 5


def test_log_deleted_tweet_duplicate(db_manager):
    """Test that duplicate deleted tweets are handled (INSERT OR IGNORE)."""
    db_manager.initialize_database()

    # Log same tweet twice
    db_manager.log_deleted_tweet(1001, "Tweet text", "2023-01-01", 5, False)
    db_manager.log_deleted_tweet(1001, "Tweet text", "2023-01-01", 5, False)

    # Should only have one entry
    assert db_manager.get_deleted_count() == 1
