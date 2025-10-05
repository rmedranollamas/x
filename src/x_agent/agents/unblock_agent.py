import os
import logging
from .base_agent import BaseAgent
from ..services.x_service import XService


class UnblockAgent(BaseAgent):
    """
    An agent responsible for unblocking all blocked users on an X account.
    """

    BLOCKED_IDS_FILE = "blocked_ids.txt"
    UNBLOCKED_IDS_FILE = "unblocked_ids.txt"

    def __init__(self, x_service: XService):
        """
        Initializes the UnblockAgent with a dependency on the XService.

        Args:
            x_service (XService): An instance of the XService to interact with the X API.
        """
        self.x_service = x_service

    def _load_ids_from_file(self, filename):
        """Loads a set of user IDs from a text file."""
        if not os.path.exists(filename):
            return set()
        ids = set()
        with open(filename, "r") as f:
            for i, line in enumerate(f, 1):
                stripped_line = line.strip()
                if stripped_line:
                    try:
                        ids.add(int(stripped_line))
                    except ValueError:
                        logging.warning(
                            f'Skipping invalid non-integer value in {filename} on line {i}: "{stripped_line}"'
                        )
        return ids

    def _save_ids_to_file(self, filename, ids):
        """Saves a list or set of user IDs to a text file."""
        with open(filename, "w") as f:
            for user_id in ids:
                f.write(f"{user_id}\n")

    def _append_id_to_file(self, filename, user_id):
        """Appends a single user ID to a text file."""
        with open(filename, "a") as f:
            f.write(f"{user_id}\n")

    def execute(self):
        """
        Executes the main logic of the unblocking agent.
        """
        logging.info("--- X Unblock Agent ---")

        # --- State Loading and Resumption Logic ---
        all_blocked_ids = self._load_ids_from_file(self.BLOCKED_IDS_FILE)

        if not all_blocked_ids:
            logging.info(
                "No local cache of blocked IDs found. Fetching from the API..."
            )
            all_blocked_ids = self.x_service.get_blocked_user_ids()
            self._save_ids_to_file(self.BLOCKED_IDS_FILE, all_blocked_ids)
            logging.info(
                f"Saved {len(all_blocked_ids)} blocked IDs to {self.BLOCKED_IDS_FILE}."
            )
        else:
            logging.info(
                f"Loaded {len(all_blocked_ids)} blocked IDs from {self.BLOCKED_IDS_FILE}."
            )

        completed_ids = self._load_ids_from_file(self.UNBLOCKED_IDS_FILE)
        logging.info(
            f"Loaded {len(completed_ids)} already unblocked IDs from {self.UNBLOCKED_IDS_FILE}."
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
        ids_to_unblock,
        total_blocked_count,
        already_unblocked_count,
    ):
        """Iterates through the list of user IDs and unblocks them."""
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
                self._append_id_to_file(self.UNBLOCKED_IDS_FILE, user_id)
                continue

            if user_details is None:
                # A non-specific error occurred, which was logged by the service.
                # We'll add it to the list of failures for this session and not mark it as complete,
                # allowing it to be retried on the next run.
                failed_ids.append(user_id)
                continue

            session_unblocked_count += 1
            self._append_id_to_file(self.UNBLOCKED_IDS_FILE, user_id)

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

    def _is_user_not_found_error(self, user_id):
        """
        A placeholder to simulate checking for a "Not Found" error condition.
        In a real implementation, the XService would provide a clearer status.
        """
        # This is a simplification. We assume that if the user_details is None,
        # and we can't find them again, they are "not found".
        return True
