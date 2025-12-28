import pytest
import sqlite3
from unittest.mock import patch
from x_agent.migrations.runner import run_migrations, _get_applied_versions


@pytest.fixture
def test_db_path(tmp_path):
    return tmp_path / "migration_test.db"


def test_run_migrations_creates_tables(test_db_path):
    with patch("x_agent.database.get_db_path", return_value=test_db_path):
        run_migrations()

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


def test_run_migrations_is_idempotent(test_db_path):
    with patch("x_agent.database.get_db_path", return_value=test_db_path):
        # Run twice
        run_migrations()
        run_migrations()

        applied = _get_applied_versions()
        assert len(applied) == 1
        assert applied[0] == 1


def test_migration_backup_triggered(test_db_path, tmp_path):
    with patch("x_agent.database.get_db_path", return_value=test_db_path):
        with patch("x_agent.database.STATE_DIR", tmp_path):
            with patch("x_agent.database.backup_database") as mock_backup:
                run_migrations()
                # Should be called once because we have one pending migration (v1)
                assert mock_backup.called
