import pytest
import sqlite3
from unittest.mock import MagicMock
from x_agent.migrations.base import Migration

def test_migration_down_raises_not_implemented():
    class DummyMigration(Migration):
        @property
        def version(self) -> int:
            return 1

        @property
        def description(self) -> str:
            return "A test migration"

        def up(self, cursor: sqlite3.Cursor) -> None:
            pass

    migration = DummyMigration()
    mock_cursor = MagicMock(spec=sqlite3.Cursor)

    with pytest.raises(NotImplementedError, match="Reverse migration not implemented."):
        migration.down(mock_cursor)
