import os
import sys
import time
import tweepy
import logging
import argparse
from dotenv import load_dotenv


def countdown(seconds, message="Waiting..."):
    """Displays a single message and waits for a given duration."""
    if seconds > 0:
        logging.info(message)
        time.sleep(seconds)


def handle_rate_limit(e):
    """Handles rate limit errors by parsing the reset time and waiting."""
    # Extract the reset time from the API response
    try:
        reset_timestamp = int(e.response.headers.get("x-rate-limit-reset", 0))
    except (ValueError, TypeError):
        reset_timestamp = 0
    
    if reset_timestamp > 0:
        # Calculate wait time
        wait_seconds = max(0, reset_timestamp - int(time.time()))
        resume_time = time.strftime("%H:%M:%S", time.localtime(reset_timestamp))
        mins = round(wait_seconds / 60)

        # Log and start countdown
        countdown_message = f"Rate limit reached. Waiting for ~{mins} minutes. Resuming at {resume_time}."
        countdown(wait_seconds, countdown_message)
    else:
        # Fallback if the header is missing
        countdown(
            15 * 60,
            "Rate limit reached, but reset time is unknown. Waiting for 15 minutes as a fallback.",
        )


# --- Constants ---
BLOCKED_IDS_FILE = "blocked_ids.txt"
UNBLOCKED_IDS_FILE = "unblocked_ids.txt"


# --- State Persistence Functions ---
def load_ids_from_file(filename):
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


def save_ids_to_file(filename, ids):
    """Saves a list or set of user IDs to a text file."""
    with open(filename, "w") as f:
        for user_id in ids:
            f.write(f"{user_id}\n")


def append_id_to_file(filename, user_id):
    """Appends a single user ID to a text file."""
    with open(filename, "a") as f:
        f.write(f"{user_id}\n")


# --- Custom Logging Handler for Single-Line Updates ---
class SingleLineUpdateHandler(logging.StreamHandler):
    """A logging handler that uses carriage returns to update a single line."""

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._last_single_line_length = 0

    def emit(self, record):
        message = self.format(record)
        if record.levelno == logging.INFO and hasattr(record, "single_line"):
            # Clear the previous line if necessary
            if self._last_single_line_length > len(message):
                print(
                    " " * self._last_single_line_length,
                    end="\r",
                    file=sys.stdout,
                    flush=True,
                )
            print(f"\r{message}", end="", file=sys.stdout, flush=True)
            self._last_single_line_length = len(message)
        else:
            # If a non-single-line record comes, clear any active single-line message
            if self._last_single_line_length > 0:
                print(
                    " " * self._last_single_line_length,
                    end="\r",
                    file=sys.stdout,
                    flush=True,
                )
                self._last_single_line_length = 0
            print(message, file=sys.stdout, flush=True)


def setup_arguments_and_logging():
    """Sets up argument parser and configures logging."""
    parser = argparse.ArgumentParser(
        description="Unblock all blocked accounts on your X profile."
    )
    parser.add_argument(
        "--debug", action="store_true", help="Enable debug logging for detailed output."
    )
    args = parser.parse_args()

    # Configure logging
    log_level = logging.DEBUG if args.debug else logging.INFO

    # Get the root logger
    logger = logging.getLogger()
    logger.setLevel(log_level)

    # Remove any existing handlers
    for handler in logger.handlers[:]:
        logger.removeHandler(handler)

    # Create our custom handler and add it
    handler = SingleLineUpdateHandler()
    formatter = logging.Formatter(
        "%(asctime)s - %(levelname)s - %(message)s", datefmt="%Y-%m-%d %H:%M:%S"
    )
    handler.setFormatter(formatter)
    logger.addHandler(handler)

    return args


def create_tweepy_clients():
    """Loads credentials and creates authenticated Tweepy clients for v1.1 and v2."""
    logging.debug("Loading environment variables from .env file...")
    load_dotenv()
    logging.debug("Environment variables loaded.")

    logging.debug("Fetching API credentials from environment variables.")
    api_key = os.getenv("X_API_KEY")
    api_key_secret = os.getenv("X_API_KEY_SECRET")
    access_token = os.getenv("X_ACCESS_TOKEN")
    access_token_secret = os.getenv("X_ACCESS_TOKEN_SECRET")

    if not all([api_key, api_key_secret, access_token, access_token_secret]):
        logging.error("Missing API credentials. Check your .env file.")
        sys.exit(1)

    logging.info("Successfully loaded API credentials.")

    try:
        logging.info("Authenticating with the X API...")
        # v2 client for unblocking
        client_v2 = tweepy.Client(
            consumer_key=api_key,
            consumer_secret=api_key_secret,
            access_token=access_token,
            access_token_secret=access_token_secret,
            wait_on_rate_limit=False,
        )
        auth_user = client_v2.get_me(user_fields=["id"])
        logging.debug(f"Authenticated as user ID: {auth_user.data.id}")

        # v1.1 client for fetching blocked IDs
        auth = tweepy.OAuth1UserHandler(
            api_key, api_key_secret, access_token, access_token_secret
        )
        api_v1 = tweepy.API(auth, wait_on_rate_limit=False)

        logging.info("Authentication successful for both API v1.1 and v2.")
        return api_v1, client_v2
    except Exception as e:
        logging.error(f"Error during API authentication: {e}", exc_info=True)
        sys.exit(1)


