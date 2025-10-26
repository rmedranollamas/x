import os
import pytest
from unittest.mock import MagicMock
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


@pytest.fixture(autouse=True)
def cleanup_state_files():
    """Cleans up state files before each test to ensure a clean slate."""
    yield
    if os.path.exists(UnblockAgent.BLOCKED_IDS_FILE):
        os.remove(UnblockAgent.BLOCKED_IDS_FILE)
    if os.path.exists(UnblockAgent.UNBLOCKED_IDS_FILE):
        os.remove(UnblockAgent.UNBLOCKED_IDS_FILE)


def test_load_ids_from_file_non_existent(unblock_agent):
    """Test loading IDs from a non-existent file returns an empty set."""
    assert unblock_agent._load_ids_from_file("non_existent.txt") == set()


def test_load_ids_from_file_empty(unblock_agent, tmp_path):
    """Test loading IDs from an empty file returns an empty set."""
    empty_file = tmp_path / "empty.txt"
    empty_file.write_text("")
    assert unblock_agent._load_ids_from_file(str(empty_file)) == set()


def test_load_ids_from_file_valid_ids(unblock_agent, tmp_path):
    """Test loading valid IDs from a file."""
    valid_file = tmp_path / "valid.txt"
    valid_file.write_text("123\n456\n789")
    assert unblock_agent._load_ids_from_file(str(valid_file)) == {123, 456, 789}


def test_load_ids_from_file_with_invalid_entries(unblock_agent, tmp_path, caplog):
    """Test loading IDs from a file with invalid entries logs warnings and skips them."""
    invalid_file = tmp_path / "invalid.txt"
    invalid_file.write_text("123\nabc\n456")
    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        ids = unblock_agent._load_ids_from_file(str(invalid_file))
        assert ids == {123, 456}
        assert "Skipping invalid non-integer value" in caplog.text


def test_save_ids_to_file(unblock_agent, tmp_path):
    """Test saving IDs to a file."""
    save_file = tmp_path / "save.txt"
    ids_to_save = {111, 222, 333}
    unblock_agent._save_ids_to_file(str(save_file), ids_to_save)
    loaded_ids = unblock_agent._load_ids_from_file(str(save_file))
    assert loaded_ids == ids_to_save


def test_append_id_to_file(unblock_agent, tmp_path):
    """Test appending an ID to a file."""
    append_file = tmp_path / "append.txt"
    append_file.write_text("100\n")
    unblock_agent._append_id_to_file(str(append_file), 200)
    unblock_agent._append_id_to_file(str(append_file), 300)
    loaded_ids = unblock_agent._load_ids_from_file(str(append_file))
    assert loaded_ids == {100, 200, 300}


def test_execute_no_blocked_ids_initially(unblock_agent, mock_x_service):
    """Test execute when no blocked IDs are cached and API returns some."""
    mock_x_service.get_blocked_user_ids.return_value = {1, 2, 3}
    mock_x_service.unblock_user.side_effect = [
        MagicMock(screen_name="user1"),
        MagicMock(screen_name="user2"),
        MagicMock(screen_name="user3"),
    ]

    unblock_agent.execute()

    mock_x_service.get_blocked_user_ids.assert_called_once()
    assert os.path.exists(UnblockAgent.BLOCKED_IDS_FILE)
    assert unblock_agent._load_ids_from_file(UnblockAgent.BLOCKED_IDS_FILE) == {1, 2, 3}
    assert unblock_agent._load_ids_from_file(UnblockAgent.UNBLOCKED_IDS_FILE) == {
        1,
        2,
        3,
    }
    assert mock_x_service.unblock_user.call_count == 3


def test_execute_all_unblocked_from_start(unblock_agent, mock_x_service):
    """Test execute when all blocked IDs are already unblocked."""
    # Simulate blocked_ids.txt and unblocked_ids.txt existing and being identical
    unblock_agent._save_ids_to_file(UnblockAgent.BLOCKED_IDS_FILE, {1, 2, 3})
    unblock_agent._save_ids_to_file(UnblockAgent.UNBLOCKED_IDS_FILE, {1, 2, 3})

    unblock_agent.execute()

    mock_x_service.get_blocked_user_ids.assert_not_called()
    mock_x_service.unblock_user.assert_not_called()


def test_execute_resumes_unblocking(unblock_agent, mock_x_service):
    """Test execute resumes unblocking from where it left off."""
    # Simulate some IDs already blocked and some already unblocked
    unblock_agent._save_ids_to_file(UnblockAgent.BLOCKED_IDS_FILE, {1, 2, 3, 4, 5})
    unblock_agent._save_ids_to_file(UnblockAgent.UNBLOCKED_IDS_FILE, {1, 2})

    mock_x_service.unblock_user.side_effect = [
        MagicMock(screen_name="user3"),
        MagicMock(screen_name="user4"),
        MagicMock(screen_name="user5"),
    ]

    unblock_agent.execute()

    mock_x_service.get_blocked_user_ids.assert_not_called()
    assert mock_x_service.unblock_user.call_count == 3
    assert unblock_agent._load_ids_from_file(UnblockAgent.UNBLOCKED_IDS_FILE) == {
        1,
        2,
        3,
        4,
        5,
    }


def test_execute_handles_not_found_users(unblock_agent, mock_x_service):
    """Test execute handles users that are not found (deleted/suspended)."""
    unblock_agent._save_ids_to_file(UnblockAgent.BLOCKED_IDS_FILE, {1, 2, 3})
    mock_x_service.unblock_user.side_effect = [
        MagicMock(screen_name="user1"),
        "NOT_FOUND",
        MagicMock(screen_name="user3"),
    ]

    unblock_agent.execute()

    assert mock_x_service.unblock_user.call_count == 3
    assert unblock_agent._load_ids_from_file(UnblockAgent.UNBLOCKED_IDS_FILE) == {
        1,
        2,
        3,
    }


def test_execute_handles_unblock_errors(unblock_agent, mock_x_service, caplog):
    """Test execute handles generic unblock errors and retries on next run."""
    unblock_agent._save_ids_to_file(UnblockAgent.BLOCKED_IDS_FILE, {1, 2, 3})
    mock_x_service.unblock_user.side_effect = [
        MagicMock(screen_name="user1"),
        None,  # Simulate a generic error
        MagicMock(screen_name="user3"),
    ]

    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        unblock_agent.execute()
        assert "Failed to unblock 1 accounts" in caplog.text

    assert mock_x_service.unblock_user.call_count == 3
    # User 2 should not be marked as unblocked, so it can be retried
    assert unblock_agent._load_ids_from_file(UnblockAgent.UNBLOCKED_IDS_FILE) == {1, 3}


def test_execute_no_blocked_ids_from_api(unblock_agent, mock_x_service):
    """Test execute when API returns no blocked IDs."""
    mock_x_service.get_blocked_user_ids.return_value = set()

    unblock_agent.execute()

    mock_x_service.get_blocked_user_ids.assert_called_once()
    mock_x_service.unblock_user.assert_not_called()
    assert not os.path.exists(UnblockAgent.BLOCKED_IDS_FILE)
    assert not os.path.exists(UnblockAgent.UNBLOCKED_IDS_FILE)
