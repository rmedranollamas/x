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

    def __init__(self) -> None:
        """Initializes the XService, creating authenticated API clients."""
        self.api_v1, self.client_v2, self.authenticated_user_id = (
            self._create_tweepy_clients()
        )

    def _countdown(self, seconds: int, message: str = "Waiting...") -> None:
        """
        Displays a message and waits for a specified duration.

        Args:
            seconds (int): The number of seconds to wait.
            message (str): The message to display while waiting.
        """
        if seconds > 0:
            logging.info(message)
            time.sleep(seconds)

    def _handle_rate_limit(self, e: tweepy.errors.TooManyRequests) -> None:
        """
        Handles API rate limit errors by waiting for the reset time.

        Args:
            e (tweepy.errors.TooManyRequests): The rate limit exception.
        """
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
            return

        self._countdown(
            15 * 60,
            "Rate limit reached, but reset time is unknown. Waiting for 15 minutes as a fallback.",
        )

    def _get_credentials(self) -> tuple[str, str, str, str]:
        """
        Loads and validates API credentials from the .env file.

        Returns:
            A tuple containing the API key, secret, access token, and secret.
        """
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
        return api_key, api_key_secret, access_token, access_token_secret

    def _create_v2_client(
        self,
        api_key: str,
        api_key_secret: str,
        access_token: str,
        access_token_secret: str,
    ) -> tuple[tweepy.Client, int]:
        """
        Creates and authenticates the Tweepy v2 client.

        Returns:
            A tuple of (authenticated Tweepy v2 client, authenticated user ID).
        """
        try:
            client_v2 = tweepy.Client(
                consumer_key=api_key,
                consumer_secret=api_key_secret,
                access_token=access_token,
                access_token_secret=access_token_secret,
                wait_on_rate_limit=True,
            )
            auth_user = client_v2.get_me(user_fields=["id"])
            user_id = auth_user.data.id
            logging.debug(f"Authenticated as user ID: {user_id}")
            return client_v2, user_id
        except Exception as e:
            logging.error(f"Error during API v2 authentication: {e}", exc_info=True)
            sys.exit(1)

    def _create_v1_client(
        self,
        api_key: str,
        api_key_secret: str,
        access_token: str,
        access_token_secret: str,
    ) -> tweepy.API:
        """
        Creates and authenticates the Tweepy v1.1 client.

        Returns:
            An authenticated Tweepy v1.1 API object.
        """
        try:
            auth = tweepy.OAuth1UserHandler(
                api_key, api_key_secret, access_token, access_token_secret
            )
            api_v1 = tweepy.API(auth, wait_on_rate_limit=True)
            return api_v1
        except Exception as e:
            logging.error(f"Error during API v1.1 authentication: {e}", exc_info=True)
            sys.exit(1)

    def _create_tweepy_clients(self) -> tuple[tweepy.API, tweepy.Client, int]:
        """
        Loads credentials and creates authenticated Tweepy clients.

        Returns:
            A tuple containing the v1.1 API object, the v2 client, and the user ID.
        """
        api_key, api_key_secret, access_token, access_token_secret = (
            self._get_credentials()
        )

        logging.info("Authenticating with the X API...")
        client_v2, user_id = self._create_v2_client(
            api_key, api_key_secret, access_token, access_token_secret
        )
        api_v1 = self._create_v1_client(
            api_key, api_key_secret, access_token, access_token_secret
        )

        logging.info("Authentication successful for both API v1.1 and v2.")
        return api_v1, client_v2, user_id

    def _check_user_exists_v1(self, user_id: int) -> bool:
        """
        Verifies if a user actually exists using V1.1 API.
        Used to distinguish between 'Ghost Blocks' (User deleted) and 'Zombie Blocks' (User active but V1 unblock fails).
        """
        try:
            self.api_v1.get_user(user_id=user_id)
            return True
        except tweepy.errors.NotFound:
            return False
        except tweepy.errors.Forbidden as e:
            # User suspended (403). Cannot be unblocked usually. Treat as Ghost to skip.
            logging.warning(f"User {user_id} is suspended (Forbidden). Treating as Ghost Block. API says: {e}")
            return False
        except Exception as e:
            logging.warning(f"Error checking existence of user {user_id}: {e}")
            # Assume exists to be safe and retry? Or assume not?
            # If we can't verify, we shouldn't mark as Ghost (forever skip).
            return True

    def get_blocked_user_ids(self) -> list[int]:
        """
        Fetches the complete list of blocked user IDs from the API using V1.1.
        We switched to V1.1 because V2 was returning 'Ghost' IDs that return 404 on unblock.
        Matching the List source (V1.1) with the Unblock source (V1.1) reduces inconsistency.

        Returns:
            A list of integer user IDs.
        """
        logging.info(
            "Fetching blocked account IDs via V1.1... (This aligns with V1.1 unblock)"
        )
        blocked_user_ids = []

        try:
            # Use Tweepy's Cursor to handle V1.1 pagination automatically.
            # self.api_v1 is configured with wait_on_rate_limit=True
            for page in tweepy.Cursor(self.api_v1.get_blocked_ids).pages():
                # page is a list of integer IDs in V1.1 get_blocked_ids
                if page:
                    logging.debug(f"Raw IDs received from API V1.1: {page}")
                    blocked_user_ids.extend(page)
                    logging.info(
                        f"Found {len(blocked_user_ids)} blocked account IDs...",
                        extra={"single_line": True},
                    )
        except Exception as e:
            logging.error(
                f"An unexpected error occurred while fetching: {e}", exc_info=True
            )
            sys.exit(1)

        logging.info(
            f"Finished fetching. Found a total of {len(blocked_user_ids)} blocked account IDs."
        )
        return blocked_user_ids

    def _unblock_user_v2(self, target_user_id: int) -> bool:
        """
        Attempts to unblock a user using the V2 API via a raw request.
        Raises exceptions if the request fails, allowing the caller to handle specific error codes.
        """
        url = f"/2/users/{self.authenticated_user_id}/blocking/{target_user_id}"
        response = self.client_v2.request(method="DELETE", route=url)
        logging.debug(f"V2 Unblock response: {response}")
        
        # If we are here, it likely succeeded (Tweepy raises on error).
        logging.info(f"V2 Unblock raw request successful for {target_user_id}.")
        return True

    def unblock_user(self, user_id: int) -> bool | str | None:
        """
        Unblocks a single user by their ID using the V1.1 API (via Tweepy).
        Handles 'Zombie Blocks' (Active users that return 404 on V1 unblock) by falling back to V2.

        Args:
            user_id: The integer ID of the user to unblock.

        Returns:
            True if successful, 'NOT_FOUND' if the user does not exist/not blocked (Ghost),
            or None if an error occurs (e.g. Rate Limit, Network).
        """
        try:
            logging.debug(f"Attempting to unblock user ID: {user_id}...")
            self.api_v1.destroy_block(user_id=user_id)
            return True

        except tweepy.errors.NotFound as e:
            # V1 says 404. Could be Ghost (Deleted) or Zombie (Active but V1 glitch).
            logging.warning(
                f"User ID {user_id} not found (404) on V1. API says: {e}. Checking if user exists (Zombie check)..."
            )
            
            if self._check_user_exists_v1(user_id):
                logging.info(f"User ID {user_id} EXISTS. Attempting V2 Unblock (Zombie Fix)...")
                try:
                    self._unblock_user_v2(user_id)
                    return True
                except tweepy.errors.NotFound as v2_e:
                    logging.warning(f"V2 Unblock ALSO returned 404 for {user_id}. Unblock impossible. Skipping. API says: {v2_e}")
                    return "NOT_FOUND" # Mark as handled to stop loop
                except Exception as v2_e:
                     logging.error(f"V2 Unblock failed for {user_id}: {v2_e}")
                     return None # Retry later
            else:
                 logging.warning(f"User ID {user_id} confirmed missing. Skipping (Ghost Block).")
                 return "NOT_FOUND"

        except tweepy.errors.Forbidden as e:
            # Forbidden (403) - Suspended or otherwise inaccessible
            logging.warning(
                f"Forbidden (403) for User ID {user_id}. API says: {e}. Skipping."
            )
            return "NOT_FOUND"

        except tweepy.errors.TooManyRequests as e:
            # Rate Limit (429)
            self._handle_rate_limit(e)
            return None

        except Exception as e:
            logging.error(
                f"Could not unblock user ID {user_id}. Exception: {e}", exc_info=True
            )
            return None

    def get_me(self) -> tweepy.User | None:
        """
        Retrieves the authenticated user's public metrics.

        Returns:
            The user object with public metrics, or None if an error occurs.
        """
        try:
            logging.debug("Fetching authenticated user's metrics...")
            auth_user = self.client_v2.get_me(user_fields=["public_metrics"])
            return auth_user.data
        except Exception as e:
            logging.error(f"Error fetching user metrics: {e}", exc_info=True)
            return None
