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

    async def execute(self) -> None:
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
            return

        if not me_data or not me_data.public_metrics:
            logging.error("Could not retrieve user metrics. Aborting.")
            return

        metrics = me_data.public_metrics
        current_followers = metrics.get("followers_count", 0)
        current_following = metrics.get("following_count", 0)
        current_tweets = metrics.get("tweet_count", 0)
        current_listed = metrics.get("listed_count", 0)
        created_at = me_data.created_at

        # Get historical metrics from the database
        comparisons = {
            "Previous": await asyncio.to_thread(self.db.get_latest_insight),
            "24h Ago": await asyncio.to_thread(self.db.get_insight_at_offset, 1),
            "7d Ago": await asyncio.to_thread(self.db.get_insight_at_offset, 7),
            "30d Ago": await asyncio.to_thread(self.db.get_insight_at_offset, 30),
        }

        # Generate the report
        self._generate_report(
            current_followers,
            current_following,
            current_tweets,
            current_listed,
            created_at,
            comparisons,
        )

        # Save the new metrics to the database
        await asyncio.to_thread(
            self.db.add_insight,
            current_followers,
            current_following,
            current_tweets,
            current_listed,
        )

        logging.info("Insights agent finished successfully.")

    def _generate_report(
        self,
        current_followers: int,
        current_following: int,
        current_tweets: int,
        current_listed: int,
        created_at: Optional[Union[datetime, time.struct_time]],
        comparisons: dict[str, Optional[sqlite3.Row]],
    ) -> None:
        """
        Generates and prints a comprehensive report.
        """
        print("\n" + "=" * 55)
        print("         🚀 X ACCOUNT MASTER INSIGHTS 🚀         ")
        print("=" * 55)

        # 1. Core Metrics & Ratios
        ratio = current_followers / current_following if current_following > 0 else 0
        print(f"Followers:  {current_followers:<8} | Following: {current_following}")
        print(f"Tweets:     {current_tweets:<8} | Ratio:     {ratio:.2f}")
        print(f"Listed In:  {current_listed:<8}")
        print("-" * 55)

        # 2. Account Vitality (New Section)
        if created_at:
            # Tweepy created_at is often a datetime object in newer versions,
            # but let's handle it safely.
            if isinstance(created_at, datetime):
                creation_dt = created_at
            else:
                # Fallback for struct_time if returned by some Tweepy versions
                creation_dt = datetime.fromtimestamp(
                    time.mktime(created_at), tz=timezone.utc
                )

            now = datetime.now(timezone.utc)
            age_days = max((now - creation_dt).days, 1)
            avg_tweets_per_day = current_tweets / age_days

            print("                ACCOUNT VITALITY                 ")
            print(f"Account Age:      {age_days:,} days")
            print(
                f"Tweet Frequency:  {avg_tweets_per_day:.2f} tweets/day (lifetime avg)"
            )
            print("-" * 55)

        # 3. Historical Comparisons
        print(f"{'Timeframe':12} | {'Followers':10} | {'Tweets':8} | {'Listed':6}")
        print("-" * 55)

        has_history = False
        for label, insight in comparisons.items():
            if not insight:
                continue
            has_history = True

            f_delta = current_followers - insight["followers"]
            t_delta = current_tweets - insight["tweet_count"]
            l_delta = current_listed - insight["listed_count"]

            f_sign = "+" if f_delta >= 0 else "-"
            t_sign = "+" if t_delta >= 0 else "-"
            l_sign = "+" if l_delta >= 0 else "-"

            f_delta_str = f"{f_sign}{abs(f_delta)}"
            t_delta_str = f"{t_sign}{abs(t_delta)}"
            l_delta_str = f"{l_sign}{abs(l_delta)}"

            print(
                f"{label:12} | {f_delta_str:>10} | {t_delta_str:>8} | {l_delta_str:>6}"
            )

        if not has_history:
            print("No historical data yet. First run recorded!")

        print("-" * 55)

        # 3. Growth Velocity & Projections (if 24h data exists)
        day_insight = comparisons.get("24h Ago") or comparisons.get("Previous")
        if day_insight:
            # Simple average-based projection
            # Calculate days since that insight (rough estimate)
            try:
                # SQLite timestamp format: '2025-12-28 12:00:00.000'
                ts_str = day_insight["timestamp"].split(".")[0]
                struct_time = time.strptime(ts_str, "%Y-%m-%d %H:%M:%S")
                delta_seconds = time.time() - time.mktime(struct_time)
                delta_days = delta_seconds / 86400 or 1  # avoid div by zero
            except Exception:
                delta_days = 1

            daily_velocity = (current_followers - day_insight["followers"]) / delta_days

            if daily_velocity > 0:
                print(f"Growth Velocity: {daily_velocity:.1f} followers/day")

                # Projections to next milestones
                for milestone in [100, 500, 1000, 5000, 10000]:
                    if current_followers < milestone:
                        days_to_go = (milestone - current_followers) / daily_velocity
                        print(f"Next Milestone:  {milestone} in {int(days_to_go)} days")
                        break
            elif daily_velocity < 0:
                print(
                    f"Growth Velocity: {daily_velocity:.1f} followers/day (Losing altitude!)"
                )

        print("=" * 45 + "\n")
