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
