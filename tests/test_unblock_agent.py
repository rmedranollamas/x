import pytest
from unittest.mock import MagicMock, AsyncMock, patch, call
from src.x_agent.agents.unblock_agent import UnblockAgent
from src.x_agent.services.x_service import XService


@pytest.fixture
def mock_x_service():
    """Fixture for a mocked XService instance."""
    service = MagicMock(spec=XService)
    service.get_blocked_user_ids = AsyncMock()
    service.unblock_user = AsyncMock()
    return service


@pytest.fixture
def mock_database():
    """Fixture for mocked database module."""
    with patch("src.x_agent.agents.unblock_agent.database") as mock_db:
        yield mock_db


@pytest.fixture
def unblock_agent(mock_x_service):
    """Fixture for an UnblockAgent instance with a mocked XService."""
    return UnblockAgent(x_service=mock_x_service)


@pytest.mark.asyncio
async def test_execute_empty_db_fetches_from_api(
    unblock_agent, mock_x_service, mock_database
):
    """Test execute fetches from API when DB is empty."""
    # Setup: DB returns 0 blocked users initially
    mock_database.get_all_blocked_users_count.side_effect = [
        0,
        3,
    ]  # Initial, then after insert
    # API returns 3 users
    mock_x_service.get_blocked_user_ids.return_value = {1, 2, 3}

    # DB returns pending users after insert
    mock_database.get_pending_blocked_users.return_value = [1, 2, 3]
    mock_database.get_processed_users_count.return_value = 0

    # Mock unblock responses
    mock_x_service.unblock_user.side_effect = ["SUCCESS", "SUCCESS", "SUCCESS"]

    await unblock_agent.execute()

    mock_database.initialize_database.assert_called_once()
    mock_x_service.get_blocked_user_ids.assert_awaited_once()
    mock_database.add_blocked_users.assert_called_once_with({1, 2, 3})
    assert mock_x_service.unblock_user.await_count == 3

    # Check that batch status updates happened
    mock_database.update_user_statuses.assert_any_call([1, 2, 3], "UNBLOCKED")


@pytest.mark.asyncio
async def test_execute_resumes_from_db(unblock_agent, mock_x_service, mock_database):
    """Test execute resumes when DB has data."""
    # Setup: DB has 5 total, 2 processed
    mock_database.get_all_blocked_users_count.return_value = 5
    # Pending are 3, 4, 5
    mock_database.get_pending_blocked_users.return_value = [3, 4, 5]
    mock_database.get_processed_users_count.return_value = 2

    mock_x_service.unblock_user.side_effect = ["SUCCESS", "SUCCESS", "SUCCESS"]

    await unblock_agent.execute()

    mock_x_service.get_blocked_user_ids.assert_not_called()
    assert mock_x_service.unblock_user.await_count == 3
    mock_database.update_user_statuses.assert_any_call([3, 4, 5], "UNBLOCKED")


@pytest.mark.asyncio
async def test_execute_with_user_id(mock_x_service):
    """Test execute with a specific user_id."""
    agent = UnblockAgent(mock_x_service, user_id=123)
    mock_x_service.unblock_user.return_value = "SUCCESS"

    await agent.execute()

    mock_x_service.unblock_user.assert_awaited_once_with(123)


@pytest.mark.asyncio
async def test_execute_handles_not_found_and_errors(
    unblock_agent, mock_x_service, mock_database
):
    """Test execute handles NOT_FOUND and failure cases."""
    mock_database.get_all_blocked_users_count.return_value = 3
    mock_database.get_pending_blocked_users.return_value = [1, 2, 3]
    mock_database.get_processed_users_count.return_value = 0

    mock_x_service.unblock_user.side_effect = [
        "SUCCESS",  # User 1
        "NOT_FOUND",  # User 2
        "FAILED",  # User 3
    ]

    await unblock_agent.execute()

    assert mock_x_service.unblock_user.await_count == 3
    mock_database.update_user_statuses.assert_has_calls(
        [
            call([1], "UNBLOCKED"),
            call([2], "NOT_FOUND"),
            call([3], "FAILED"),
        ],
        any_order=True,
    )