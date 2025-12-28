import sys
import asyncio
import logging
import tweepy.asynchronous
import tweepy
from ..config import settings


class XService:
    """
    A service class to encapsulate all interactions with the X (Twitter) API.
    Uses Tweepy AsyncClient (API v2) and v1.1 API for legacy actions.
    """

    def __init__(self) -> None:
        """Initializes the XService with both async and sync clients."""
        self.client = tweepy.asynchronous.AsyncClient(
            consumer_key=settings.x_api_key,
            consumer_secret=settings.x_api_key_secret,
            access_token=settings.x_access_token,
            access_token_secret=settings.x_access_token_secret,
            wait_on_rate_limit=True,
        )
        auth = tweepy.OAuth1UserHandler(
            settings.x_api_key,
            settings.x_api_key_secret,
            settings.x_access_token,
            settings.x_access_token_secret,
        )
        self.api_v1 = tweepy.API(auth, wait_on_rate_limit=True)
        self.user_id: int | None = None

    async def initialize(self) -> None:
        """Authenticates and retrieves the current user's ID."""
        try:
            logging.debug("Authenticating...")
            me = await self.client.get_me()
            if not me.data:
                raise Exception("Authentication successful but no user data returned.")
            self.user_id = me.data.id
            logging.info(f"Authenticated as {me.data.username} (ID: {self.user_id})")
        except Exception as e:
            logging.error(f"Authentication failed: {e}", exc_info=True)
            sys.exit(1)

    async def ensure_initialized(self) -> None:
        """Ensures the service is authenticated and user_id is set."""
        if self.user_id is None:
            await self.initialize()

    async def get_blocked_user_ids(self) -> set[int]:
        """
        Fetches the complete list of blocked user IDs using v1.1 API.
        """
        logging.info("Fetching blocked account IDs via v1.1 API...")
        blocked_user_ids = set()
        cursor = -1

        try:
            while cursor != 0:
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
            logging.error(f"Error fetching blocked IDs: {e}", exc_info=True)
            raise

        logging.info(
            f"Finished fetching. Found a total of {len(blocked_user_ids)} blocked account IDs."
        )
        return blocked_user_ids

    async def _check_user_exists_v1(self, user_id: int) -> bool | None:
        """Checks if a user exists and is active using v1.1 API.

        Returns:
            True: User exists.
            False: User does not exist (NotFound or Forbidden).
            None: Unable to verify (Unexpected error).
        """
        try:
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
        Attempts to fix a 'Zombie Block' (user exists but V1 unblock fails with 404).
        Strategies:
        1. V2 Unblock fallback.
        2. Toggle Block fix (block then unblock).
        """
        logging.warning(
            f"User ID {user_id} EXISTS. Attempting recovery strategies (Zombie Fix)..."
        )

        # Strategy 1: V2 Unblock
        try:
            # client.unblock corresponds to DELETE /2/users/:id/blocking/:target_user_id
            await self.ensure_initialized()
            await self.client.request(
                "DELETE",
                f"/2/users/{self.user_id}/blocking/{user_id}",
                user_auth=True,
            )
            return "SUCCESS"
        except tweepy.errors.NotFound as e:
            logging.warning(
                f"V2 Unblock ALSO returned 404 for {user_id}. API says: {e}"
            )
        except Exception as e:
            logging.warning(f"V2 Unblock failed for {user_id}: {e}")

        # Strategy 2: Toggle Block Fix
        try:
            await asyncio.to_thread(self.api_v1.create_block, user_id=user_id)
            await asyncio.to_thread(self.api_v1.destroy_block, user_id=user_id)
            return "SUCCESS"
        except Exception as e:
            logging.warning(f"Toggle Block Fix failed for {user_id}: {e}")

        logging.warning(
            f"All recovery strategies failed for {user_id}. Will retry in next session."
        )
        return "FAILED"

    async def unblock_user(self, user_id: int) -> str:
        """
        Unblocks a user using v1.1 API with Zombie Block recovery.
        Returns "SUCCESS", "NOT_FOUND", or "FAILED".
        """
        try:
            await asyncio.to_thread(self.api_v1.destroy_block, user_id=user_id)
            return "SUCCESS"
        except tweepy.errors.NotFound as e:
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
            logging.warning(f"Failed to unblock {user_id}: {e}")
            return "FAILED"

    async def get_me(self) -> tweepy.Response:
        """Retrieves the authenticated user's information."""
        return await self.client.get_me(user_fields=["public_metrics"])

    async def get_following_user_ids(self) -> set[int]:
        """
        Fetches the complete list of user IDs that the authenticated user follows.
        Uses v1.1 API.
        """
        logging.info("Fetching following account IDs via v1.1 API...")
        following_ids = set()
        cursor = -1

        try:
            while cursor != 0:
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
            logging.error(f"Error fetching following IDs: {e}", exc_info=True)
            raise

        logging.info(
            f"Finished fetching. Found a total of {len(following_ids)} following account IDs."
        )
        return following_ids

    async def get_follower_user_ids(self) -> set[int]:
        """
        Fetches the complete list of user IDs that follow the authenticated user.
        Uses v1.1 API.
        """
        logging.info("Fetching follower account IDs via v1.1 API...")
        follower_ids = set()
        cursor = -1

        try:
            while cursor != 0:
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
            logging.error(f"Error fetching follower IDs: {e}", exc_info=True)
            raise

        logging.info(
            f"Finished fetching. Found a total of {len(follower_ids)} follower account IDs."
        )
        return follower_ids

    async def unfollow_user(self, user_id: int) -> str:
        """
        Unfollows a user using v1.1 API.
        Returns "SUCCESS" or "FAILED".
        """
        try:
            await asyncio.to_thread(self.api_v1.destroy_friendship, user_id=user_id)
            return "SUCCESS"
        except Exception as e:
            logging.warning(f"Failed to unfollow {user_id}: {e}")
            return "FAILED"
