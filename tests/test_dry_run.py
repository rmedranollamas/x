import pytest
import logging
from unittest.mock import MagicMock, AsyncMock
from x_agent.agents.unblock_agent import UnblockAgent
from x_agent.agents.unfollow_agent import UnfollowAgent
from x_agent.services.x_service import XService
from x_agent.database import DatabaseManager


@pytest.fixture
def mock_x_service():
    service = MagicMock(spec=XService)
    service.ensure_initialized = AsyncMock()
    service.get_blocked_user_ids = AsyncMock(return_value={101, 102})
    service.get_follower_user_ids = AsyncMock(return_value={201, 202})
    service.unblock_user = AsyncMock(return_value="SUCCESS")
    service.unfollow_user = AsyncMock(return_value="SUCCESS")
    return service


@pytest.fixture
def mock_db_manager():
    db = MagicMock(spec=DatabaseManager)
    db.initialize_database = MagicMock()
    return db


@pytest.mark.asyncio
async def test_unblock_agent_dry_run(mock_x_service, mock_db_manager, caplog):
    # Setup DB to return some pending users
    mock_db_manager.get_all_blocked_users_count.return_value = 2
    mock_db_manager.get_pending_blocked_users.return_value = [101, 102]
    mock_db_manager.get_processed_users_count.return_value = 0

    agent = UnblockAgent(
        x_service=mock_x_service, db_manager=mock_db_manager, dry_run=True
    )

    with caplog.at_level(logging.INFO):
        await agent.execute()

    # Verify no unblock calls were made to the service
    mock_x_service.unblock_user.assert_not_called()

    # Verify log contains Dry Run messages
    assert "DRY RUN ENABLED" in caplog.text
    assert "[Dry Run] Would unblock 101" in caplog.text
    assert "[Dry Run] Would unblock 102" in caplog.text

    # Verify DB update was still called (as it tracks progress even in dry run in my current implementation)
    # Actually, should it update the DB in dry run?
    # In my implementation of UnblockAgent:
    # await asyncio.to_thread(self.db.update_user_statuses, uids, status if status != "SUCCESS" else "UNBLOCKED")
    # Yes, it does. This allows the user to see what would be updated.
    assert mock_db_manager.update_user_statuses.called


@pytest.mark.asyncio
async def test_unfollow_agent_dry_run(mock_x_service, mock_db_manager, caplog, capsys):
    # Setup DB: previous followers had 101, 201, 202. Current has 201, 202.
    # So 101 unfollowed.
    mock_db_manager.get_all_follower_ids.return_value = {101, 201, 202}
    mock_x_service.get_follower_user_ids.return_value = {201, 202}

    agent = UnfollowAgent(
        x_service=mock_x_service, db_manager=mock_db_manager, dry_run=True
    )

    with caplog.at_level(logging.INFO):
        await agent.execute()

    # Verify no destructive calls to DB
    mock_db_manager.replace_followers.assert_not_called()
    mock_db_manager.log_unfollows.assert_not_called()

    # Verify logs
    assert "[Dry Run] Would update follower list in database." in caplog.text
    assert "[Dry Run] Would log 1 unfollow events." in caplog.text

    # Verify stdout report
    captured = capsys.readouterr()
    assert "Unfollows:       1" in captured.out
