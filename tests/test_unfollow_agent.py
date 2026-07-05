import pytest
from unittest.mock import MagicMock, AsyncMock
from x_agent.agents.unfollow_agent import UnfollowAgent
from x_agent.services.x_service import XService
from x_agent.database import DatabaseManager


@pytest.fixture
def mock_x_service():
    service = MagicMock(spec=XService)
    service.ensure_initialized = AsyncMock()
    service.get_follower_user_ids = AsyncMock()
    service.get_users_by_ids = AsyncMock(return_value=[])
    return service


@pytest.fixture
def mock_db_manager():
    return MagicMock(spec=DatabaseManager)


@pytest.fixture
def unfollow_agent(mock_x_service, mock_db_manager):
    return UnfollowAgent(x_service=mock_x_service, db_manager=mock_db_manager)


@pytest.mark.asyncio
async def test_execute_first_run(unfollow_agent, mock_x_service, mock_db_manager):
    """Test first run where no previous followers exist in DB."""
    mock_x_service.get_follower_user_ids.return_value = {101, 102}
    mock_db_manager.get_all_follower_ids.return_value = set()

    await unfollow_agent.execute()

    mock_x_service.get_follower_user_ids.assert_awaited_once()
    mock_db_manager.get_all_follower_ids.assert_called_once()
    mock_db_manager.replace_followers.assert_called_once_with({101, 102})
    mock_db_manager.log_unfollows.assert_not_called()


@pytest.mark.asyncio
async def test_execute_detect_unfollow(unfollow_agent, mock_x_service, mock_db_manager):
    """Test detecting an unfollow (ID 101 is gone)."""
    mock_x_service.get_follower_user_ids.return_value = {102, 103}
    mock_db_manager.get_all_follower_ids.return_value = {101, 102}

    await unfollow_agent.execute()

    # 101 was in DB but not in API -> Unfollowed
    # 103 is new
    mock_db_manager.log_unfollows.assert_called_once_with([101])
    mock_db_manager.replace_followers.assert_called_once_with({102, 103})


@pytest.mark.asyncio
async def test_execute_no_changes(unfollow_agent, mock_x_service, mock_db_manager):
    """Test when follower list remains the same."""
    mock_x_service.get_follower_user_ids.return_value = {101, 102}
    mock_db_manager.get_all_follower_ids.return_value = {101, 102}

    await unfollow_agent.execute()

    mock_db_manager.log_unfollows.assert_not_called()
    mock_db_manager.replace_followers.assert_called_once_with({101, 102})

@pytest.mark.asyncio
async def test_execute_report_handles(unfollow_agent, mock_x_service, mock_db_manager):
    """Test that the agent resolves usernames for unfollowers and reports them."""
    import tweepy
    mock_x_service.get_follower_user_ids.return_value = {102, 103}
    mock_db_manager.get_all_follower_ids.return_value = {101, 102}

    user101 = MagicMock(spec=tweepy.User)
    user101.id = 101
    user101.username = "unfollowed_user"

    mock_x_service.get_users_by_ids.return_value = [user101]

    report = await unfollow_agent.execute()

    assert "- @unfollowed_user (ID: 101)" in report
    assert "Unfollows:       1" in report
