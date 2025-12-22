import logging
import asyncio
from .base_agent import BaseAgent
from ..services.x_service import XService
from .. import database


class UnblockAgent(BaseAgent):
    """
    An agent responsible for unblocking all blocked users on an X account.
    """

    def __init__(self, x_service: XService, user_id: int | None = None) -> None:
        """
        Initializes the agent with a service to interact with the X API.

        Args:
            x_service: An instance of XService.
            user_id: Optional. A specific user ID to unblock.
        """
        self.x_service = x_service
        self.user_id = user_id

        if self.user_id is not None and not isinstance(self.user_id, int):
            raise TypeError("User ID must be an integer")

    async def execute(self) -> None:
        """
        Executes the main logic of the agent.

        Fetches the list of blocked users, stores them in the DB, and unblocks them.
        """
        # Specific user_id override
        if self.user_id is not None:
            logging.info(f"Attempting to unblock specific user ID: {self.user_id}")
            status = await self.x_service.unblock_user(self.user_id)
            if status == "SUCCESS":
                logging.info(f"Successfully unblocked {self.user_id}.")
            else:
                logging.error(f"Failed to unblock {self.user_id}: {status}")
            return

        logging.info("--- X Unblock Agent (Async) ---")
        database.initialize_database()

        # --- State Loading and Resumption Logic ---
        total_blocked_count = database.get_all_blocked_users_count()

        if total_blocked_count == 0:
            logging.info(
                "No local cache of blocked IDs found. Fetching from the API..."
            )
            all_blocked_ids = await self.x_service.get_blocked_user_ids()
            if all_blocked_ids:
                database.add_blocked_users(all_blocked_ids)
                total_blocked_count = len(all_blocked_ids)
                logging.info(f"Saved {total_blocked_count} blocked IDs to database.")
            else:
                logging.info("No blocked IDs found from the API.")
                return
        else:
            logging.info(f"Found {total_blocked_count} blocked IDs in database.")

        pending_ids = database.get_pending_blocked_users()
        processed_count = database.get_processed_users_count()

        logging.info(
            f"Already processed: {processed_count}. Remaining to unblock: {len(pending_ids)}."
        )

        if not pending_ids:
            logging.info(
                "All accounts from the list have been unblocked. Nothing to do!"
            )
            return

        # --- Unblocking Process ---
        await self._unblock_user_ids(pending_ids, total_blocked_count, processed_count)

    async def _unblock_user_ids(
        self,
        ids_to_unblock: list[int],
        total_blocked_count: int,
        already_unblocked_count: int,
    ) -> None:
        """
        Iterates through a list of user IDs and unblocks each one concurrently.
        """
        total_to_unblock_session = len(ids_to_unblock)
        logging.info(
            f"Starting the unblocking process for {total_to_unblock_session} accounts..."
        )

        session_unblocked_count = 0
        failed_count = 0

        # Limit concurrency
        sem = asyncio.Semaphore(20)

        async def unblock_worker(user_id):
            nonlocal session_unblocked_count, failed_count
            async with sem:
                status = await self.x_service.unblock_user(user_id)

                if status == "SUCCESS":
                    database.update_user_status(user_id, "UNBLOCKED")
                    session_unblocked_count += 1

                    processed_so_far = (
                        already_unblocked_count + session_unblocked_count + failed_count
                    )
                    remaining = total_blocked_count - processed_so_far
                    logging.info(
                        f"Unblocked ID {user_id}. Remaining: ~{remaining}",
                        extra={"single_line": True},
                    )

                elif status == "NOT_FOUND":
                    database.update_user_status(user_id, "NOT_FOUND")
                    failed_count += 1
                    logging.debug(f"User {user_id} not found (possibly deleted).")
                else:
                    database.update_user_status(user_id, "FAILED")
                    failed_count += 1
                    logging.warning(f"Failed to unblock {user_id}.")

        tasks = [unblock_worker(uid) for uid in ids_to_unblock]
        await asyncio.gather(*tasks)

        logging.info("\n--- Unblocking Process Complete! ---")
        logging.info(
            f"Total accounts unblocked in this session: {session_unblocked_count}"
        )
