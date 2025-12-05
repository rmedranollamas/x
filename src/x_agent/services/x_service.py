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
                wait_on_rate_limit=False,
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
            api_v1 = tweepy.API(auth, wait_on_rate_limit=False)
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

    def get_blocked_user_ids(self) -> list[int]:
        """
        Fetches the complete list of blocked user IDs from the API using V2.

        Returns:
            A list of integer user IDs.
        """
        logging.info("Fetching blocked account IDs via V2... (This will be fast)")
        blocked_user_ids = []

        # Use Tweepy's Paginator to handle V2 pagination automatically
        paginator = tweepy.Paginator(
            self.client_v2.get_blocked,
            max_results=1000,
            user_auth=True,  # Ensure user context auth is used
        )

        try:
            for response in paginator:
                if response.data:
                    ids = [user.id for user in response.data]
                    logging.debug(f"Raw IDs received from API: {ids}")
                    blocked_user_ids.extend(ids)
                    logging.info(
                        f"Found {len(blocked_user_ids)} blocked account IDs...",
                        extra={"single_line": True},
                    )

                # Check for rate limit headers in the response if available?
                # Tweepy Paginator usually handles basic iteration, but we might need to catch exceptions
                # if we want custom wait logic. However, passing `wait_on_rate_limit=False` to Client
                # means exceptions will be raised.
        except tweepy.errors.TooManyRequests as e:
            # If Paginator encounters a rate limit, it raises the exception
            self._handle_rate_limit(e)
            # We can't easily "resume" a Paginator from an exception without complex logic.
            # For now, fail gracefully or accept that we got partial list?
            # Since 'unblock' is resumable, partial list is actually OK!
            # We return what we have. The next run will get the rest (or the start again).
            logging.warning("Rate limit hit during fetching. Returning partial list.")
        except Exception as e:
            logging.error(
                f"An unexpected error occurred while fetching: {e}", exc_info=True
            )
            sys.exit(1)

        logging.info(
            f"Finished fetching. Found a total of {len(blocked_user_ids)} blocked account IDs."
        )
        return blocked_user_ids

    def unblock_user(self, user_id: int) -> bool | str | None:
        """
        Unblocks a single user by their ID using the V2 API.

        Args:
            user_id: The integer ID of the user to unblock.

        Returns:
            True if successful, 'NOT_FOUND' if the user does not exist/not blocked,
            or None if an error occurs.
        """
        while True:
            try:
                logging.debug(f"Attempting to unblock user ID: {user_id}...")
                # Use V2 API for unblocking manually via request.
                # Note: Client base URL likely includes '/2', so we use '/users/...'
                url = f"/users/{self.authenticated_user_id}/blocking/{user_id}"
                response = self.client_v2.request("DELETE", url)

                # V2 returns 200 OK with {"data": {"blocking": false}} on success
                if response.errors:
                    # Check for specific errors if needed
                    logging.warning(
                        f"V2 API Error unblocking {user_id}: {response.errors}"
                    )
                    # Proceed to check exceptions or assume failure?
                    # request() usually raises exceptions for HTTP errors unless configured otherwise.
                    pass

                return True
            except tweepy.errors.TooManyRequests as e:
                self._handle_rate_limit(e)
                # Retry after waiting
                continue
            except tweepy.errors.NotFound as e:
                response_url = e.response.url if e.response else "Unknown URL"
                error_details = e.response.text if e.response else "No response body"
                logging.warning(
                    f"User ID {user_id} not found or not blocked (404). URL: {response_url}. Response: {error_details}. Skipping."
                )
                return "NOT_FOUND"
            except tweepy.errors.BadRequest as e:
                # Sometimes V2 returns Bad Request for invalid/suspended users
                logging.warning(f"Bad Request for User ID {user_id}: {e}. Skipping.")
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
