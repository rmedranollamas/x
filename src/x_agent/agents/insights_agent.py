import logging
import asyncio
import sqlite3
import time
from datetime import datetime, timezone
from typing import Optional, TYPE_CHECKING, Union
import tweepy
from .base_agent import BaseAgent
from ..services.x_service import XService

if TYPE_CHECKING:
    from ..database import DatabaseManager


class InsightsAgent(BaseAgent):
    """An agent for gathering and reporting comprehensive account insights."""

    agent_name = "insights"

    def __init__(
        self, x_service: XService, db_manager: "DatabaseManager", **kwargs
    ) -> None:
        """
        Initializes the agent.

        Args:
            x_service: An instance of XService.
            db_manager: Database Manager.
        """
        super().__init__(db_manager)
        self.x_service = x_service

    async def execute(self) -> str | None:
        """Runs the insights agent to generate and store the report."""
        logging.info("Starting the insights agent...")

        await asyncio.to_thread(self.db.initialize_database)

        try:
            if not self.x_service.user_id:
                await self.x_service.initialize()

            response = await self.x_service.get_me()
            me_data = response.data
        except tweepy.errors.TweepyException as e:
            logging.error(f"Could not retrieve user metrics: {e}")
            return None

        if not me_data or not me_data.public_metrics:
            logging.error("Could not retrieve user metrics. Aborting.")
            return None

        metrics = me_data.public_metrics
        current_followers_count = metrics.get("followers_count", 0)
        current_following_count = metrics.get("following_count", 0)
        current_tweets_count = metrics.get("tweet_count", 0)
        current_listed_count = metrics.get("listed_count", 0)
        created_at = me_data.created_at

        # Follower change detection (logic from UnfollowAgent)
        logging.info("Fetching current follower IDs for change detection...")
        current_follower_ids = await self.x_service.get_follower_user_ids()
        previous_follower_ids = await asyncio.to_thread(self.db.get_all_follower_ids)

        new_follower_users = []
        lost_follower_users = []

        if previous_follower_ids:
            new_ids = list(current_follower_ids - previous_follower_ids)
            lost_ids = list(previous_follower_ids - current_follower_ids)

            if new_ids:
                logging.info(f"Resolving {len(new_ids)} new follower usernames...")
                new_follower_users = await self.x_service.get_users_by_ids(new_ids)

            if lost_ids:
                logging.info(f"Resolving {len(lost_ids)} lost follower usernames...")
                lost_follower_users = await self.x_service.get_users_by_ids(lost_ids)

        # Update follower list in DB
        await asyncio.to_thread(self.db.replace_followers, current_follower_ids)

        # Get historical metrics from the database for timeframes
        comparisons = {
            "Previous": await asyncio.to_thread(self.db.get_latest_insight),
            "24h Ago": await asyncio.to_thread(self.db.get_insight_at_offset, 1),
            "7d Ago": await asyncio.to_thread(self.db.get_insight_at_offset, 7),
            "30d Ago": await asyncio.to_thread(self.db.get_insight_at_offset, 30),
        }

        # Generate the report
        report = self._generate_report(
            current_followers_count,
            current_following_count,
            current_tweets_count,
            current_listed_count,
            created_at,
            comparisons,
            new_follower_users,
            lost_follower_users,
        )

        # Print to stdout as before
        print(report)

        # Save the new metrics to the database
        await asyncio.to_thread(
            self.db.add_insight,
            current_followers_count,
            current_following_count,
            current_tweets_count,
            current_listed_count,
        )

        logging.info("Insights agent finished successfully.")
        return report

    def _generate_report(
        self,
        current_followers: int,
        current_following: int,
        current_tweets: int,
        current_listed: int,
        created_at: Optional[Union[datetime, time.struct_time]],
        comparisons: dict[str, Optional[sqlite3.Row]],
        new_followers: list[tweepy.User],
        lost_followers: list[tweepy.User],
    ) -> str:
        """
        Generates a comprehensive report optimized for narrow screens.
        """
        width = 42
        lines = []
        lines.append("\n" + "=" * width)
        lines.append("      🚀 X ACCOUNT MASTER INSIGHTS 🚀")
        lines.append("=" * width)

        # 1. Core Metrics
        ratio = current_followers / current_following if current_following > 0 else 0
        lines.append(f"Followers: {current_followers:,}")
        lines.append(f"Following: {current_following:,}")
        lines.append(f"Tweets:    {current_tweets:,}")
        lines.append(f"Listed:    {current_listed:,}")
        lines.append(f"Ratio:     {ratio:.2f}")
        lines.append("-" * width)

        # 2. Follower Changes
        if new_followers or lost_followers:
            lines.append("           FOLLOWERS LOG")
            if new_followers:
                lines.append(f"New ({len(new_followers)}):")
                for u in new_followers:
                    lines.append(f" + @{u.username}")
            if lost_followers:
                lines.append(f"Lost ({len(lost_followers)}):")
                for u in lost_followers:
                    lines.append(f" - @{u.username}")
            lines.append("-" * width)

        # 3. Account Vitality
        if created_at:
            if isinstance(created_at, datetime):
                creation_dt = created_at
            else:
                creation_dt = datetime.fromtimestamp(
                    time.mktime(created_at), tz=timezone.utc
                )

            now = datetime.now(timezone.utc)
            age_days = max((now - creation_dt).days, 1)
            avg_tweets_per_day = current_tweets / age_days

            lines.append("          ACCOUNT VITALITY")
            lines.append(f"Age:      {age_days:,} days")
            lines.append(f"Activity: {avg_tweets_per_day:.2f} tweets/day")
            lines.append("-" * width)

        # 4. Historical Comparisons
        lines.append(f"{'Period':9} | {'Follows':7} | {'Tweets':6} | {'List'}")
        lines.append("-" * width)

        has_history = False
        for label, insight in comparisons.items():
            if not insight:
                continue
            has_history = True

            f_delta = current_followers - insight["followers"]
            t_delta = current_tweets - insight["tweet_count"]
            l_delta = current_listed - insight["listed_count"]

            f_delta_str = f"{f_delta:+}"
            t_delta_str = f"{t_delta:+}"
            l_delta_str = f"{l_delta:+}"

            lines.append(
                f"{label:9} | {f_delta_str:>7} | {t_delta_str:>6} | {l_delta_str}"
            )

        if not has_history:
            lines.append("No historical data recorded yet.")

        lines.append("-" * width)

        # 5. Growth Velocity & Projections
        day_insight = comparisons.get("24h Ago") or comparisons.get("Previous")
        if day_insight:
            try:
                ts_str = day_insight["timestamp"].split(".")[0]
                struct_time = time.strptime(ts_str, "%Y-%m-%d %H:%M:%S")
                delta_seconds = time.time() - time.mktime(struct_time)
                delta_days = delta_seconds / 86400 or 1
            except Exception:
                delta_days = 1

            daily_velocity = (current_followers - day_insight["followers"]) / delta_days

            if daily_velocity > 0:
                lines.append(f"Velocity:  {daily_velocity:.1f} followers/day")
                for milestone in [100, 500, 1000, 5000, 10000, 50000, 100000]:
                    if current_followers < milestone:
                        days_to_go = (milestone - current_followers) / daily_velocity
                        lines.append(f"Target:    {milestone:,} in {int(days_to_go)}d")
                        break
            elif daily_velocity < 0:
                lines.append(f"Velocity:  {daily_velocity:.1f} (Downwards)")

        lines.append("=" * width + "\n")
        return "\n".join(lines)
        return "\n".join(lines)
