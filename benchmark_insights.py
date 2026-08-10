import asyncio
import time
from datetime import datetime, timezone
from unittest.mock import AsyncMock, MagicMock
from x_agent.agents.insights_agent import InsightsAgent

async def run_benchmark():
    x_service_mock = MagicMock()
    # Mock methods
    x_service_mock.user_id = "test_user"
    x_service_mock.get_me = AsyncMock(return_value=MagicMock(data=MagicMock(
        public_metrics={"followers_count": 100, "following_count": 50, "tweet_count": 100, "listed_count": 5},
        created_at=datetime.now(timezone.utc)
    )))

    current_ids = set(range(1, 101))
    previous_ids = set(range(51, 151))
    # new_ids: 1-50 (50 new)
    # lost_ids: 101-150 (50 lost)

    x_service_mock.get_follower_user_ids = AsyncMock(return_value=current_ids)

    db_mock = MagicMock()
    db_mock.get_all_follower_ids = MagicMock(return_value=previous_ids)
    db_mock.get_latest_insight = MagicMock(return_value=None)
    db_mock.get_insight_at_offset = MagicMock(return_value=None)

    # We want get_users_by_ids to miss all to trigger fallback for all 100 users
    x_service_mock.get_users_by_ids = AsyncMock(return_value=[])

    async def mock_resolve_user_fallback(uid):
        await asyncio.sleep(0.01) # Simulate network delay
        return f"user_{uid}"

    x_service_mock.resolve_user_fallback = mock_resolve_user_fallback

    agent = InsightsAgent(x_service_mock, db_mock)

    start_time = time.time()
    await agent.execute()
    end_time = time.time()

    print(f"Time taken: {end_time - start_time:.2f} seconds")

if __name__ == "__main__":
    asyncio.run(run_benchmark())
