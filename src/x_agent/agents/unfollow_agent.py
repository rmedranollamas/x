import logging
import asyncio
from typing import TYPE_CHECKING
from .base_agent import BaseAgent
from ..services.x_service import XService

if TYPE_CHECKING:
    from ..database import DatabaseManager


class UnfollowAgent(BaseAgent):
    """
    An agent responsible for detecting who has unfollowed the user.
    It compares the current follower list with the previously stored one.
    """

    def __init__(
        self,
        x_service: XService,
        db_manager: "DatabaseManager",
        dry_run: bool = False,
        **kwargs,
    ) -> None:
        """
        Initializes the agent.

        Args:
            x_service: An instance of XService.
            db_manager: Database Manager.
            dry_run: If True, simulate actions.
        """
        super().__init__(db_manager)
        self.x_service = x_service
        self.dry_run = dry_run

    async def execute(self) -> None:
        """
        Executes the unfollow detection logic.
        1) Gets current follower list from the API.
        2) Compares to the follower list stored in the DB.
        3) Stores the new follower list.
        4) Shows stats and stores unfollow events in the DB.
        """
        logging.info("--- X Unfollow Detection Agent ---")
        await self.x_service.ensure_initialized()
        await asyncio.to_thread(self.db.initialize_database)

        # 1) Get current follower list from the API
        logging.info("Fetching current followers from X API...")
        current_followers = await self.x_service.get_follower_user_ids()

        # 2) Compare to the follower list stored on the DB
        logging.info("Comparing with previously stored followers...")
        previous_followers = await asyncio.to_thread(self.db.get_all_follower_ids)

        if not previous_followers:
            logging.info(
                "No previous follower data found. This is likely the first run."
            )
            unfollowed_ids = set()
            new_followers_count = len(current_followers)
        else:
            unfollowed_ids = previous_followers - current_followers
            new_followers = current_followers - previous_followers
            new_followers_count = len(new_followers)

        # 3) Store the new follower list
        if self.dry_run:
            logging.info("[Dry Run] Would update follower list in database.")
        else:
            logging.info("Updating follower list in database...")
            await asyncio.to_thread(self.db.replace_followers, current_followers)

        # 4) Show stats and store unfollow events
        self._report_stats(len(current_followers), unfollowed_ids, new_followers_count)

        if unfollowed_ids:
            if self.dry_run:
                logging.info(
                    f"[Dry Run] Would log {len(unfollowed_ids)} unfollow events."
                )
            else:
                logging.info(f"Logging {len(unfollowed_ids)} unfollow events...")
                await asyncio.to_thread(self.db.log_unfollows, list(unfollowed_ids))

        logging.info("Unfollow detection completed.")

    def _report_stats(
        self, current_total: int, unfollowed_ids: set[int], new_followers_count: int
    ) -> None:
        """Reports the findings to the console."""
        print("\n--- Unfollow Detection Report ---")
        print(f"Total Followers: {current_total}")
        print(f"New Followers:   {new_followers_count}")
        print(f"Unfollows:       {len(unfollowed_ids)}")

        if unfollowed_ids:
            print("\nIDs that unfollowed you:")
            for uid in sorted(unfollowed_ids):
                print(f" - {uid}")

        print("---------------------------------\n")
