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
        await asyncio.to_thread(database.initialize_database)

        # --- State Loading and Resumption Logic ---
        total_blocked_count = await asyncio.to_thread(
            database.get_all_blocked_users_count
        )

        if total_blocked_count == 0:
            logging.info(
                "No local cache of blocked IDs found. Fetching from the API..."
            )
            all_blocked_ids = await self.x_service.get_blocked_user_ids()
            if all_blocked_ids:
                await asyncio.to_thread(database.add_blocked_users, all_blocked_ids)
                total_blocked_count = len(all_blocked_ids)
                logging.info(f"Saved {total_blocked_count} blocked IDs to database.")
            else:
                logging.info("No blocked IDs found from the API.")
                return
        else:
            logging.info(f"Found {total_blocked_count} blocked IDs in database.")

        pending_ids = await asyncio.to_thread(database.get_pending_blocked_users)
        processed_count = await asyncio.to_thread(database.get_processed_users_count)

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

        sem = asyncio.Semaphore(20)

        async def unblock_worker(user_id: int) -> tuple[int, str]:
            async with sem:
                status = await self.x_service.unblock_user(user_id)
                # Log immediate progress for long sessions
                logging.info(
                    f"Processed ID {user_id}: {status}", extra={"single_line": True}
                )
                return user_id, status

        tasks = [unblock_worker(uid) for uid in ids_to_unblock]
        results = await asyncio.gather(*tasks)

        # Group results by status
        status_map = {"SUCCESS": [], "NOT_FOUND": [], "FAILED": []}
        for user_id, status in results:
            if status in status_map:
                status_map[status].append(user_id)

        # Batch update database
        if status_map["SUCCESS"]:
            await asyncio.to_thread(
                database.update_user_statuses, status_map["SUCCESS"], "UNBLOCKED"
            )
        if status_map["NOT_FOUND"]:
            await asyncio.to_thread(
                database.update_user_statuses, status_map["NOT_FOUND"], "NOT_FOUND"
            )
        if status_map["FAILED"]:
            await asyncio.to_thread(
                database.update_user_statuses, status_map["FAILED"], "FAILED"
            )

        # Final report
        session_unblocked_count = len(status_map["SUCCESS"])
        session_not_found_count = len(status_map["NOT_FOUND"])
        session_failed_count = len(status_map["FAILED"])

        logging.info("\n--- Unblocking Process Complete! ---")
        logging.info(
            f"Total accounts unblocked in this session: {session_unblocked_count}"
        )
        if session_not_found_count > 0:
            logging.info(
                f"Accounts not found (deleted/suspended): {session_not_found_count}"
            )
        if session_failed_count > 0:
            logging.warning(
                f"Failed to unblock {session_failed_count} accounts. They will be retried on the next run."
            )
