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
        # v1.1 API for actions not yet in v2 or AsyncClient
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
            self.user_id = me.data.id
            logging.info(f"Authenticated as {me.data.username} (ID: {self.user_id})")
        except Exception as e:
            logging.error(f"Authentication failed: {e}", exc_info=True)
            sys.exit(1)

    async def get_blocked_user_ids(self) -> set[int]:
        """
        Fetches the complete list of blocked user IDs using v1.1 API.
        """
        logging.info("Fetching blocked account IDs via v1.1 API...")
        blocked_user_ids = set()
        cursor = -1

        try:
            while cursor != 0:
                # Use to_thread for sync v1.1 call
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

    async def unblock_user(self, user_id: int) -> str | None:
        """
        Unblocks a user using v1.1 API.
        Returns "SUCCESS", "NOT_FOUND", "FAILED".
        """
        try:
            await asyncio.to_thread(self.api_v1.destroy_block, user_id=user_id)
            return "SUCCESS"
        except tweepy.errors.NotFound:
            return "NOT_FOUND"
        except Exception as e:
            logging.warning(f"Failed to unblock {user_id}: {e}")
            return "FAILED"

    async def get_me(self):
        return await self.client.get_me(user_fields=["public_metrics"])
