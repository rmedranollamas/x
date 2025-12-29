import logging
import asyncio
import time
from typing import TYPE_CHECKING
from .base_agent import BaseAgent
from ..services.x_service import XService

if TYPE_CHECKING:
    from ..database import DatabaseManager


class UnblockAgent(BaseAgent):
    """
    An agent responsible for unblocking all blocked users on an X account.
    """

    def __init__(
        self,
        x_service: XService,
        db_manager: "DatabaseManager",
        dry_run: bool = False,
        user_id: int | None = None,
        refresh: bool = False,
    ) -> None:
        """
        Initializes the agent.

        Args:
            x_service: An instance of XService.
            db_manager: Database Manager instance.
            dry_run: If True, simulate actions.
            user_id: Optional. A specific user ID to unblock.
            refresh: Optional. If True, re-fetches blocked IDs from API.
        """
        super().__init__(db_manager)
        self.x_service = x_service
        self.dry_run = dry_run
        self.user_id = user_id
        self.refresh = refresh

        if self.user_id is not None and not isinstance(self.user_id, int):
            raise TypeError("User ID must be an integer")

    async def execute(self) -> None:
        """
        Executes the main logic of the agent.

        Fetches the list of blocked users, stores them in the DB, and unblocks them.
        """
        await self.x_service.ensure_initialized()
        await asyncio.to_thread(self.db.initialize_database)

        if self.dry_run:
            logging.info("DRY RUN ENABLED: No changes will be made to X.")

        # Specific user_id override
        if self.user_id is not None:
            logging.info(f"Attempting to unblock specific user ID: {self.user_id}")
            if self.dry_run:
                status = "SUCCESS"
                logging.info(f"[Dry Run] Would unblock {self.user_id}")
            else:
                status = await self.x_service.unblock_user(self.user_id)

            if status == "SUCCESS":
                logging.info(f"Successfully unblocked {self.user_id}.")
                await asyncio.to_thread(
                    self.db.update_user_status, self.user_id, "UNBLOCKED"
                )
            else:
                logging.error(f"Failed to unblock {self.user_id}: {status}")
                await asyncio.to_thread(
                    self.db.update_user_status, self.user_id, "FAILED"
                )
            return

        logging.info("--- X Unblock Agent (Async) ---")

        # --- State Loading and Resumption Logic ---
        total_blocked_count = await asyncio.to_thread(
            self.db.get_all_blocked_users_count
        )

        if total_blocked_count == 0 or self.refresh:
            if self.refresh:
                logging.info(
                    "Refresh requested. Fetching latest blocked IDs from API..."
                )
                await asyncio.to_thread(self.db.clear_pending_blocked_users)
            else:
                logging.info(
                    "No local cache of blocked IDs found. Fetching from the API..."
                )

            all_blocked_ids = await self.x_service.get_blocked_user_ids()
            if all_blocked_ids:
                await asyncio.to_thread(self.db.add_blocked_users, all_blocked_ids)
                total_blocked_count = await asyncio.to_thread(
                    self.db.get_all_blocked_users_count
                )
                logging.info(f"Saved {len(all_blocked_ids)} blocked IDs to database.")
            else:
                if not self.refresh:
                    logging.info("No blocked IDs found from the API.")
                    return

        pending_ids = await asyncio.to_thread(self.db.get_pending_blocked_users)
        processed_count = await asyncio.to_thread(self.db.get_processed_users_count)

        logging.info(
            f"Already processed: {processed_count}. Remaining to unblock: {len(pending_ids)}."
        )

        if not pending_ids:
            logging.info(
                "All accounts from the list have been unblocked. Nothing to do!"
            )
            return

        # --- Unblocking Process ---
        await self._unblock_user_ids(pending_ids)

    async def _unblock_user_ids(
        self,
        ids_to_unblock: list[int],
    ) -> None:
        """
        Iterates through a list of user IDs and unblocks each one concurrently in batches.
        """
        total_to_unblock_session = len(ids_to_unblock)
        logging.info(
            f"Starting the unblocking process for {total_to_unblock_session} accounts..."
        )

        start_time = time.time()
        sem = asyncio.Semaphore(20)
        # We'll collect results and update the DB in chunks to ensure progress is saved
        batch_size = 50
        session_stats = {"SUCCESS": 0, "NOT_FOUND": 0, "FAILED": 0}

        async def unblock_worker(user_id: int) -> tuple[int, str]:
            if self.dry_run:
                # Simulate work
                logging.info(
                    f"[Dry Run] Would unblock {user_id}", extra={"single_line": True}
                )
                return user_id, "SUCCESS"

            async with sem:
                status = await self.x_service.unblock_user(user_id)
                logging.info(
                    f"Processed ID {user_id}: {status}", extra={"single_line": True}
                )
                return user_id, status

        for i in range(0, len(ids_to_unblock), batch_size):
            chunk = ids_to_unblock[i : i + batch_size]
            tasks = [unblock_worker(uid) for uid in chunk]
            results = await asyncio.gather(*tasks)

            # Process chunk results
            chunk_status_map = {"SUCCESS": [], "NOT_FOUND": [], "FAILED": []}
            for user_id, status in results:
                if status in chunk_status_map:
                    chunk_status_map[status].append(user_id)
                    session_stats[status] += 1

            # Update database for this chunk
            for status, uids in chunk_status_map.items():
                if uids:
                    await asyncio.to_thread(
                        self.db.update_user_statuses,
                        uids,
                        status if status != "SUCCESS" else "UNBLOCKED",
                    )

        end_time = time.time()
        duration = end_time - start_time
        total_processed = sum(session_stats.values())
        rate = total_processed / duration if duration > 0 else 0

        logging.info("\n--- Unblocking Process Complete! ---")
        logging.info(f"Time taken: {duration:.2f} seconds")
        logging.info(f"Rate: {rate:.2f} accounts/second")
        logging.info(
            f"Total accounts unblocked in this session: {session_stats['SUCCESS']}"
        )
        if session_stats["NOT_FOUND"] > 0:
            logging.info(
                f"Accounts not found (deleted/suspended): {session_stats['NOT_FOUND']}"
            )
        if session_stats["FAILED"] > 0:
            logging.warning(
                f"Failed to unblock {session_stats['FAILED']} accounts. They will be retried on the next run."
            )
