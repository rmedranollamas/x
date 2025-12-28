import logging
import importlib
import pkgutil
from pathlib import Path
from typing import List, Type
from x_agent.migrations.base import Migration
from x_agent.migrations import versions


def _ensure_migrations_table():
    """Ensures the schema_versions table exists."""
    from x_agent.database import db_transaction

    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("""
            CREATE TABLE IF NOT EXISTS schema_versions (
                version INTEGER PRIMARY KEY,
                applied_at DATETIME DEFAULT (STRFTIME('%Y-%m-%d %H:%M:%f', 'NOW')),
                description TEXT
            )
        """)


def _get_applied_versions() -> List[int]:
    """Returns a list of already applied migration versions."""
    from x_agent.database import db_transaction

    with db_transaction() as conn:
        cursor = conn.cursor()
        cursor.execute("SELECT version FROM schema_versions ORDER BY version ASC")
        return [row["version"] for row in cursor.fetchall()]


def _get_migration_classes() -> List[Type[Migration]]:
    """Discovers and returns all Migration subclasses in the versions package."""
    migrations = []

    if not versions.__file__:
        raise RuntimeError("Cannot resolve migration versions path.")

    package_path = Path(versions.__file__).parent

    for _, name, _ in pkgutil.iter_modules([str(package_path)]):
        module = importlib.import_module(f"x_agent.migrations.versions.{name}")
        for attribute_name in dir(module):
            attribute = getattr(module, attribute_name)
            if (
                isinstance(attribute, type)
                and issubclass(attribute, Migration)
                and attribute is not Migration
            ):
                migrations.append(attribute)

    return sorted(migrations, key=lambda m: m.version)


def run_migrations():
    """


    Executes all pending migrations.


    """

    _ensure_migrations_table()

    applied_versions = _get_applied_versions()

    available_migrations = _get_migration_classes()

    pending_migrations = [
        m for m in available_migrations if m.version not in applied_versions
    ]

    if not pending_migrations:
        logging.info("Schema is up to date.")

        return

    logging.info(f"Found {len(pending_migrations)} pending migrations.")

    from x_agent.database import backup_database, db_transaction

    backup_database()

    with db_transaction() as conn:
        cursor = conn.cursor()

        for migration_class in pending_migrations:
            migration = migration_class()
            logging.info(
                f"Applying migration {migration.version}: {migration.description}..."
            )

            try:
                migration.up(cursor)
                cursor.execute(
                    "INSERT INTO schema_versions (version, description) VALUES (?, ?)",
                    (migration.version, migration.description),
                )
                logging.info(f"Migration {migration.version} applied successfully.")
            except Exception as e:
                logging.error(f"Failed to apply migration {migration.version}: {e}")
                raise
