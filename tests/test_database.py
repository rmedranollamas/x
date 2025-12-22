import pytest
import sqlite3
from unittest.mock import patch
from src.x_agent import database


@pytest.fixture
def mock_db(tmp_path):
    """Fixture to use a temporary database file for testing."""
    test_db = tmp_path / "test_insights.db"
    with patch("src.x_agent.database.DB_FILE", test_db):
        database.initialize_database()
        yield test_db


def test_initialize_database(tmp_path):
    test_db = tmp_path / "init_test.db"
    with patch("src.x_agent.database.DB_FILE", test_db):
        database.initialize_database()
        assert test_db.exists()

        conn = sqlite3.connect(test_db)
        cursor = conn.cursor()
        cursor.execute("SELECT name FROM sqlite_master WHERE type='table'")
        tables = [row[0] for row in cursor.fetchall()]
        assert "insights" in tables
        assert "blocked_users" in tables
        conn.close()


def test_blocked_users_operations(tmp_path):
    test_db = tmp_path / "ops_test.db"
    with patch("src.x_agent.database.DB_FILE", test_db):
        database.initialize_database()

        # Test adding users
        user_ids = {101, 102, 103}
        database.add_blocked_users(user_ids)
        assert database.get_all_blocked_users_count() == 3
        assert len(database.get_pending_blocked_users()) == 3

        # Test duplicates (should ignore)
        database.add_blocked_users({101, 104})
        assert database.get_all_blocked_users_count() == 4

        # Test update status
        database.update_user_status(101, "UNBLOCKED")
        database.update_user_status(102, "FAILED")

        assert database.get_processed_users_count() == 2
        pending = database.get_pending_blocked_users()
        assert 101 not in pending
        assert 103 in pending


def test_insights_operations(tmp_path):
    test_db = tmp_path / "insights_test.db"
    with patch("src.x_agent.database.DB_FILE", test_db):
        database.initialize_database()

        database.add_insight(100, 50)
        database.add_insight(110, 55)

        latest = database.get_latest_insight()
        assert latest["followers"] == 110
        assert latest["following"] == 55
