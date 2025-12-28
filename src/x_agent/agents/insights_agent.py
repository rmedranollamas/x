import logging
import asyncio
import sqlite3
from typing import Optional
import tweepy
from .base_agent import BaseAgent
from ..services.x_service import XService
from .. import database


class InsightsAgent(BaseAgent):
    """An agent for gathering and reporting daily account insights."""

    agent_name = "insights"

    def __init__(self, x_service: XService) -> None:
        """
        Initializes the agent with a service to interact with the X API.

        Args:
            x_service: An instance of XService.
        """
        self.x_service = x_service

    async def execute(self) -> None:
        """Runs the insights agent to generate and store the daily report."""
        logging.info("Starting the insights agent...")

        await asyncio.to_thread(database.initialize_database)

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

        current_followers = me_data.public_metrics.get("followers_count", 0)
        current_following = me_data.public_metrics.get("following_count", 0)

        # Get historical metrics from the database
        comparisons = {
            "Previous Run": await asyncio.to_thread(database.get_latest_insight),
            "24h Ago": await asyncio.to_thread(database.get_insight_at_offset, 1),
            "7d Ago": await asyncio.to_thread(database.get_insight_at_offset, 7),
            "30d Ago": await asyncio.to_thread(database.get_insight_at_offset, 30),
        }

        # Generate the report
        self._generate_report(current_followers, current_following, comparisons)

        # Save the new metrics to the database
        await asyncio.to_thread(
            database.add_insight, current_followers, current_following
        )

        logging.info("Insights agent finished successfully.")

    def _generate_report(
        self,
        current_followers: int,
        current_following: int,
        comparisons: dict[str, Optional[sqlite3.Row]],
    ) -> None:
        """
        Generates and prints a report comparing current and historical metrics.
        """
        print("\n--- Daily X Account Insights ---")
        print(f"Current Followers: {current_followers}")
        print(f"Current Following: {current_following}")
        print("-" * 32)

        has_any_history = False
        for label, insight in comparisons.items():
            if not insight:
                continue

            has_any_history = True
            prev_followers = insight["followers"]
            prev_following = insight["following"]

            # Avoid comparing with self if the latest insight is the same as current
            # (though we save AFTER reporting, so 'Previous Run' is usually truly previous)
            f_delta = current_followers - prev_followers
            f_sign = "+" if f_delta >= 0 else ""

            l_delta = current_following - prev_following
            l_sign = "+" if l_delta >= 0 else ""

            # Check if there's actually a difference to report for this timeframe
            # or if it's the very first entry.
            print(
                f"{label:12} | Followers: {f_sign}{f_delta:4} | Following: {l_sign}{l_delta:4}"
            )

        if not has_any_history:
            print("No historical data available yet. This is your first run!")

        print("-" * 32 + "\n")
