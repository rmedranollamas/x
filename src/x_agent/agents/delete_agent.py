import logging
import asyncio
import tweepy
import json
from pathlib import Path
from datetime import datetime, timezone, timedelta
from typing import TYPE_CHECKING, List
from .base_agent import BaseAgent
from ..services.x_service import XService

if TYPE_CHECKING:
    from ..database import DatabaseManager


class DeleteAgent(BaseAgent):
    """
    An agent responsible for deleting old tweets based on specific rules.
    Supports both live API fetching and X data archive files.
    """

    # Deletion Thresholds
    CRITICAL_AGE_DAYS = 365
    GRACE_PERIOD_DAYS = 7
    RETWEET_MAX_AGE_DAYS = 30
    POPULAR_TWEET_THRESHOLD = 20
    POPULAR_REPLY_THRESHOLD = 5
    LINK_TWEET_THRESHOLD = 10
    LINK_REPLY_THRESHOLD = 3

    def __init__(
        self,
        x_service: XService,
        db_manager: "DatabaseManager",
        dry_run: bool = False,
        protected_ids: List[int] | None = None,
        archive_path: str | None = None,
        **kwargs,
    ) -> None:
        """
        Initializes the agent.

        Args:
            x_service: An instance of XService.
            db_manager: Database Manager.
            dry_run: If True, simulate actions.
            protected_ids: Optional. List of tweet IDs to never delete.
            archive_path: Optional. Path to the tweets.js file from X archive.
        """
        super().__init__(db_manager)
        self.x_service = x_service
        self.dry_run = dry_run
        self.protected_ids = set(protected_ids or [])
        self.archive_path = Path(archive_path) if archive_path else None
        self.stats = {"deleted": 0, "skipped": 0, "errors": 0}

    async def execute(self) -> str:
        """
        Executes the deletion logic using archive file or live API.
        """
        logging.info("--- X Delete Agent ---")
        await self.x_service.ensure_initialized()
        await asyncio.to_thread(self.db.initialize_database)

        if self.dry_run:
            logging.info("DRY RUN ENABLED: No tweets will be actually deleted.")

        # Add pinned tweet to protected IDs
        if self.x_service.pinned_tweet_id:
            self.protected_ids.add(self.x_service.pinned_tweet_id)
            logging.info(f"Protected pinned tweet: {self.x_service.pinned_tweet_id}")

        now = datetime.now(timezone.utc)

        # Priority 1: Archive Processing
        if self.archive_path:
            await self._process_archive(now)
        else:
            # Priority 2: Live API Fetching (Hybrid V1.1)
            await self._process_live_api(now)

        report = self._generate_report()
        logging.info(report)
        return report

    async def _process_archive(self, now: datetime):
        """Parses and processes tweets from an X archive file."""
        if not self.archive_path.exists():
            logging.error(f"Archive file not found: {self.archive_path}")
            return

        logging.info(f"Processing archive: {self.archive_path}")
        try:
            content = await asyncio.to_thread(
                self.archive_path.read_text, encoding="utf-8"
            )
            # Archive files (tweets.js) are JS files, we need the JSON part
            # window.YTD.tweets.part0 = [ ... ]
            json_str = content[content.find("[") :]
            tweets_data = json.loads(json_str)

            logging.info(f"Found {len(tweets_data)} tweets in archive.")

            for entry in tweets_data:
                tweet_raw = entry.get("tweet", {})
                # Create a pseudo-tweet object compatible with _process_tweet
                # X Archive date format: "Wed Oct 24 10:00:00 +0000 2018"
                created_at = datetime.strptime(
                    tweet_raw["created_at"], "%a %b %d %H:%M:%S %z %Y"
                )

                # Mock a Tweepy Status object for _process_tweet
                class MockStatus:
                    def __init__(self, data):
                        self.id = int(data["id"])
                        self.created_at = created_at
                        self.favorite_count = int(data.get("favorite_count", 0))
                        self.retweet_count = int(data.get("retweet_count", 0))
                        self.in_reply_to_status_id = data.get("in_reply_to_status_id")
                        self.full_text = data.get("full_text", "")
                        self.text = self.full_text  # Robustness
                        self.entities = data.get("entities", {})
                        self.extended_entities = data.get("extended_entities", {})

                tweet = MockStatus(tweet_raw)
                await self._process_tweet(tweet, now)

        except Exception as e:
            logging.error(f"Failed to process archive: {e}", exc_info=True)

    async def _process_live_api(self, now: datetime):
        """Fetches tweets from live API and processes them."""
        logging.info("Using live API hybrid fetch...")
        max_id = None
        while True:
            try:
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
                if max_id and tweet.id == max_id:
                    continue
                await self._process_tweet(tweet, now)
                max_id = tweet.id

            await asyncio.sleep(1)

    async def _process_tweet(self, tweet, now: datetime):
        """Applies the rules to a single tweet (Status object) and deletes if necessary."""
        tweet_id = tweet.id

        # Skip if already deleted (checkpointing)
        if await asyncio.to_thread(self.db.is_tweet_deleted, tweet_id):
            logging.debug(f"Skipping already deleted tweet: {tweet_id}")
            self.stats["deleted"] += 1  # Count as deleted for progress tracking
            return

        # v1.1 uses created_at as a datetime object already (usually)
        created_at = tweet.created_at
        if created_at.tzinfo is None:
            created_at = created_at.replace(tzinfo=timezone.utc)

        likes = int(tweet.favorite_count)
        retweets = int(tweet.retweet_count)
        engagement_score = likes + retweets

        # Metadata detection
        text = getattr(tweet, "full_text", getattr(tweet, "text", ""))
        is_response = tweet.in_reply_to_status_id is not None
        age = now - created_at

        # Rich Metadata
        entities = getattr(tweet, "entities", {})
        ext_entities = getattr(tweet, "extended_entities", {})
        has_media = bool(entities.get("media") or ext_entities.get("media"))
        has_link = bool(entities.get("urls"))
        is_thread = "1/" in text or "🧵" in text
        is_retweet = text.startswith("RT @")

        # --- RULE 1: Protected IDs (Pinned, etc.) ---
        if tweet_id in self.protected_ids:
            logging.info(f"KEEP [Protected] ID: {tweet_id}")
            self.stats["skipped"] += 1
            return

        # --- RULE 2: Grace Period (< 7 days) ---
        if age < timedelta(days=self.GRACE_PERIOD_DAYS):
            logging.info(f"KEEP [Recent]    ID: {tweet_id} (Age: {age.days}d)")
            self.stats["skipped"] += 1
            return

        # --- RULE 3: Retweet Cleanup (> 30 days) ---
        if is_retweet and age > timedelta(days=self.RETWEET_MAX_AGE_DAYS):
            reason = f"old retweet (> {self.RETWEET_MAX_AGE_DAYS} days)"
            await self._delete_tweet(tweet, engagement_score, is_response, reason)
            return

        # --- RULE 4: High Value Content (Threads & Media) ---
        if is_thread:
            logging.info(f"KEEP [Thread]    ID: {tweet_id}")
            self.stats["skipped"] += 1
            return

        if has_media:
            logging.info(f"KEEP [Media]     ID: {tweet_id}")
            self.stats["skipped"] += 1
            return

        # --- RULE 5: Critical Age (> 1 year) ---
        if age > timedelta(days=self.CRITICAL_AGE_DAYS):
            reason = (
                f"older than {self.CRITICAL_AGE_DAYS} days and no special protection"
            )
            await self._delete_tweet(tweet, engagement_score, is_response, reason)
            return

        # --- RULE 6: Engagement Thresholds (7 days to 1 year) ---
        if is_response:
            threshold = (
                self.LINK_REPLY_THRESHOLD if has_link else self.POPULAR_REPLY_THRESHOLD
            )
        else:
            threshold = (
                self.LINK_TWEET_THRESHOLD if has_link else self.POPULAR_TWEET_THRESHOLD
            )

        if engagement_score >= threshold:
            kind = "Pop-Reply" if is_response else "Popular"
            if has_link:
                kind += "+Link"
            logging.info(
                f"KEEP [{kind:12}] ID: {tweet_id} ({engagement_score} likes+rt, threshold: {threshold})"
            )
            self.stats["skipped"] += 1
            return

        # --- RULE 7: Otherwise, delete ---
        reason = f"low engagement ({engagement_score} < {threshold})"
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
                    engagement,
                    is_response,
                )
                # Polite delay to avoid hammering the API
                if not self.dry_run:
                    await asyncio.sleep(1)
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
