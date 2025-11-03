
import logging
from .base_agent import BaseAgent
from ..services.x_service import XService
from .. import database

class InsightsAgent(BaseAgent):
    """An agent for gathering and reporting daily account insights."""

    agent_name = "insights"

    def execute(self, x_service: XService):
        """Runs the insights agent to generate and store the daily report."""
        self.x_service = x_service
        logging.info("Starting the insights agent...")
        database.initialize_database()

        # Get the latest metrics from the X API
        me = self.x_service.get_me()
        if not me or not me.public_metrics:
            logging.error("Could not retrieve user metrics. Aborting.")
            return

        current_followers = me.public_metrics.get("followers_count", 0)
        current_following = me.public_metrics.get("following_count", 0)

        # Get the previous metrics from the database
        latest_insight = database.get_latest_insight()

        # Generate the report
        self._generate_report(current_followers, current_following, latest_insight)

        # Save the new metrics to the database
        database.add_insight(current_followers, current_following)

        logging.info("Insights agent finished successfully.")

    def _generate_report(self, current_followers, current_following, latest_insight):
        """Generates and prints a report comparing current and previous metrics."""
        report_lines = ["\n--- Daily X Account Insights ---"]

        if latest_insight:
            prev_followers = latest_insight["followers"]
            prev_following = latest_insight["following"]

            follower_change = current_followers - prev_followers
            following_change = current_following - prev_following

            report_lines.append(f"Followers: {current_followers} ({follower_change:+.0f})")
            report_lines.append(f"Following: {current_following} ({following_change:+.0f})")
        else:
            report_lines.append(f"Followers: {current_followers} (First run)")
            report_lines.append(f"Following: {current_following} (First run)")

        report_lines.append("--------------------------------\n")
        print("\n".join(report_lines))
