import pytest
from unittest.mock import MagicMock, patch, call
import os

from src.x_agent.agents.unblock_agent import UnblockAgent
from src.x_agent.services.x_service import XService


@pytest.fixture
def mock_x_service():
    """Fixture for a mocked XService instance."""
    return MagicMock(spec=XService)


@pytest.fixture
def unblock_agent(mock_x_service):
    """Fixture for an UnblockAgent instance with a mocked XService."""
    return UnblockAgent(x_service=mock_x_service)


@pytest.fixture
def mock_db():
    """Fixture for mocking the database module."""
    with patch("src.x_agent.agents.unblock_agent.database") as mock_db:
        yield mock_db


def test_execute_no_blocked_ids_initially(unblock_agent, mock_x_service, mock_db):
    """Test execute when no blocked IDs are in the database and the API returns some."""
    # --- Arrange ---
    # Mock the database to simulate behavior:
    # 1. Agent fetches from API -> returns [1, 2, 3]
    # 2. Agent saves to DB.
    # 3. Agent uses API list for queue.
    mock_db.get_all_blocked_ids_from_db.return_value = {1, 2, 3}
    mock_db.get_unblocked_ids_from_db.return_value = set()

    # Mock the API to return a list of blocked IDs
    mock_x_service.get_blocked_user_ids.return_value = [1, 2, 3]

    # Mock the unblock calls to simulate success (returning True)
    mock_x_service.unblock_user.side_effect = [True, True, True]

    # --- Act ---
    unblock_agent.execute()

    # --- Assert ---
    # Verify database initialization was called
    mock_db.initialize_database.assert_called_once()

    # Verify the agent fetched from the API and saved to the database
    mock_x_service.get_blocked_user_ids.assert_called_once()
    mock_db.add_blocked_ids_to_db.assert_called_once_with({1, 2, 3})

    # Verify all users were unblocked and marked as complete in the database
    assert mock_x_service.unblock_user.call_count == 3
    assert mock_db.mark_user_as_unblocked_in_db.call_count == 3
    mock_db.mark_user_as_unblocked_in_db.assert_any_call(1)
    mock_db.mark_user_as_unblocked_in_db.assert_any_call(2)
    mock_db.mark_user_as_unblocked_in_db.assert_any_call(3)


def test_execute_all_unblocked_from_start(unblock_agent, mock_x_service, mock_db):
    """Test execute when all blocked IDs are already unblocked."""
    mock_db.get_all_blocked_ids_from_db.return_value = {1, 2, 3}
    mock_db.get_unblocked_ids_from_db.return_value = {1, 2, 3}
    # API returns empty list? No, if API returns list, we process it.
    # If API returns empty list, we fall back to DB.
    # Here we assume API returns empty to test the "Nothing to do" path from DB fallback?
    # Or if API returns IDs, we unblock them.
    # The original test likely assumed API returned nothing or matching DB?
    # Let's assume API returns nothing for this test case.
    mock_x_service.get_blocked_user_ids.return_value = []

    unblock_agent.execute()

    # API should be checked
    mock_x_service.get_blocked_user_ids.assert_called_once()
    # Unblock not called because API returned nothing and DB calc showed nothing to do
    mock_x_service.unblock_user.assert_not_called()


def test_execute_resumes_unblocking(unblock_agent, mock_x_service, mock_db):
    """Test execute resumes unblocking, skipping locally completed IDs (Ghost protection)."""

    mock_db.get_all_blocked_ids_from_db.return_value = {1, 2, 3, 4, 5}

    mock_db.get_unblocked_ids_from_db.return_value = {1, 2}

    # The API returns all 5 (including ghosts 1 and 2)

    mock_x_service.get_blocked_user_ids.return_value = [1, 2, 3, 4, 5]

    # It should ONLY try to unblock 3, 4, 5.

    # 1 and 2 are skipped because they are in the DB.

    mock_x_service.unblock_user.side_effect = [True, True, True]

    unblock_agent.execute()

    # Verify calls

    assert mock_x_service.unblock_user.call_count == 3

    mock_x_service.unblock_user.assert_has_calls([call(3), call(4), call(5)])

    # Verify 1 and 2 were NOT called

    call_args_list = mock_x_service.unblock_user.call_args_list

    args = [c[0][0] for c in call_args_list]

    assert 1 not in args

    assert 2 not in args


def test_execute_handles_not_found_users(unblock_agent, mock_x_service, mock_db):
    """Test execute handles users that are not found (deleted/suspended)."""
    mock_db.get_all_blocked_ids_from_db.return_value = {1, 2, 3}
    mock_db.get_unblocked_ids_from_db.return_value = set()
    # The agent now relies on the API return for the queue order
    mock_x_service.get_blocked_user_ids.return_value = [1, 2, 3]

    mock_x_service.unblock_user.side_effect = [True, "NOT_FOUND", True]

    unblock_agent.execute()

    assert mock_x_service.unblock_user.call_count == 3
    assert mock_db.mark_user_as_unblocked_in_db.call_count == 3


def test_execute_handles_unblock_errors(unblock_agent, mock_x_service, mock_db, caplog):
    """Test execute handles generic unblock errors and retries on next run."""
    mock_db.get_all_blocked_ids_from_db.return_value = {1, 2, 3}
    mock_db.get_unblocked_ids_from_db.return_value = set()
    # The agent now relies on the API return for the queue order
    mock_x_service.get_blocked_user_ids.return_value = [1, 2, 3]

    mock_x_service.unblock_user.side_effect = [True, None, True]

    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        unblock_agent.execute()
        assert "Failed to unblock 1 accounts" in caplog.text

    assert mock_x_service.unblock_user.call_count == 3
    assert mock_db.mark_user_as_unblocked_in_db.call_count == 2


def test_execute_no_blocked_ids_from_api(unblock_agent, mock_x_service, mock_db):
    """Test execute when API returns no blocked IDs."""
    mock_db.get_all_blocked_ids_from_db.return_value = set()
    mock_x_service.get_blocked_user_ids.return_value = []

    unblock_agent.execute()

    mock_x_service.get_blocked_user_ids.assert_called_once()
    mock_x_service.unblock_user.assert_not_called()
    mock_db.add_blocked_ids_to_db.assert_not_called()
