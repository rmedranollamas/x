from abc import ABC, abstractmethod


class BaseAgent(ABC):
    """
    Abstract base class for all agents.
    Defines the common interface that all agents must implement.
    """

    @abstractmethod
    async def execute(self):
        """
        The main entry point for the agent's logic.
        This method must be implemented by all subclasses.
        """
        pass
