import pytest
from unittest.mock import MagicMock, patch, call
from src.x_agent.agents.blocked_ids_agent import BlockedIdsAgent
from src.x_agent.services.x_service import XService


@pytest.fixture
def mock_x_service():
    return MagicMock(spec=XService)


@pytest.fixture
def blocked_ids_agent(mock_x_service):
    return BlockedIdsAgent(mock_x_service)


def test_execute_found_ids(blocked_ids_agent, mock_x_service):
    """Test execution when blocked IDs are found."""
    mock_x_service.get_blocked_user_ids.return_value = [123, 456]

    with patch("builtins.print") as mock_print:
        blocked_ids_agent.execute()

        mock_x_service.get_blocked_user_ids.assert_called_once()
        assert mock_print.call_count == 2
        mock_print.assert_has_calls([call(123), call(456)])


def test_execute_no_ids(blocked_ids_agent, mock_x_service):
    """Test execution when no blocked IDs are found."""
    mock_x_service.get_blocked_user_ids.return_value = []

    with patch("builtins.print") as mock_print:
        blocked_ids_agent.execute()

        mock_x_service.get_blocked_user_ids.assert_called_once()
        mock_print.assert_not_called()
