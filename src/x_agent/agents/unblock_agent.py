import os
import logging
from .base_agent import BaseAgent
from ..services.x_service import XService


class UnblockAgent(BaseAgent):
    """
    An agent responsible for unblocking all blocked users on an X account.
    """

    BLOCKED_IDS_FILE = ".state/blocked_ids.txt"
    UNBLOCKED_IDS_FILE = ".state/unblocked_ids.txt"

    def __init__(self, x_service: XService) -> None:
        """
        Initializes the agent with a service to interact with the X API.

        Args:
            x_service: An instance of XService.
        """
        self.x_service = x_service

    def _load_ids_from_file(self, filename: str) -> set[int]:
        """
        Loads a set of user IDs from a text file, skipping invalid entries.

        Args:
            filename: The path to the file.

        Returns:
            A set of integer user IDs.
        """
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

    def _save_ids_to_file(self, filename: str, ids: set[int]) -> None:
        """
        Saves a set of user IDs to a text file.

        Args:
            filename: The path to the file.
            ids: A set of integer user IDs to save.
        """
        with open(filename, "w") as f:
            for user_id in ids:
                f.write(f"{user_id}\n")

    def _append_id_to_file(self, filename: str, user_id: int) -> None:
        """
        Appends a single user ID to a text file.

        Args:
            filename: The path to the file.
            user_id: The integer user ID to append.
        """
        with open(filename, "a") as f:
            f.write(f"{user_id}\n")

    def execute(self) -> None:
        """
        Executes the main logic of the agent.

        Fetches the list of blocked users, compares it with already unblocked
        users, and unblocks the remaining accounts.
        """
        logging.info("--- X Unblock Agent ---")

        # --- State Loading and Resumption Logic ---
        all_blocked_ids = self._load_ids_from_file(self.BLOCKED_IDS_FILE)

        if not all_blocked_ids:
            logging.info(
                "No local cache of blocked IDs found. Fetching from the API..."
            )
            all_blocked_ids = self.x_service.get_blocked_user_ids()
            if all_blocked_ids:
                self._save_ids_to_file(self.BLOCKED_IDS_FILE, all_blocked_ids)
                logging.info(
                    f"Saved {len(all_blocked_ids)} blocked IDs to {self.BLOCKED_IDS_FILE}."
                )
            else:
                logging.info("No blocked IDs found from the API.")
                return
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