def fetch_blocked_user_ids(api_v1):
    """Fetches the complete list of blocked user IDs from the API v1.1."""
    logging.info("Fetching blocked account IDs... (This will be fast)")
    blocked_user_ids = set()
    cursor = -1
    while True:
        try:
            logging.debug(f"Fetching blocked IDs with cursor: {cursor}")
            ids, (prev_cursor, next_cursor) = api_v1.get_blocked_ids(cursor=cursor)
            blocked_user_ids.update(ids)
            logging.info(
                f"Found {len(blocked_user_ids)} blocked account IDs...",
                extra={"single_line": True},
            )
            cursor = next_cursor
            if cursor == 0:
                break

        except tweepy.errors.TooManyRequests as e:
            handle_rate_limit(e)
            # After waiting, the loop will retry with the same cursor
            continue

        except Exception as e:
            logging.error(
                f"An unexpected error occurred while fetching: {e}", exc_info=True
            )
            sys.exit(1)

    logging.info(
        f"Finished fetching. Found a total of {len(blocked_user_ids)} blocked account IDs."
    )
    return blocked_user_ids


def unblock_user_ids(
    api_v1, blocked_user_ids, total_blocked_count, already_unblocked_count
):
    """Iterates through the list of user IDs and unblocks them, saving progress."""
    total_to_unblock_session = len(blocked_user_ids)
    if total_to_unblock_session == 0:
        logging.info(
            "All previously blocked accounts have been unblocked. Nothing to do!"
        )
        return

    logging.info(
        f"Starting the unblocking process for {total_to_unblock_session} accounts..."
    )

    session_unblocked_count = 0
    failed_ids = []

    ids_to_process = list(blocked_user_ids)
    index = 0
    while index < len(ids_to_process):
        user_id = ids_to_process[index]
        try:
            logging.debug(f"Attempting to unblock user ID: {user_id}...")
            user_details = api_v1.destroy_block(user_id=user_id)
            session_unblocked_count += 1
            append_id_to_file(UNBLOCKED_IDS_FILE, user_id)

            username = f"@{user_details.screen_name}"

            total_unblocked = already_unblocked_count + session_unblocked_count
            remaining = total_blocked_count - total_unblocked

            logging.info(
                f"({total_unblocked}/{total_blocked_count}) Successfully unblocked {username} (ID: {user_id}). Remaining: {remaining}."
            )

            index += 1  # Move to the next user

        except tweepy.errors.NotFound:
            logging.warning(
                f"User ID {user_id} not found. The account may have been deleted. Skipping."
            )
            # Add to completed list to avoid retrying
            append_id_to_file(UNBLOCKED_IDS_FILE, user_id)
            index += 1
            continue

        except tweepy.errors.TooManyRequests as e:
            handle_rate_limit(e)
            # After waiting, the loop will retry with the same user_id
            continue

        except Exception as e:
            logging.error(
                f"Could not unblock user ID {user_id}. Reason: {e}", exc_info=True
            )
            failed_ids.append(user_id)
            index += 1  # Move to the next user after a failure

    logging.info("--- Unblocking Process Complete! ---")
    logging.info(f"Total accounts unblocked in this session: {session_unblocked_count}")
    if failed_ids:
        logging.warning(f"Failed to unblock {len(failed_ids)} accounts. Check logs for details.")
        logging.warning(f"Failed IDs: {failed_ids}")


def main():
    """
    Main function to run the X unblocking tool.
    """
    setup_arguments_and_logging()
    logging.info("--- X Unblocker Tool ---")

    # --- State Loading and Resumption Logic ---
    all_blocked_ids = load_ids_from_file(BLOCKED_IDS_FILE)

    api_v1, client_v2 = None, None

    if not all_blocked_ids:
        logging.info("No local cache of blocked IDs found. Fetching from the API...")
        api_v1, client_v2 = create_tweepy_clients()
        all_blocked_ids = fetch_blocked_user_ids(api_v1)
        save_ids_to_file(BLOCKED_IDS_FILE, all_blocked_ids)
        logging.info(f"Saved {len(all_blocked_ids)} blocked IDs to {BLOCKED_IDS_FILE}.")
    else:
        logging.info(
            f"Loaded {len(all_blocked_ids)} blocked IDs from {BLOCKED_IDS_FILE}."
        )

    completed_ids = load_ids_from_file(UNBLOCKED_IDS_FILE)
    logging.info(
        f"Loaded {len(completed_ids)} already unblocked IDs from {UNBLOCKED_IDS_FILE}."
    )

    ids_to_unblock = all_blocked_ids - completed_ids

    if not ids_to_unblock:
        logging.info("All accounts from the list have been unblocked. Nothing to do!")
        sys.exit(0)

    # --- Unblocking Process ---
    # Create clients only if we need them
    if not api_v1:
        api_v1, _ = create_tweepy_clients()

    total_blocked_count = len(all_blocked_ids)
    already_unblocked_count = len(completed_ids)
    unblock_user_ids(
        api_v1, ids_to_unblock, total_blocked_count, already_unblocked_count
    )


if __name__ == "__main__":
    main()
