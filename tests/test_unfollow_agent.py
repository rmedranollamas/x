import pytest
from unittest.mock import MagicMock, AsyncMock, patch
from src.x_agent.agents.unfollow_agent import UnfollowAgent
from src.x_agent.services.x_service import XService


@pytest.fixture
def mock_x_service():
    service = MagicMock(spec=XService)
    service.get_following_user_ids = AsyncMock()
    service.get_follower_user_ids = AsyncMock()
    service.unfollow_user = AsyncMock()
    return service


@pytest.fixture
def mock_database():
    with patch("src.x_agent.agents.unfollow_agent.database") as mock_db:
        yield mock_db


@pytest.fixture
def unfollow_agent(mock_x_service):
    return UnfollowAgent(x_service=mock_x_service)


@pytest.mark.asyncio
async def test_execute_empty_db_fetches_from_api(
    unfollow_agent, mock_x_service, mock_database
):
    """Test execute fetches from API and filters non-followers."""
    mock_database.get_all_following_users_count.return_value = 0
    mock_x_service.get_following_user_ids.return_value = {1, 2, 3}
    mock_x_service.get_follower_user_ids.return_value = {2}  # Only 2 follows back

    mock_database.get_pending_following_users.return_value = [1, 3]
    mock_database.get_processed_following_count.return_value = 0
    mock_x_service.unfollow_user.side_effect = ["SUCCESS", "SUCCESS"]

    await unfollow_agent.execute()

    mock_x_service.get_following_user_ids.assert_awaited_once()
    mock_x_service.get_follower_user_ids.assert_awaited_once()
    # Should add 1 and 3 (non-followers)
    mock_database.add_following_users.assert_called_once_with({1, 3})
    assert mock_x_service.unfollow_user.await_count == 2
    mock_database.update_following_status.assert_any_call([1, 3], "UNFOLLOWED")


@pytest.mark.asyncio
async def test_execute_resumes_from_db(unfollow_agent, mock_x_service, mock_database):
    """Test execute resumes when DB has data."""
    mock_database.get_all_following_users_count.return_value = 5
    mock_database.get_pending_following_users.return_value = [4, 5]
    mock_database.get_processed_following_count.return_value = 3

    mock_x_service.unfollow_user.side_effect = ["SUCCESS", "SUCCESS"]

    await unfollow_agent.execute()

    mock_x_service.get_following_user_ids.assert_not_called()
    assert mock_x_service.unfollow_user.await_count == 2
    mock_database.update_following_status.assert_any_call([4, 5], "UNFOLLOWED")


@pytest.mark.asyncio
async def test_execute_no_non_followers(unfollow_agent, mock_x_service, mock_database):
    """Test execute when everyone follows back."""
    mock_database.get_all_following_users_count.return_value = 0
    mock_x_service.get_following_user_ids.return_value = {1, 2}
    mock_x_service.get_follower_user_ids.return_value = {1, 2}

    await unfollow_agent.execute()

    mock_database.add_following_users.assert_not_called()
    mock_x_service.unfollow_user.assert_not_called()
