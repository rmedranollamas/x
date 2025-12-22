import logging
from .base_agent import BaseAgent
from ..services.x_service import XService


class BlockedIdsAgent(BaseAgent):
    """
    An agent responsible for fetching and printing the list of blocked user IDs.
    """

    def __init__(self, x_service: XService) -> None:
        """
        Initializes the agent with a service to interact with the X API.

        Args:
            x_service: An instance of XService.
        """
        self.x_service = x_service

    async def execute(self) -> None:
        """
        Executes the main logic of the agent.

        Fetches the list of blocked users and prints the user IDs to the console.
        """
        logging.info("--- X Blocked IDs Agent ---")
        api_blocked_ids = await self.x_service.get_blocked_user_ids()
        if api_blocked_ids:
            logging.info(f"Found {len(api_blocked_ids)} blocked user IDs:")
            for user_id in api_blocked_ids:
                print(user_id)
        else:
            logging.info("No blocked IDs found from the API.")
