import pytest
from unittest.mock import MagicMock, AsyncMock, patch
from x_agent.agents.unfollow_agent import UnfollowAgent
from x_agent.services.x_service import XService


@pytest.fixture
def mock_x_service():
    service = MagicMock(spec=XService)
    service.ensure_initialized = AsyncMock()
    service.get_follower_user_ids = AsyncMock()
    return service


@pytest.fixture
def mock_database():
    with patch("x_agent.agents.unfollow_agent.database") as mock_db:
        yield mock_db


@pytest.fixture
def unfollow_agent(mock_x_service):
    return UnfollowAgent(x_service=mock_x_service)


@pytest.mark.asyncio
async def test_execute_first_run(unfollow_agent, mock_x_service, mock_database):
    """Test first run where no previous followers exist in DB."""
    mock_x_service.get_follower_user_ids.return_value = {101, 102}
    mock_database.get_all_follower_ids.return_value = set()

    await unfollow_agent.execute()

    mock_x_service.get_follower_user_ids.assert_awaited_once()
    mock_database.get_all_follower_ids.assert_called_once()
    mock_database.replace_followers.assert_called_once_with({101, 102})
    mock_database.log_unfollows.assert_not_called()


@pytest.mark.asyncio
async def test_execute_detect_unfollow(unfollow_agent, mock_x_service, mock_database):
    """Test detecting an unfollow (ID 101 is gone)."""
    mock_x_service.get_follower_user_ids.return_value = {102, 103}
    mock_database.get_all_follower_ids.return_value = {101, 102}

    await unfollow_agent.execute()

    # 101 was in DB but not in API -> Unfollowed
    # 103 is new
    mock_database.log_unfollows.assert_called_once_with([101])
    mock_database.replace_followers.assert_called_once_with({102, 103})


@pytest.mark.asyncio
async def test_execute_no_changes(unfollow_agent, mock_x_service, mock_database):
    """Test when follower list remains the same."""
    mock_x_service.get_follower_user_ids.return_value = {101, 102}
    mock_database.get_all_follower_ids.return_value = {101, 102}

    await unfollow_agent.execute()

    mock_database.log_unfollows.assert_not_called()
    mock_database.replace_followers.assert_called_once_with({101, 102})
