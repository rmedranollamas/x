from abc import ABC, abstractmethod
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from ..database import DatabaseManager


class BaseAgent(ABC):
    """
    Abstract base class for all agents.
    Defines the common interface that all agents must implement.
    """

    def __init__(self, db_manager: "DatabaseManager", *args, **kwargs):
        self.db = db_manager

    @abstractmethod
    async def execute(self):
        """
        The main entry point for the agent's logic.
        This method must be implemented by all subclasses.
        """
        pass
