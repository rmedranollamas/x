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
        self.api_v1, self.client_v2 = self._create_tweepy_clients()

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
    ) -> tweepy.Client:
        """
        Creates and authenticates the Tweepy v2 client.

        Returns:
            An authenticated Tweepy v2 client.
        """
        try:
            client_v2 = tweepy.Client(
                consumer_key=api_key,
                consumer_secret=api_key_secret,
                access_token=access_token,
                access_token_secret=access_token_secret,
                wait_on_rate_limit=False,
            )
            auth_user = client_v2.get_me(user_fields=["id"])
            logging.debug(f"Authenticated as user ID: {auth_user.data.id}")
            return client_v2
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
            api_v1 = tweepy.API(auth, wait_on_rate_limit=False)
            return api_v1
        except Exception as e:
            logging.error(f"Error during API v1.1 authentication: {e}", exc_info=True)
            sys.exit(1)

    def _create_tweepy_clients(self) -> tuple[tweepy.API, tweepy.Client]:
        """
        Loads credentials and creates authenticated Tweepy clients.

        Returns:
            A tuple containing the v1.1 API object and the v2 client.
        """
        api_key, api_key_secret, access_token, access_token_secret = (
            self._get_credentials()
        )

        logging.info("Authenticating with the X API...")
        client_v2 = self._create_v2_client(
            api_key, api_key_secret, access_token, access_token_secret
        )
        api_v1 = self._create_v1_client(
            api_key, api_key_secret, access_token, access_token_secret
        )

        logging.info("Authentication successful for both API v1.1 and v2.")
        return api_v1, client_v2

    def get_blocked_user_ids(self) -> list[int]:
        """
        Fetches the complete list of blocked user IDs from the API.

        Returns:
            A list of integer user IDs, ordered by most recently blocked.
        """
        logging.info("Fetching blocked account IDs... (This will be fast)")
        blocked_user_ids = []
        cursor = -1
        while True:
            try:
                logging.debug(f"Fetching blocked IDs with cursor: {cursor}")
                # The response is a tuple: (list_of_ids, (previous_cursor, next_cursor))
                raw_response = self.api_v1.get_blocked_ids(cursor=cursor)
                ids = raw_response[0]
                next_cursor = raw_response[1][1]

                logging.debug(f"Raw IDs received from API: {ids}")
                blocked_user_ids.extend(ids)
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

    def unblock_user(self, user_id: int) -> tweepy.User | str | None:
        """
        Unblocks a single user by their ID.

        Args:
            user_id: The integer ID of the user to unblock.

        Returns:
            The user object if successful, 'NOT_FOUND' if the user does not exist,
            or None if an error occurs.
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
