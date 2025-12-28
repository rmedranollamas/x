from abc import ABC, abstractmethod
import sqlite3


class Migration(ABC):
    """
    Abstract base class for all migrations.
    """

    @property
    @abstractmethod
    def version(self) -> int:
        """The version number of this migration (must be unique and sequential)."""
        pass

    @property
    @abstractmethod
    def description(self) -> str:
        """A brief description of what this migration does."""
        pass

    @abstractmethod
    def up(self, cursor: sqlite3.Cursor) -> None:
        """Apply the migration."""
        pass

    def down(self, cursor: sqlite3.Cursor) -> None:
        """Revert the migration (optional)."""
        raise NotImplementedError("Reverse migration not implemented.")
