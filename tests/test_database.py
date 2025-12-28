import pytest
import sqlite3
from pathlib import Path
from unittest.mock import patch
from x_agent import database


@pytest.fixture
def test_db_path(tmp_path):
    """Fixture to provide a temporary database path."""
    return tmp_path / "test_insights.db"


def test_initialize_database(test_db_path):
    # run_migrations is imported inside initialize_database
    # it then calls _ensure_migrations_table which imports db_transaction from database
    # db_transaction uses get_db_path from database.
    # So patching database.get_db_path should work.
    with patch("x_agent.database.get_db_path", return_value=test_db_path):
        database.initialize_database()
        assert test_db_path.exists()

        conn = sqlite3.connect(test_db_path)
        cursor = conn.cursor()
        cursor.execute("SELECT name FROM sqlite_master WHERE type='table'")
        tables = [row[0] for row in cursor.fetchall()]
        assert "insights" in tables
        assert "blocked_users" in tables
        assert "schema_versions" in tables
        conn.close()


def test_backup_database(test_db_path, tmp_path):
    with patch("x_agent.database.get_db_path", return_value=test_db_path):
        with patch("x_agent.database.STATE_DIR", tmp_path):
            database.initialize_database()
            backup_path = database.backup_database()
            assert backup_path is not None
            assert Path(backup_path).exists()
            assert Path(backup_path).name.startswith("test_insights_")


def test_blocked_users_operations(test_db_path):
    with patch("x_agent.database.get_db_path", return_value=test_db_path):
        database.initialize_database()

        # Test adding users
        user_ids = {101, 102, 103}
        database.add_blocked_users(user_ids)
        assert database.get_all_blocked_users_count() == 3
        assert len(database.get_pending_blocked_users()) == 3


def test_insights_operations(test_db_path):
    with patch("x_agent.database.get_db_path", return_value=test_db_path):
        database.initialize_database()

        database.add_insight(100, 50)
        database.add_insight(110, 55)

        latest = database.get_latest_insight()
        assert latest is not None
        assert latest["followers"] == 110
        assert latest["following"] == 55
