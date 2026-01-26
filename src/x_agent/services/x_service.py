import sys
import asyncio
import logging
from datetime import datetime, timezone
import tweepy.asynchronous
import tweepy
from tenacity import (
    retry,
    stop_after_attempt,
    wait_exponential,
    retry_if_exception,
)
from ..config import settings


def is_transient_error(exception):
    """
    Returns True if the exception is likely a transient network/server error.
    """
    # Catching 'NoneType' object has no attribute 'items' which is a common side-effect
    # of a failed retry/sleep cycle in tweepy's async client.
    if isinstance(
        exception, AttributeError
    ) and "'NoneType' object has no attribute 'items'" in str(exception):
        return True

    if isinstance(exception, tweepy.errors.TweepyException):
        # Retry on 5xx errors or connection issues
        if isinstance(exception, tweepy.errors.HTTPException):
            # v1.1 / Sync client uses status_code
            # v2 / Async client uses status
            status = getattr(exception.response, "status_code", None) or getattr(
                exception.response, "status", None
            )
            if status and status >= 500:
                return True
        # Check for connection errors (often wrapped)
        return "Connection" in str(exception) or "Timeout" in str(exception)
    return False


class XService:
    """
    A service class to encapsulate all interactions with the X (Twitter) API.
    Uses Tweepy AsyncClient (API v2) and v1.1 API for legacy actions.
    """

    def __init__(self) -> None:
        """Initializes the XService with both async and sync clients."""
        self._init_v2_client()
        auth = tweepy.OAuth1UserHandler(
            settings.x_api_key,
            settings.x_api_key_secret,
            settings.x_access_token,
            settings.x_access_token_secret,
        )
        self.api_v1 = tweepy.API(auth, wait_on_rate_limit=True)
        self.user_id: int | None = None
        self.pinned_tweet_id: int | None = None
        self.v1_lock = asyncio.Lock()

    def _init_v2_client(self) -> None:
        """Initializes or re-initializes the v2 AsyncClient."""
        self.client = tweepy.asynchronous.AsyncClient(
            consumer_key=settings.x_api_key,
            consumer_secret=settings.x_api_key_secret,
            access_token=settings.x_access_token,
            access_token_secret=settings.x_access_token_secret,
            wait_on_rate_limit=False,  # We handle rate limits manually to avoid corruption
        )

    async def _recreate_v2_client(self) -> None:
        """Closes the current v2 session and creates a new one."""
        logging.info("Re-creating X API v2 session to recover from corruption...")
        await self.close()
        self._init_v2_client()

    async def initialize(self) -> None:
        """Authenticates and retrieves the current user's ID."""
        try:
            logging.debug("Authenticating...")
            me = await self.client.get_me(user_fields=["pinned_tweet_id"])
            if not me.data:
                raise Exception("Authentication successful but no user data returned.")
            self.user_id = me.data.id
            self.pinned_tweet_id = me.data.pinned_tweet_id
            logging.info(f"Authenticated as {me.data.username} (ID: {self.user_id})")
        except Exception as e:
            logging.error(f"Authentication failed: {e}", exc_info=True)
            sys.exit(1)

    async def ensure_initialized(self) -> None:
        """Ensures the service is authenticated and user_id is set."""
        if self.user_id is None:
            await self.initialize()

    async def close(self) -> None:
        """Closes the underlying async client session."""
        if self.client and hasattr(self.client, "session") and self.client.session:
            await self.client.session.close()
            logging.debug("XService session closed.")

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=4, max=10),
        retry=retry_if_exception(is_transient_error),
        reraise=True,
    )
    async def get_blocked_user_ids(self) -> set[int]:
        """
        Fetches the complete list of blocked user IDs using v1.1 API.
        """
        logging.info("Fetching blocked account IDs via v1.1 API...")
        blocked_user_ids = set()
        cursor = -1

        try:
            while cursor != 0:
                async with self.v1_lock:
                    # We wrap the sync call in a retry block via the outer decorator
                    # But since it's a generator-like process, retrying the whole thing might be expensive.
                    # Ideally we retry individual pages. But for now, simple retry is safer than complex logic.
                    ids, (_, next_cursor) = await asyncio.to_thread(
                        self.api_v1.get_blocked_ids, cursor=cursor
                    )
                blocked_user_ids.update(ids)
                cursor = next_cursor
                if len(blocked_user_ids) % 1000 == 0 or cursor == 0:
                    logging.info(
                        f"Fetched {len(blocked_user_ids)} IDs...",
                        extra={"single_line": True},
                    )
        except Exception as e:
            # If it's transient, the decorator handles it. If not, we log and re-raise.
            if not is_transient_error(e):
                logging.error(f"Error fetching blocked IDs: {e}", exc_info=True)
            raise

        logging.info(
            f"Finished fetching. Found a total of {len(blocked_user_ids)} blocked account IDs."
        )
        return blocked_user_ids

    async def _check_user_exists_v1(self, user_id: int) -> bool | None:
        """Checks if a user exists and is active using v1.1 API."""
        try:
            async with self.v1_lock:
                await asyncio.to_thread(self.api_v1.get_user, user_id=user_id)
            return True
        except tweepy.errors.NotFound:
            return False
        except tweepy.errors.Forbidden as e:
            logging.warning(
                f"User {user_id} is suspended (Forbidden). Treating as Ghost Block. API says: {e}"
            )
            return False
        except Exception as e:
            logging.warning(f"Error checking existence of user {user_id}: {e}")
            return None

    async def _handle_zombie_recovery(self, user_id: int) -> str:
        """
        Attempts to fix a 'Zombie Block'.
        """
        logging.warning(
            f"User ID {user_id} EXISTS. Attempting recovery strategies (Zombie Fix)..."
        )

        # Strategy 1: V2 Unblock
        try:
            await self.ensure_initialized()
            await self.client.request(
                "DELETE",
                f"/2/users/{self.user_id}/blocking/{user_id}",
                params={},
                user_auth=True,
            )
            return "SUCCESS"
        except tweepy.errors.NotFound as e:
            logging.warning(
                f"V2 Unblock ALSO returned 404 for {user_id}. API says: {e}"
            )
        except Exception as e:
            if "unexpected mimetype: text/html" in str(e):
                logging.warning(
                    f"V2 Unblock failed for {user_id}: API returned HTML (likely 404/500) instead of JSON."
                )
            else:
                logging.warning(f"V2 Unblock failed for {user_id}: {e}")

        # Strategy 2: Toggle Block Fix
        try:
            async with self.v1_lock:
                await asyncio.to_thread(self.api_v1.create_block, user_id=user_id)
                await asyncio.to_thread(self.api_v1.destroy_block, user_id=user_id)
            return "SUCCESS"
        except Exception as e:
            logging.warning(f"Toggle Block Fix failed for {user_id}: {e}")

        logging.warning(
            f"All recovery strategies failed for {user_id} (True Zombie Block). Skipping to avoid infinite retries."
        )
        return "NOT_FOUND"

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=2, max=5),
        retry=retry_if_exception(is_transient_error),
        reraise=True,
    )
    async def unblock_user(self, user_id: int) -> str:
        """
        Unblocks a user using v1.1 API with Zombie Block recovery.
        Returns "SUCCESS", "NOT_FOUND", or "FAILED".
        """
        try:
            async with self.v1_lock:
                await asyncio.to_thread(self.api_v1.destroy_block, user_id=user_id)
            return "SUCCESS"
        except tweepy.errors.NotFound as e:
            # Not found is not transient, so we handle it immediately
            logging.warning(
                f"User ID {user_id} not found (404) on V1. API says: {e}. Checking if user exists..."
            )
            exists = await self._check_user_exists_v1(user_id)
            if exists is True:
                return await self._handle_zombie_recovery(user_id)
            elif exists is False:
                logging.warning(
                    f"User ID {user_id} confirmed missing. Skipping (Ghost Block)."
                )
                return "NOT_FOUND"
            else:  # exists is None
                logging.warning(
                    f"Could not verify existence of {user_id}. Returning FAILED to retry later."
                )
                return "FAILED"
        except Exception as e:
            if is_transient_error(e):
                raise  # Reraise to let tenacity handle it
            logging.warning(f"Failed to unblock {user_id}: {e}")
            return "FAILED"

    async def get_me(self) -> tweepy.Response:
        """Retrieves the authenticated user's information."""
        return await self.client.get_me(
            user_fields=["public_metrics", "created_at", "description"]
        )

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=4, max=10),
        retry=retry_if_exception(is_transient_error),
        reraise=True,
    )
    async def get_following_user_ids(self) -> set[int]:
        """
        Fetches the complete list of user IDs that the authenticated user follows.
        """
        logging.info("Fetching following account IDs via v1.1 API...")
        following_ids = set()
        cursor = -1

        try:
            while cursor != 0:
                async with self.v1_lock:
                    ids, (_, next_cursor) = await asyncio.to_thread(
                        self.api_v1.get_friend_ids, cursor=cursor
                    )
                following_ids.update(ids)
                cursor = next_cursor
                if len(following_ids) % 1000 == 0 or cursor == 0:
                    logging.info(
                        f"Fetched {len(following_ids)} IDs...",
                        extra={"single_line": True},
                    )
        except Exception as e:
            if not is_transient_error(e):
                logging.error(f"Error fetching following IDs: {e}", exc_info=True)
            raise

        logging.info(
            f"Finished fetching. Found a total of {len(following_ids)} following account IDs."
        )
        return following_ids

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=4, max=10),
        retry=retry_if_exception(is_transient_error),
        reraise=True,
    )
    async def get_follower_user_ids(self) -> set[int]:
        """
        Fetches the complete list of user IDs that follow the authenticated user.
        """
        logging.info("Fetching follower account IDs via v1.1 API...")
        follower_ids = set()
        cursor = -1

        try:
            while cursor != 0:
                async with self.v1_lock:
                    ids, (_, next_cursor) = await asyncio.to_thread(
                        self.api_v1.get_follower_ids, cursor=cursor
                    )
                follower_ids.update(ids)
                cursor = next_cursor
                if len(follower_ids) % 1000 == 0 or cursor == 0:
                    logging.info(
                        f"Fetched {len(follower_ids)} IDs...",
                        extra={"single_line": True},
                    )
        except Exception as e:
            if not is_transient_error(e):
                logging.error(f"Error fetching follower IDs: {e}", exc_info=True)
            raise

        logging.info(
            f"Finished fetching. Found a total of {len(follower_ids)} follower account IDs."
        )
        return follower_ids

    async def _handle_v2_rate_limit(
        self, exception: tweepy.errors.TooManyRequests
    ) -> None:
        """Handles v2 rate limits by sleeping until the reset time or Retry-After."""
        headers = exception.response.headers
        logging.warning(f"RATE LIMIT HEADERS: {dict(headers)}")

        reset_at = headers.get("x-rate-limit-reset")
        retry_after = headers.get("retry-after")

        if reset_at:
            reset_time = datetime.fromtimestamp(int(reset_at), tz=timezone.utc)
            # Add a small buffer of 5 seconds
            wait_seconds = (reset_time - datetime.now(timezone.utc)).total_seconds() + 5
            if wait_seconds > 0:
                logging.warning(
                    f"Rate limit hit (v2). Resets at {reset_time.strftime('%Y-%m-%d %H:%M:%S UTC')}. "
                    f"Sleeping for {wait_seconds:.0f} seconds..."
                )
                await asyncio.sleep(wait_seconds)
            else:
                # If reset is in the past, X might be applying a daily cap
                logging.warning(
                    "Reset time is in the past. This often indicates a Daily Limit (50/day on Free tier)."
                )
                logging.warning("Sleeping for 30 minutes as backoff...")
                await asyncio.sleep(1801)
        elif retry_after:
            wait_seconds = int(retry_after) + 5
            logging.warning(
                f"Rate limit hit (v2). Retry-After: {retry_after}s. Sleeping for {wait_seconds}s..."
            )
            await asyncio.sleep(wait_seconds)
        else:
            logging.warning(
                "Rate limit hit (v2). No reset header. Sleeping for 15 minutes..."
            )
            await asyncio.sleep(901)

        await self._recreate_v2_client()

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=2, max=10),
        retry=retry_if_exception(is_transient_error),
        reraise=True,
    )
    async def get_users_by_ids(self, user_ids: list[int]) -> list[tweepy.User]:
        """
        Resolves a list of user IDs to user objects using the v2 API.
        X API v2 allows up to 100 IDs per request.
        """
        if not user_ids:
            return []

        all_users = []
        # Chunk IDs into groups of 100
        for i in range(0, len(user_ids), 100):
            chunk = user_ids[i : i + 100]
            try:
                response = await self.client.get_users(ids=chunk)
                if response.data:
                    all_users.extend(response.data)
            except tweepy.errors.TooManyRequests as e:
                await self._handle_v2_rate_limit(e)
                # Retry this specific chunk
                remaining_users = await self.get_users_by_ids(user_ids[i:])
                all_users.extend(remaining_users)
                break
            except Exception as e:
                if is_transient_error(e):
                    if "NoneType" in str(e):
                        await self._recreate_v2_client()
                        remaining_users = await self.get_users_by_ids(user_ids[i:])
                        all_users.extend(remaining_users)
                        break
                    raise
                logging.warning(f"Error fetching users for chunk starting at {i}: {e}")
        return all_users

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=2, max=5),
        retry=retry_if_exception(is_transient_error),
        reraise=True,
    )
    async def unfollow_user(self, user_id: int) -> str:
        """
        Unfollows a user using v1.1 API.
        Returns "SUCCESS" or "FAILED".
        """
        try:
            async with self.v1_lock:
                await asyncio.to_thread(self.api_v1.destroy_friendship, user_id=user_id)
            return "SUCCESS"
        except Exception as e:
            if is_transient_error(e):
                raise
            logging.warning(f"Failed to unfollow {user_id}: {e}")
            return "FAILED"

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=2, max=10),
        retry=retry_if_exception(is_transient_error),
        reraise=True,
    )
    async def get_user_tweets_v1(
        self, user_id: int, max_id: int | None = None
    ) -> list[tweepy.models.Status]:
        """
        Fetches a page of tweets using v1.1 API (Legacy/Free Tier).
        Uses max_id for pagination instead of tokens.
        """
        async with self.v1_lock:
            return await asyncio.to_thread(
                self.api_v1.user_timeline,
                user_id=user_id,
                count=200,
                max_id=max_id,
                include_rts=True,
                tweet_mode="extended",
            )

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=2, max=10),
        retry=retry_if_exception(is_transient_error),
        reraise=True,
    )
    async def get_user_tweets(
        self, user_id: int, pagination_token: str | None = None
    ) -> tweepy.Response:
        """
        Fetches a page of tweets for a given user ID using v2 API.
        Includes public metrics and creation date.
        """
        try:
            return await self.client.get_users_tweets(
                id=user_id,
                max_results=100,
                pagination_token=pagination_token,
                tweet_fields=["public_metrics", "created_at", "referenced_tweets"],
                exclude=["retweets"],
            )
        except tweepy.errors.TooManyRequests as e:
            await self._handle_v2_rate_limit(e)
            return await self.get_user_tweets(user_id, pagination_token)
        except Exception as e:
            if is_transient_error(e):
                if "NoneType" in str(e):
                    await self._recreate_v2_client()
                    return await self.get_user_tweets(user_id, pagination_token)
                raise
            raise

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=2, max=5),
        retry=retry_if_exception(is_transient_error),
        reraise=True,
    )
    async def delete_tweet(self, tweet_id: int) -> bool:
        """
        Deletes a tweet using v2 API.
        Returns True if successful, False otherwise.
        """
        try:
            response = await self.client.delete_tweet(id=tweet_id)
            if response.data:
                return response.data.get("deleted", False)
            return False
        except tweepy.errors.TooManyRequests as e:
            await self._handle_v2_rate_limit(e)
            # Recursion gives us a fresh set of tenacity attempts
            return await self.delete_tweet(tweet_id)
        except Exception as e:
            if is_transient_error(e):
                if "NoneType" in str(e):
                    await self._recreate_v2_client()
                    return await self.delete_tweet(tweet_id)
                raise
            logging.warning(f"Failed to delete tweet {tweet_id}: {e}")
            return False
