import pytest
import tweepy
from unittest.mock import MagicMock, AsyncMock
from x_agent.agents.insights_agent import InsightsAgent
from x_agent.services.x_service import XService
from x_agent.database import DatabaseManager


@pytest.fixture
def mock_x_service():
    service = MagicMock(spec=XService)
    service.user_id = 12345
    service.get_me = AsyncMock()
    service.initialize = AsyncMock()
    service.get_follower_user_ids = AsyncMock(return_value=set())
    service.get_users_by_ids = AsyncMock(return_value=[])
    return service


@pytest.fixture
def mock_db_manager():
    mock_db = MagicMock(spec=DatabaseManager)
    # Default offset returns to None to avoid MagicMock comparison errors
    mock_db.get_insight_at_offset.return_value = None
    mock_db.get_all_follower_ids.return_value = set()
    return mock_db


@pytest.fixture
def insights_agent(mock_x_service, mock_db_manager):
    return InsightsAgent(x_service=mock_x_service, db_manager=mock_db_manager)


@pytest.mark.asyncio
async def test_execute_first_run(
    insights_agent, mock_x_service, mock_db_manager, capsys
):
    """Test insights agent behavior on the first run (no previous data)."""
    # Setup API response
    mock_me = MagicMock()
    mock_me.public_metrics = {
        "followers_count": 100,
        "following_count": 50,
        "tweet_count": 10,
        "listed_count": 5,
    }
    mock_me.created_at = None
    mock_x_service.get_me.return_value = MagicMock(data=mock_me)
    mock_x_service.get_follower_user_ids.return_value = {1, 2, 3}

    # No previous insight in DB
    mock_db_manager.get_latest_insight.return_value = None
    mock_db_manager.get_all_follower_ids.return_value = set()

    report = await insights_agent.execute()

    mock_db_manager.initialize_database.assert_called_once()
    mock_db_manager.add_insight.assert_called_once_with(100, 50, 10, 5)
    mock_db_manager.replace_followers.assert_called_once_with({1, 2, 3})

    assert "Followers:  100" in report
    assert "Following: 50" in report


@pytest.mark.asyncio
async def test_execute_with_follower_changes(
    insights_agent, mock_x_service, mock_db_manager
):
    """Test insights agent correctly identifies and reports follower changes."""
    # Current metrics
    mock_me = MagicMock()
    mock_me.public_metrics = {
        "followers_count": 102,
        "following_count": 50,
        "tweet_count": 10,
        "listed_count": 5,
    }
    mock_me.created_at = None
    mock_x_service.get_me.return_value = MagicMock(data=mock_me)

    # Follower IDs: 1 and 2 stayed, 3 left, 4 and 5 joined.
    mock_x_service.get_follower_user_ids.return_value = {1, 2, 4, 5}
    mock_db_manager.get_all_follower_ids.return_value = {1, 2, 3}

    # Mock user resolution
    user4 = MagicMock(spec=tweepy.User)
    user4.username = "new_user4"
    user5 = MagicMock(spec=tweepy.User)
    user5.username = "new_user5"
    user3 = MagicMock(spec=tweepy.User)
    user3.username = "lost_user3"

    async def mock_get_users(ids):
        if 4 in ids and 5 in ids:
            return [user4, user5]
        if 3 in ids:
            return [user3]
        return []

    mock_x_service.get_users_by_ids.side_effect = mock_get_users

    # Previous metrics in DB
    mock_db_manager.get_latest_insight.return_value = {
        "followers": 101,
        "following": 50,
        "tweet_count": 10,
        "listed_count": 5,
    }

    report = await insights_agent.execute()

    assert "FOLLOWER CHANGES" in report
    assert "New (2): @new_user4, @new_user5" in report
    assert "Lost (1): @lost_user3" in report
    mock_db_manager.replace_followers.assert_called_once_with({1, 2, 4, 5})
