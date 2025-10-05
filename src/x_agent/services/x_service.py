import os
import sys
import time
import tweepy
import logging
from dotenv import load_dotenv


class XService:
    """
    A service class to encapsulate all interactions with the X (Twitter) API.
    Handles client authentication, rate limiting, and API calls.
    """

    def __init__(self):
        """
        Initializes the XService by loading credentials and creating API clients.
        """
        self.api_v1, self.client_v2 = self._create_tweepy_clients()

    def _countdown(self, seconds, message="Waiting..."):
        """Displays a single message and waits for a given duration."""
        if seconds > 0:
            logging.info(message)
            time.sleep(seconds)

    def _handle_rate_limit(self, e):
        """Handles rate limit errors by parsing the reset time and waiting."""
        try:
            reset_timestamp = int(e.response.headers.get("x-rate-limit-reset", 0))
        except (ValueError, TypeError):
            reset_timestamp = 0

        if reset_timestamp > 0:
            wait_seconds = max(0, reset_timestamp - int(time.time()))
            resume_time = time.strftime("%H:%M:%S", time.localtime(reset_timestamp))
            mins = round(wait_seconds / 60)
            countdown_message = f"Rate limit reached. Waiting for ~{mins} minutes. Resuming at {resume_time}."
            self._countdown(wait_seconds, countdown_message)
        else:
            self._countdown(
                15 * 60,
                "Rate limit reached, but reset time is unknown. Waiting for 15 minutes as a fallback.",
            )

    def _create_tweepy_clients(self):
        """Loads credentials and creates authenticated Tweepy clients."""
        logging.debug("Loading environment variables from .env file...")
        load_dotenv()
        logging.debug("Environment variables loaded.")

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
            client_v2 = tweepy.Client(
                consumer_key=api_key,
                consumer_secret=api_key_secret,
                access_token=access_token,
                access_token_secret=access_token_secret,
                wait_on_rate_limit=False,
            )
            auth_user = client_v2.get_me(user_fields=["id"])
            logging.debug(f"Authenticated as user ID: {auth_user.data.id}")

            auth = tweepy.OAuth1UserHandler(
                api_key, api_key_secret, access_token, access_token_secret
            )
            api_v1 = tweepy.API(auth, wait_on_rate_limit=False)

            logging.info("Authentication successful for both API v1.1 and v2.")
            return api_v1, client_v2
        except Exception as e:
            logging.error(f"Error during API authentication: {e}", exc_info=True)
            sys.exit(1)

    def get_blocked_user_ids(self):
        """Fetches the complete list of blocked user IDs from the API."""
        logging.info("Fetching blocked account IDs... (This will be fast)")
        blocked_user_ids = set()
        cursor = -1
        while True:
            try:
                logging.debug(f"Fetching blocked IDs with cursor: {cursor}")
                ids, (_, next_cursor) = self.api_v1.get_blocked_ids(cursor=cursor)
                blocked_user_ids.update(ids)
                logging.info(
                    f"Found {len(blocked_user_ids)} blocked account IDs...",
                    extra={"single_line": True},
                )
                cursor = next_cursor
                if cursor == 0:
                    break
            except tweepy.errors.TooManyRequests as e:
                self._handle_rate_limit(e)
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

    def unblock_user(self, user_id):
        """
        Unblocks a single user by their ID.

        Args:
            user_id (int): The ID of the user to unblock.

        Returns:
            tweepy.User: The user object of the unblocked user, or None on failure.
        """
        while True:
            try:
                logging.debug(f"Attempting to unblock user ID: {user_id}...")
                return self.api_v1.destroy_block(user_id=user_id)
            except tweepy.errors.TooManyRequests as e:
                self._handle_rate_limit(e)
                # Retry after waiting
                continue
            except tweepy.errors.NotFound:
                logging.warning(
                    f"User ID {user_id} not found. The account may have been deleted. Skipping."
                )
                return "NOT_FOUND"
            except Exception as e:
                logging.error(
                    f"Could not unblock user ID {user_id}. Reason: {e}", exc_info=True
                )
                return None
