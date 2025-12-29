import pytest
import sqlite3
from unittest.mock import patch
from x_agent.migrations.runner import run_migrations, _get_applied_versions
from x_agent.database import DatabaseManager


@pytest.fixture
def test_db_path(tmp_path):
    return tmp_path / "migration_test.db"


@pytest.fixture
def db_manager(test_db_path):
    return DatabaseManager(db_path=test_db_path)


def test_run_migrations_creates_tables(db_manager, test_db_path):
    run_migrations(db_manager)

    conn = sqlite3.connect(test_db_path)
    cursor = conn.cursor()

    # Verify schema_versions exists and migration 1 is applied
    cursor.execute("SELECT version FROM schema_versions")
    versions = [row[0] for row in cursor.fetchall()]
    assert 1 in versions

    # Verify business tables exist
    cursor.execute("SELECT name FROM sqlite_master WHERE type='table'")
    tables = [row[0] for row in cursor.fetchall()]
    assert "insights" in tables
    assert "blocked_users" in tables
    conn.close()


def test_run_migrations_is_idempotent(db_manager, test_db_path):
    # Run twice
    run_migrations(db_manager)
    run_migrations(db_manager)

    applied = _get_applied_versions(db_manager)
    assert len(applied) == 1
    assert applied[0] == 1


def test_migration_backup_triggered(db_manager, test_db_path, tmp_path):
    # We patch the backup_database method on the instance
    with patch.object(db_manager, "backup_database") as mock_backup:
        run_migrations(db_manager)
        # Should be called once because we have one pending migration (v1)
        assert mock_backup.called
