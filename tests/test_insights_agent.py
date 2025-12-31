import pytest
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
    return service


@pytest.fixture
def mock_db_manager():
    mock_db = MagicMock(spec=DatabaseManager)
    # Default offset returns to None to avoid MagicMock comparison errors
    mock_db.get_insight_at_offset.return_value = None
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

    # No previous insight in DB
    mock_db_manager.get_latest_insight.return_value = None

    report = await insights_agent.execute()

    mock_db_manager.initialize_database.assert_called_once()
    mock_db_manager.add_insight.assert_called_once_with(100, 50, 10, 5)

    assert "Followers:  100" in report
    assert "Following: 50" in report


@pytest.mark.asyncio
async def test_execute_with_previous_data(
    insights_agent, mock_x_service, mock_db_manager, capsys
):
    """Test insights agent behavior when comparing with previous data."""
    # Current metrics
    mock_me = MagicMock()
    mock_me.public_metrics = {
        "followers_count": 110,
        "following_count": 45,
        "tweet_count": 15,
        "listed_count": 7,
    }
    mock_me.created_at = None
    mock_x_service.get_me.return_value = MagicMock(data=mock_me)

    # Previous metrics in DB
    mock_db_manager.get_latest_insight.return_value = {
        "followers": 100,
        "following": 50,
        "tweet_count": 5,
        "listed_count": 2,
    }

    report = await insights_agent.execute()

    mock_db_manager.add_insight.assert_called_once_with(110, 45, 15, 7)

    assert "Followers:  110" in report
    assert "+10" in report
