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

        if api_blocked_ids:
            database.add_blocked_ids_to_db(api_blocked_ids)
            logging.info(
                f"Synced {len(api_blocked_ids)} blocked IDs from API to database."
            )
        else:
            logging.info("No blocked IDs returned from the API.")

        all_blocked_ids = database.get_all_blocked_ids_from_db()

        if not all_blocked_ids:
            logging.info("No blocked IDs found in database or API. Nothing to do.")
            return

        logging.info(
            f"Total blocked IDs tracked in database: {len(all_blocked_ids)}"
        )

        completed_ids = database.get_unblocked_ids_from_db()
        logging.info(
            f"Loaded {len(completed_ids)} already unblocked IDs from the database."
        )

        ids_to_unblock = all_blocked_ids - completed_ids

        if not ids_to_unblock:
            logging.info(
                "All accounts from the list have been unblocked. Nothing to do!"
            )
            return

        # --- Unblocking Process ---
        self._unblock_user_ids(ids_to_unblock, len(all_blocked_ids), len(completed_ids))

    def _unblock_user_ids(
        self,
        ids_to_unblock: set[int],
        total_blocked_count: int,
        already_unblocked_count: int,
    ) -> None:
        """
        Iterates through a set of user IDs and unblocks each one.

        Args:
            ids_to_unblock: A set of user IDs to unblock.
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
            user_details = self.x_service.unblock_user(user_id)

            # unblock_user returns a user object on success, "NOT_FOUND" for deleted users,
            # and None for other errors.
            if user_details == "NOT_FOUND":
                # User not found, so we can consider the "unblocking" task for this ID complete.
                database.mark_user_as_unblocked_in_db(user_id)
                continue

            if user_details is None:
                # A non-specific error occurred, which was logged by the service.
                # We'll add it to the list of failures for this session and not mark it as complete,
                # allowing it to be retried on the next run.
                failed_ids.append(user_id)
                continue

            session_unblocked_count += 1
            database.mark_user_as_unblocked_in_db(user_id)

            username = f"@{user_details.screen_name}"
            total_unblocked = already_unblocked_count + session_unblocked_count
            remaining = total_blocked_count - total_unblocked

            logging.info(
                f"({total_unblocked}/{total_blocked_count}) Successfully unblocked {username} (ID: {user_id}). Remaining: {remaining}."
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
