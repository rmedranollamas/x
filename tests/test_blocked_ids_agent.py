import pytest
import logging
from unittest.mock import MagicMock, AsyncMock
from x_agent.agents.blocked_ids_agent import BlockedIdsAgent
from x_agent.services.x_service import XService
from x_agent.database import DatabaseManager


@pytest.fixture
def mock_x_service():
    service = MagicMock(spec=XService)
    service.get_blocked_user_ids = AsyncMock()
    return service


@pytest.fixture
def mock_db_manager():
    return MagicMock(spec=DatabaseManager)


@pytest.fixture
def blocked_ids_agent(mock_x_service, mock_db_manager):
    return BlockedIdsAgent(x_service=mock_x_service, db_manager=mock_db_manager)


@pytest.mark.asyncio
async def test_execute_success(blocked_ids_agent, mock_x_service, capsys, caplog):
    """Test successful execution of the blocked IDs agent."""
    mock_x_service.get_blocked_user_ids.return_value = {101, 102}

    with caplog.at_level(logging.INFO):
        await blocked_ids_agent.execute()

    mock_x_service.get_blocked_user_ids.assert_awaited_once()

    # Check log messages
    assert "Found 2 blocked user IDs" in caplog.text

    # Check printed IDs
    captured = capsys.readouterr()
    assert "101" in captured.out
    assert "102" in captured.out


@pytest.mark.asyncio
async def test_execute_no_ids(blocked_ids_agent, mock_x_service, capsys, caplog):
    """Test execution when no blocked IDs are found."""
    mock_x_service.get_blocked_user_ids.return_value = set()

    with caplog.at_level(logging.INFO):
        await blocked_ids_agent.execute()

    mock_x_service.get_blocked_user_ids.assert_awaited_once()

    # Check log message
    assert "No blocked IDs found" in caplog.text
