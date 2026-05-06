import pytest
import json
from datetime import datetime, timezone
from unittest.mock import MagicMock, AsyncMock
from pathlib import Path
from x_agent.agents.delete_agent import DeleteAgent, MockStatus
from x_agent.services.x_service import XService
from x_agent.database import DatabaseManager

@pytest.fixture
def mock_x_service():
    service = MagicMock(spec=XService)
    service.ensure_initialized = AsyncMock()
    service.user_id = 12345
    service.pinned_tweet_id = None
    return service

@pytest.fixture
def mock_db_manager():
    db = MagicMock(spec=DatabaseManager)
    db.initialize_database = MagicMock()
    db.is_tweet_deleted = MagicMock(return_value=False)
    return db

@pytest.mark.asyncio
async def test_process_archive_creates_mock_status(mock_x_service, mock_db_manager, tmp_path):
    # Create a dummy tweets.js file
    archive_content = 'window.YTD.tweets.part0 = [{"tweet": {"id": "123456789", "created_at": "Wed Oct 24 10:00:00 +0000 2018", "full_text": "Hello world", "favorite_count": "10", "retweet_count": "5"}}]'
    archive_file = tmp_path / "tweets.js"
    archive_file.write_text(archive_content)

    agent = DeleteAgent(
        x_service=mock_x_service,
        db_manager=mock_db_manager,
        dry_run=True,
        archive_path=str(archive_file)
    )

    # We want to intercept _process_tweet to verify the tweet object passed to it
    agent._process_tweet = AsyncMock()

    now = datetime.now(timezone.utc)
    await agent._process_archive(now)

    # Verify _process_tweet was called with a MockStatus object
    agent._process_tweet.assert_called_once()
    tweet = agent._process_tweet.call_args[0][0]

    assert isinstance(tweet, MockStatus)
    assert tweet.id == 123456789
    assert tweet.full_text == "Hello world"
    assert tweet.favorite_count == 10
    assert tweet.retweet_count == 5
    assert tweet.created_at == datetime(2018, 10, 24, 10, 0, 0, tzinfo=timezone.utc)

def test_mock_status_initialization():
    data = {
        "id": "987654321",
        "favorite_count": "42",
        "retweet_count": "7",
        "full_text": "Unit test tweet",
        "entities": {"hashtags": []}
    }
    created_at = datetime(2023, 1, 1, 12, 0, 0, tzinfo=timezone.utc)

    status = MockStatus(data, created_at)

    assert status.id == 987654321
    assert status.created_at == created_at
    assert status.favorite_count == 42
    assert status.retweet_count == 7
    assert status.full_text == "Unit test tweet"
    assert status.text == "Unit test tweet"
    assert status.entities == {"hashtags": []}
    assert status.extended_entities == {}
    assert status.in_reply_to_status_id is None
