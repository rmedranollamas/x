import os
import sys
import time
import tweepy
import logging
import argparse
from dotenv import load_dotenv

def countdown(seconds, message="Waiting..."):
    """Displays a countdown timer for a given duration using logging."""
    for i in range(seconds, 0, -1):
        mins, secs = divmod(i, 60)
        timer = f"{mins:02d}:{secs:02d}"
        logging.info(f"{message} {timer}", extra={'single_line': True})
        time.sleep(1)
    # Clear the line after countdown finishes
    logging.info("", extra={'single_line': True}) # Clear the line

# --- Constants ---
RATE_LIMIT_THRESHOLD = 50
RATE_LIMIT_PAUSE_SECONDS = 901

# --- Custom Logging Handler for Single-Line Updates ---
class SingleLineUpdateHandler(logging.StreamHandler):
    """A logging handler that uses carriage returns to update a single line."""
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._last_single_line_length = 0

    def emit(self, record):
        if record.levelno == logging.INFO and hasattr(record, 'single_line'):
            message = self.format(record)
            # Clear the previous line if necessary
            if self._last_single_line_length > len(message):
                print(" " * self._last_single_line_length, end="\r", flush=True)
            print(f"\r{message}", end="", flush=True)
            self._last_single_line_length = len(message)
        else:
            # If a non-single-line record comes, clear any active single-line message
            if self._last_single_line_length > 0:
                print(" " * self._last_single_line_length, end="\r", flush=True)
                self._last_single_line_length = 0
            super().emit(record)

def setup_arguments_and_logging():
    """Sets up argument parser and configures logging."""
    parser = argparse.ArgumentParser(description="Unblock all blocked accounts on your X profile.")
    parser.add_argument("--debug", action="store_true", help="Enable debug logging for detailed output.")
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
    formatter = logging.Formatter('%(asctime)s - %(levelname)s - %(message)s', datefmt='%Y-%m-%d %H:%M:%S')
    handler.setFormatter(formatter)
    logger.addHandler(handler)
    
    return args

def create_tweepy_client():
    """Loads credentials and creates an authenticated Tweepy client."""
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
        client = tweepy.Client(
            consumer_key=api_key,
            consumer_secret=api_key_secret,
            access_token=access_token,
            access_token_secret=access_token_secret,
            wait_on_rate_limit=True,
        )
        auth_user = client.get_me(user_fields=["id"])
        logging.debug(f"Authenticated as user ID: {auth_user.data.id}")
        logging.info("Authentication successful.")
        return client
    except tweepy.errors.TweepyException as e:
        logging.error(f"Error during API authentication: {e}", exc_info=True)
        sys.exit(1)

def fetch_blocked_users(client):
    """Fetches the complete list of blocked user IDs from the API."""
    logging.info("Fetching blocked accounts... (This might take a moment)")
    blocked_users = []
    try:
        logging.debug("Starting to paginate through blocked users from the API.")
        for response in tweepy.Paginator(client.get_blocked, max_results=100):
            if response.data:
                logging.debug(f"Fetched a batch of {len(response.data)} blocked users.")
                blocked_users.extend(response.data)
                logging.info(f"Found {len(blocked_users)} blocked accounts...", extra={'single_line': True})
        logging.info(f"Finished fetching. Found a total of {len(blocked_users)} blocked accounts.")
        return blocked_users

    except tweepy.errors.TweepyException as e:
        logging.error(f"An unexpected error occurred while fetching: {e}", exc_info=True)
        sys.exit(1)

def unblock_users(client, blocked_users):
    """Iterates through the list of users and unblocks them."""
    total_blocked = len(blocked_users)
    if total_blocked == 0:
        logging.info("You have no blocked accounts. Nothing to do!")
        return

    logging.info(f"Starting the unblocking process for {total_blocked} accounts...")
    estimated_minutes = (total_blocked // RATE_LIMIT_THRESHOLD) * (RATE_LIMIT_PAUSE_SECONDS // 60)
    logging.info(f"Estimated time to complete is around {estimated_minutes} minutes.")

    unblocked_count = 0
    for user in blocked_users:
        try:
            logging.debug(f"Attempting to unblock user @{user.username} (ID: {user.id})...")
            client.unblock(target_user_id=user.id)
            unblocked_count += 1
            logging.debug(f"Successfully unblocked @{user.username}.")
            logging.info(f"({unblocked_count}/{total_blocked}) Unblocked @{user.username}", extra={'single_line': True})

        except tweepy.errors.TweepyException as e:
            logging.error(f"Could not unblock @{user.username}. Reason: {e}", exc_info=True)
    logging.info(f"--- Unblocking Process Complete! ---")
    logging.info(f"Total accounts unblocked: {unblocked_count}")

def main():
    """
    Main function to run the X unblocking tool.
    """
    setup_arguments_and_logging()
    logging.info("--- X Unblocker Tool ---")
    
    client = create_tweepy_client()
    blocked_users = fetch_blocked_users(client)
    unblock_users(client, blocked_users)



if __name__ == "__main__":
    main()
