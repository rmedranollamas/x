import logging
import asyncio
import tweepy
from datetime import datetime, timezone, timedelta
from typing import TYPE_CHECKING, List
from .base_agent import BaseAgent
from ..services.x_service import XService

if TYPE_CHECKING:
    from ..database import DatabaseManager


class DeleteAgent(BaseAgent):
    """
    An agent responsible for deleting old tweets based on specific rules.
    """

    def __init__(
        self,
        x_service: XService,
        db_manager: "DatabaseManager",
        dry_run: bool = False,
        protected_ids: List[int] | None = None,
        **kwargs,
    ) -> None:
        """
        Initializes the agent.

        Args:
            x_service: An instance of XService.
            db_manager: Database Manager.
            dry_run: If True, simulate actions.
            protected_ids: Optional. List of tweet IDs to never delete.
        """
        super().__init__(db_manager)
        self.x_service = x_service
        self.dry_run = dry_run
        self.protected_ids = set(protected_ids or [])
        self.stats = {"deleted": 0, "skipped": 0, "errors": 0}

    async def execute(self) -> str:
        """
        Executes the deletion logic across all pages of tweets using v1.1.
        """
        logging.info("--- X Delete Agent (V1.1 Hybrid) ---")
        await self.x_service.ensure_initialized()
        await asyncio.to_thread(self.db.initialize_database)

        if self.dry_run:
            logging.info("DRY RUN ENABLED: No tweets will be actually deleted.")

        # Add pinned tweet to protected IDs
        if self.x_service.pinned_tweet_id:
            self.protected_ids.add(self.x_service.pinned_tweet_id)
            logging.info(f"Protected pinned tweet: {self.x_service.pinned_tweet_id}")

        max_id = None
        now = datetime.now(timezone.utc)

        while True:
            try:
                # V1.1 returns a list of Status objects
                tweets = await self.x_service.get_user_tweets_v1(
                    self.x_service.user_id, max_id=max_id
                )
            except tweepy.errors.Unauthorized as e:
                logging.warning(
                    f"Reached API access limit for fetching tweets: {e}. "
                    "Processing what was already fetched."
                )
                break
            except Exception as e:
                logging.error(f"Error fetching tweets: {e}")
                break

            if not tweets:
                break

            for tweet in tweets:
                # max_id pagination requires us to skip the first tweet of subsequent pages
                if max_id and tweet.id == max_id:
                    continue

                await self._process_tweet(tweet, now)
                max_id = tweet.id

            # Small delay between pages
            await asyncio.sleep(1)

        report = self._generate_report()
        logging.info(report)
        return report

    async def _process_tweet(self, tweet, now: datetime):
        """Applies the rules to a single tweet (Status object) and deletes if necessary."""
        tweet_id = tweet.id
        # v1.1 uses created_at as a datetime object already (usually)
        created_at = tweet.created_at
        if created_at.tzinfo is None:
            created_at = created_at.replace(tzinfo=timezone.utc)

        likes = tweet.favorite_count
        retweets = tweet.retweet_count

        # Check if it's a response (reply)
        is_response = tweet.in_reply_to_status_id is not None

        age = now - created_at

        # Rule 1: Protected
        if tweet_id in self.protected_ids:
            logging.info(f"KEEP [Protected] ID: {tweet_id}")
            self.stats["skipped"] += 1
            return

        # Rule 2: Older than 1 year - Delete regardless
        if age > timedelta(days=365):
            reason = "older than 1 year"
            await self._delete_tweet(tweet, likes + retweets, is_response, reason)
            return

        # Rule 3: Less than 7 days - Keep
        if age < timedelta(days=7):
            logging.info(f"KEEP [Recent]    ID: {tweet_id} (Age: {age.days}d)")
            self.stats["skipped"] += 1
            return

        # Rule 4: Engagement thresholds (7 days to 1 year)
        engagement_score = likes + retweets

        if not is_response and engagement_score >= 50:
            logging.info(
                f"KEEP [Popular]   ID: {tweet_id} ({engagement_score} likes+rt)"
            )
            self.stats["skipped"] += 1
            return

        if is_response and engagement_score >= 5:
            logging.info(
                f"KEEP [Pop-Reply] ID: {tweet_id} ({engagement_score} likes+rt)"
            )
            self.stats["skipped"] += 1
            return

        # Rule 5: Otherwise, delete
        reason = (
            f"low engagement ({engagement_score} likes+rt, is_response={is_response})"
        )
        await self._delete_tweet(tweet, engagement_score, is_response, reason)

    async def _delete_tweet(self, tweet, engagement, is_response, reason):
        """Helper to delete and log."""
        tweet_id = tweet.id
        text = getattr(tweet, "full_text", getattr(tweet, "text", "No text"))
        # Clean up text for logging
        display_text = text.replace("\n", " ")[:60]

        if self.dry_run:
            logging.info(
                f"DELETE           ID: {tweet_id} | Eng: {engagement} | {display_text}..."
            )
            self.stats["deleted"] += 1
        else:
            logging.info(f"Deleting tweet {tweet_id}: {reason}")
            success = await self.x_service.delete_tweet(tweet_id)
            if success:
                self.stats["deleted"] += 1
                await asyncio.to_thread(
                    self.db.log_deleted_tweet,
                    tweet_id,
                    text,
                    tweet.created_at.isoformat(),
                    engagement,  # We store engagement score in the 'views' column for simplicity
                    is_response,
                )
            else:
                self.stats["errors"] += 1

    def _generate_report(self) -> str:
        """Generates a summary of the deletion session."""
        lines = [
            "\n--- Delete Agent Report ---",
            f"Tweets Processed: {sum(self.stats.values())}",
            f"Tweets Deleted:   {self.stats['deleted']}",
            f"Tweets Skipped:   {self.stats['skipped']}",
            f"Errors:           {self.stats['errors']}",
            "---------------------------\n",
        ]
        return "\n".join(lines)
