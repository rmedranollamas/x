import logging
from .base_agent import BaseAgent
from ..services.x_service import XService
from .. import database


class UnblockAgent(BaseAgent):
    """
    An agent responsible for unblocking all blocked users on an X account.
    """

    def __init__(self, x_service: XService) -> None:
        """
        Initializes the agent with a service to interact with the X API.

        Args:
            x_service: An instance of XService.
        """
        self.x_service = x_service

    def execute(self) -> None:
        """
        Executes the main logic of the agent.

        Fetches the list of blocked users, compares it with already unblocked
        users, and unblocks the remaining accounts.
        """
        logging.info("--- X Unblock Agent ---")
        database.initialize_database()

        # --- State Loading and Resumption Logic ---
        logging.info("Fetching latest blocked IDs from the API to sync...")
        api_blocked_ids = self.x_service.get_blocked_user_ids()

        logging.info(f"[DEBUG] API returned {len(api_blocked_ids)} blocked IDs.")

        if api_blocked_ids:
            logging.info("[DEBUG] Entering API sync block. Using API list as queue.")
            database.add_blocked_ids_to_db(set(api_blocked_ids))
            logging.info(
                f"Synced {len(api_blocked_ids)} blocked IDs from API to database."
            )
            # If we have fresh data from the API, trust it 100%.
            # Any ID returned by the API is currently blocked and must be unblocked.
            # We do NOT filter by 'completed_ids' here because a user might have been
            # unblocked in the past (and marked as such in DB) but is now re-blocked.
            ids_to_unblock = sorted(api_blocked_ids)
            total_count = len(ids_to_unblock)
            # For reporting purposes, we can count how many we *thought* were done
            completed_ids = database.get_unblocked_ids_from_db()
            already_unblocked_count = len(completed_ids)

        else:
            logging.info("No blocked IDs returned from the API.")
            # Fallback to DB if API returned nothing (and didn't crash).
            # This path is rare given XService implementation but good for safety.
            all_blocked_ids = list(database.get_all_blocked_ids_from_db())

            if not all_blocked_ids:
                logging.info("No blocked IDs found in database or API. Nothing to do.")
                return

            completed_ids = database.get_unblocked_ids_from_db()
            logging.info(
                f"Loaded {len(completed_ids)} already unblocked IDs from the database."
            )

            # In fallback mode, we must filter, otherwise we'd loop forever on the DB list.
            ids_to_unblock = [
                uid for uid in all_blocked_ids if uid not in completed_ids
            ]
            # Sort ascending here as well for consistency
            ids_to_unblock.sort()
            total_count = len(all_blocked_ids)
            already_unblocked_count = len(completed_ids)

        logging.info(f"[DEBUG] Final ids_to_unblock count: {len(ids_to_unblock)}")

        if not ids_to_unblock:
            logging.info(
                "All accounts from the list have been unblocked. Nothing to do!"
            )
            return

        # --- Unblocking Process ---
        self._unblock_user_ids(ids_to_unblock, total_count, already_unblocked_count)

    def _unblock_user_ids(
        self,
        ids_to_unblock: list[int],
        total_blocked_count: int,
        already_unblocked_count: int,
    ) -> None:
        """
        Iterates through a list of user IDs and unblocks each one.

        Args:
            ids_to_unblock: A list of user IDs to unblock (ordered).
            total_blocked_count: The total number of users who were blocked.
            already_unblocked_count: The number of users already unblocked.
        """
        total_to_unblock_session = len(ids_to_unblock)
        logging.info(
            f"Starting the unblocking process for {total_to_unblock_session} accounts..."
        )

        session_unblocked_count = 0
        failed_ids = []

        for user_id in ids_to_unblock:
            result = self.x_service.unblock_user(user_id)

            # unblock_user returns True on success, "NOT_FOUND" for deleted users,
            # and None for other errors.
            if result == "NOT_FOUND":
                # User not found, so we can consider the "unblocking" task for this ID complete.
                database.mark_user_as_unblocked_in_db(user_id)
                continue

            if result is None:
                # A non-specific error occurred, which was logged by the service.
                # We'll add it to the list of failures for this session and not mark it as complete,
                # allowing it to be retried on the next run.
                failed_ids.append(user_id)
                continue

            session_unblocked_count += 1
            database.mark_user_as_unblocked_in_db(user_id)

            total_unblocked = already_unblocked_count + session_unblocked_count
            remaining = total_blocked_count - total_unblocked

            logging.info(
                f"({total_unblocked}/{total_blocked_count}) Successfully unblocked ID: {user_id}. Remaining: {remaining}."
            )

        logging.info("--- Unblocking Process Complete! ---")
        logging.info(
            f"Total accounts unblocked in this session: {session_unblocked_count}"
        )
        if failed_ids:
            logging.warning(
                f"Failed to unblock {len(failed_ids)} accounts. Check logs for details."
            )
            logging.warning(f"Failed IDs: {failed_ids}")
