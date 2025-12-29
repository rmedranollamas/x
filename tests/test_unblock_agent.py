import pytest
from unittest.mock import MagicMock, AsyncMock, call
from x_agent.agents.unblock_agent import UnblockAgent
from x_agent.services.x_service import XService
from x_agent.database import DatabaseManager


@pytest.fixture
def mock_x_service():
    """Fixture for a mocked XService instance."""
    service = MagicMock(spec=XService)
    service.get_blocked_user_ids = AsyncMock()
    service.unblock_user = AsyncMock()
    service.ensure_initialized = AsyncMock()
    return service


@pytest.fixture
def mock_db_manager():
    """Fixture for mocked DatabaseManager."""
    return MagicMock(spec=DatabaseManager)


@pytest.fixture
def unblock_agent(mock_x_service, mock_db_manager):
    """Fixture for an UnblockAgent instance with a mocked XService and DB."""
    return UnblockAgent(x_service=mock_x_service, db_manager=mock_db_manager)


def test_unblock_agent_init_with_invalid_user_id_type(mock_x_service, mock_db_manager):
    """
    Test that UnblockAgent's __init__ method raises TypeError when user_id is not an integer.
    """
    # Test with a string user_id
    with pytest.raises(TypeError, match="User ID must be an integer"):
        UnblockAgent(
            x_service=mock_x_service, db_manager=mock_db_manager, user_id="invalid"
        )

    # Test with a float user_id
    with pytest.raises(TypeError, match="User ID must be an integer"):
        UnblockAgent(
            x_service=mock_x_service, db_manager=mock_db_manager, user_id=123.45
        )

    # Test with a list user_id
    with pytest.raises(TypeError, match="User ID must be an integer"):
        UnblockAgent(
            x_service=mock_x_service, db_manager=mock_db_manager, user_id=[123]
        )


@pytest.mark.asyncio
async def test_execute_empty_db_fetches_from_api(
    unblock_agent, mock_x_service, mock_db_manager
):
    """Test execute fetches from API when DB is empty."""
    # Setup: DB returns 0 blocked users initially
    mock_db_manager.get_all_blocked_users_count.side_effect = [
        0,
        3,
    ]  # Initial, then after insert
    # API returns 3 users
    mock_x_service.get_blocked_user_ids.return_value = {1, 2, 3}

    # DB returns pending users after insert
    mock_db_manager.get_pending_blocked_users.return_value = [1, 2, 3]
    mock_db_manager.get_processed_users_count.return_value = 0

    # Mock unblock responses
    mock_x_service.unblock_user.side_effect = ["SUCCESS", "SUCCESS", "SUCCESS"]

    await unblock_agent.execute()

    # Note: we check if initialize_database was called on the instance
    mock_db_manager.initialize_database.assert_called_once()
    mock_x_service.get_blocked_user_ids.assert_awaited_once()
    mock_db_manager.add_blocked_users.assert_called_once_with({1, 2, 3})
    assert mock_x_service.unblock_user.await_count == 3

    # Check that batch status updates happened
    mock_db_manager.update_user_statuses.assert_any_call([1, 2, 3], "UNBLOCKED")


@pytest.mark.asyncio
async def test_execute_resumes_from_db(unblock_agent, mock_x_service, mock_db_manager):
    """Test execute resumes when DB has data."""
    # Setup: DB has 5 total, 2 processed
    mock_db_manager.get_all_blocked_users_count.return_value = 5
    # Pending are 3, 4, 5
    mock_db_manager.get_pending_blocked_users.return_value = [3, 4, 5]
    mock_db_manager.get_processed_users_count.return_value = 2

    mock_x_service.unblock_user.side_effect = ["SUCCESS", "SUCCESS", "SUCCESS"]

    await unblock_agent.execute()

    mock_x_service.get_blocked_user_ids.assert_not_called()
    assert mock_x_service.unblock_user.await_count == 3
    mock_db_manager.update_user_statuses.assert_any_call([3, 4, 5], "UNBLOCKED")


@pytest.mark.asyncio
async def test_execute_with_user_id(mock_x_service, mock_db_manager):
    """Test execute with a specific user_id."""
    agent = UnblockAgent(mock_x_service, db_manager=mock_db_manager, user_id=123)
    mock_x_service.unblock_user.return_value = "SUCCESS"

    await agent.execute()

    mock_x_service.unblock_user.assert_awaited_once_with(123)
    mock_db_manager.update_user_status.assert_called_with(123, "UNBLOCKED")


@pytest.mark.asyncio
async def test_execute_handles_not_found_and_errors(
    unblock_agent, mock_x_service, mock_db_manager
):
    """Test execute handles NOT_FOUND and failure cases."""
    mock_db_manager.get_all_blocked_users_count.return_value = 3
    mock_db_manager.get_pending_blocked_users.return_value = [1, 2, 3]
    mock_db_manager.get_processed_users_count.return_value = 0

    mock_x_service.unblock_user.side_effect = [
        "SUCCESS",  # User 1
        "NOT_FOUND",  # User 2
        "FAILED",  # User 3
    ]

    await unblock_agent.execute()

    assert mock_x_service.unblock_user.await_count == 3
    mock_db_manager.update_user_statuses.assert_has_calls(
        [
            call([1], "UNBLOCKED"),
            call([2], "NOT_FOUND"),
            call([3], "FAILED"),
        ],
        any_order=True,
    )
