import logging
import asyncio
from .base_agent import BaseAgent
from ..services.x_service import XService
from .. import database


class UnfollowAgent(BaseAgent):
    """
    An agent responsible for unfollowing accounts.
    By default, it targets accounts that do not follow the user back.
    """

    def __init__(
        self,
        x_service: XService,
        non_followers_only: bool = True,
        refresh: bool = False,
    ) -> None:
        """
        Initializes the agent with a service to interact with the X API.

        Args:
            x_service: An instance of XService.
            non_followers_only: If True, only unfollow users who don't follow back.
            refresh: Optional. If True, re-fetches following/follower IDs from API.
        """
        self.x_service = x_service
        self.non_followers_only = non_followers_only
        self.refresh = refresh

    async def execute(self) -> None:
        """
        Executes the main logic of the agent.
        """
        logging.info("--- X Unfollow Agent (Async) ---")
        await self.x_service.ensure_initialized()
        await asyncio.to_thread(database.initialize_database)

        total_following_count = await asyncio.to_thread(
            database.get_all_following_users_count
        )

        if total_following_count == 0 or self.refresh:
            if self.refresh:
                logging.info(
                    "Refresh requested. Fetching latest following/follower IDs from API..."
                )
                await asyncio.to_thread(database.clear_pending_following_users)
            else:
                logging.info(
                    "No local cache of following users found. Fetching from API..."
                )

            following_ids = await self.x_service.get_following_user_ids()

            if self.non_followers_only:
                follower_ids = await self.x_service.get_follower_user_ids()
                target_ids = following_ids - follower_ids
                logging.info(
                    f"Identified {len(target_ids)} users who do not follow you back."
                )
            else:
                target_ids = following_ids

            if target_ids:
                await asyncio.to_thread(database.add_following_users, target_ids)
                total_following_count = await asyncio.to_thread(
                    database.get_all_following_users_count
                )
                logging.info(f"Saved {len(target_ids)} target users to database.")
            else:
                if not self.refresh:
                    logging.info("No users found to unfollow.")
                    return
        else:
            logging.info(f"Found {total_following_count} target users in database.")

        pending_ids = await asyncio.to_thread(database.get_pending_following_users)
        processed_count = await asyncio.to_thread(
            database.get_processed_following_count
        )

        logging.info(
            f"Already processed: {processed_count}. Remaining to unfollow: {len(pending_ids)}."
        )

        if not pending_ids:
            logging.info("All targeted accounts have been unfollowed. Nothing to do!")
            return

        await self._unfollow_user_ids(pending_ids)

    async def _unfollow_user_ids(self, ids_to_unfollow: list[int]) -> None:
        """
        Iterates through a list of user IDs and unfollows each one concurrently.
        """
        total_session = len(ids_to_unfollow)
        logging.info(
            f"Starting the unfollowing process for {total_session} accounts..."
        )

        sem = asyncio.Semaphore(20)

        async def unfollow_worker(user_id: int) -> tuple[int, str]:
            async with sem:
                status = await self.x_service.unfollow_user(user_id)
                logging.info(
                    f"Processed ID {user_id}: {status}", extra={"single_line": True}
                )
                return user_id, status

        tasks = [unfollow_worker(uid) for uid in ids_to_unfollow]
        results = await asyncio.gather(*tasks)

        # Group results by status
        status_map = {}
        for user_id, status in results:
            status_map.setdefault(status, []).append(user_id)

        # Update database based on status
        for status, uids in status_map.items():
            db_status = "UNFOLLOWED" if status == "SUCCESS" else status
            await asyncio.to_thread(database.update_following_status, uids, db_status)

        session_success = len(status_map.get("SUCCESS", []))
        session_failed = len(status_map.get("FAILED", []))

        logging.info("\n--- Unfollowing Process Complete! ---")
        logging.info(f"Total accounts unfollowed in this session: {session_success}")
        if session_failed > 0:
            logging.warning(
                f"Failed to unfollow {session_failed} accounts. They will be retried on the next run."
            )

        other_statuses = [
            status for status in status_map if status not in ("SUCCESS", "FAILED")
        ]
        for status in other_statuses:
            logging.info(f"Accounts with status '{status}': {len(status_map[status])}")
