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
    }
    mock_x_service.get_me.return_value = MagicMock(data=mock_me)

    # No previous insight in DB
    mock_db_manager.get_latest_insight.return_value = None

    await insights_agent.execute()

    mock_db_manager.initialize_database.assert_called_once()
    mock_db_manager.add_insight.assert_called_once_with(100, 50, 10)

    captured = capsys.readouterr()
    assert "Followers:  100" in captured.out
    assert "Following: 50" in captured.out


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
    }
    mock_x_service.get_me.return_value = MagicMock(data=mock_me)

    # Previous metrics in DB
    mock_db_manager.get_latest_insight.return_value = {
        "followers": 100,
        "following": 50,
        "tweet_count": 5,
    }

    await insights_agent.execute()

    mock_db_manager.add_insight.assert_called_once_with(110, 45, 15)

    captured = capsys.readouterr()
    assert "Followers:  110" in captured.out
    assert "+10" in captured.out
    assert "+10" in captured.out  # Tweet delta 15-5
