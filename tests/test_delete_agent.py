"""Comprehensive tests for DeleteAgent."""
import pytest
import json
import asyncio
from datetime import datetime, timezone, timedelta
from pathlib import Path
from unittest.mock import MagicMock, AsyncMock, patch
import tweepy

from x_agent.agents.delete_agent import DeleteAgent, MockStatus
from x_agent.services.x_service import XService
from x_agent.database import DatabaseManager


@pytest.fixture
def mock_x_service():
    """Create a mock XService instance."""
    service = MagicMock(spec=XService)
    service.ensure_initialized = AsyncMock()
    service.user_id = 12345
    service.pinned_tweet_id = None
    service.get_user_tweets_v1 = AsyncMock(return_value=[])
    service.delete_tweet = AsyncMock(return_value=True)
    return service


@pytest.fixture
def mock_db_manager():
    """Create a mock DatabaseManager instance."""
    db = MagicMock(spec=DatabaseManager)
    db.initialize_database = MagicMock()
    db.get_all_deleted_tweet_ids = MagicMock(return_value=set())
    db.log_deleted_tweet = MagicMock()
    return db


@pytest.fixture
def delete_agent(mock_x_service, mock_db_manager):
    """Create a DeleteAgent instance."""
    return DeleteAgent(
        x_service=mock_x_service,
        db_manager=mock_db_manager,
        dry_run=False,
    )


class TestDeleteAgentInit:
    """Tests for DeleteAgent initialization."""

    def test_init_defaults(self, mock_x_service, mock_db_manager):
        """Test default initialization."""
        agent = DeleteAgent(
            x_service=mock_x_service,
            db_manager=mock_db_manager,
        )
        assert agent.x_service == mock_x_service
        assert agent.db == mock_db_manager
        assert agent.dry_run is False
        assert agent.protected_ids == set()
        assert agent.archive_path is None
        assert agent.stats == {"deleted": 0, "skipped": 0, "errors": 0}
        assert agent.deleted_tweet_ids == set()

    def test_init_with_protected_ids(self, mock_x_service, mock_db_manager):
        """Test initialization with protected IDs."""
        protected = [1, 2, 3]
        agent = DeleteAgent(
            x_service=mock_x_service,
            db_manager=mock_db_manager,
            protected_ids=protected,
        )
        assert agent.protected_ids == {1, 2, 3}

    def test_init_with_archive_path(self, mock_x_service, mock_db_manager, tmp_path):
        """Test initialization with archive path."""
        archive_file = tmp_path / "tweets.js"
        archive_file.write_text("[]")
        
        agent = DeleteAgent(
            x_service=mock_x_service,
            db_manager=mock_db_manager,
            archive_path=str(archive_file),
        )
        assert agent.archive_path == archive_file

    def test_init_with_dry_run(self, mock_x_service, mock_db_manager):
        """Test initialization with dry run enabled."""
        agent = DeleteAgent(
            x_service=mock_x_service,
            db_manager=mock_db_manager,
            dry_run=True,
        )
        assert agent.dry_run is True


class TestDeleteAgentExecute:
    """Tests for DeleteAgent.execute method."""

    @pytest.mark.asyncio
    async def test_execute_with_archive(self, mock_x_service, mock_db_manager, tmp_path):
        """Test execute with archive file."""
        archive_file = tmp_path / "tweets.js"
        archive_content = 'window.YTD.tweets.part0 = [{"tweet": {"id": "123", "created_at": "Wed Oct 24 10:00:00 +0000 2018", "full_text": "Test", "favorite_count": "0", "retweet_count": "0"}}]'
        archive_file.write_text(archive_content)
        
        mock_db_manager.get_all_deleted_tweet_ids = MagicMock(return_value=set())
        
        agent = DeleteAgent(
            x_service=mock_x_service,
            db_manager=mock_db_manager,
            archive_path=str(archive_file),
            dry_run=True,
        )
        
        await agent.execute()
        
        mock_x_service.ensure_initialized.assert_called_once()
        mock_db_manager.initialize_database.assert_called_once()

    @pytest.mark.asyncio
    async def test_execute_without_archive_uses_live_api(self, mock_x_service, mock_db_manager):
        """Test execute without archive uses live API."""
        mock_db_manager.get_all_deleted_tweet_ids = MagicMock(return_value=set())
        mock_x_service.get_user_tweets_v1 = AsyncMock(return_value=[])
        
        agent = DeleteAgent(
            x_service=mock_x_service,
            db_manager=mock_db_manager,
            archive_path=None,
        )
        
        await agent.execute()
        
        mock_x_service.get_user_tweets_v1.assert_called_once()

    @pytest.mark.asyncio
    async def test_execute_with_pinned_tweet(self, mock_x_service, mock_db_manager, tmp_path):
        """Test execute adds pinned tweet to protected IDs."""
        mock_x_service.pinned_tweet_id = 999
        mock_db_manager.get_all_deleted_tweet_ids = MagicMock(return_value=set())
        
        archive_file = tmp_path / "tweets.js"
        archive_file.write_text('window.YTD.tweets.part0 = []')
        
        agent = DeleteAgent(
            x_service=mock_x_service,
            db_manager=mock_db_manager,
            archive_path=str(archive_file),
        )
        
        await agent.execute()
        
        assert 999 in agent.protected_ids


class TestProcessArchive:
    """Tests for _process_archive method."""

    @pytest.mark.asyncio
    async def test_process_archive_file_not_found(self, delete_agent, tmp_path, caplog):
        """Test _process_archive with non-existent file."""
        delete_agent.archive_path = tmp_path / "nonexistent.js"
        now = datetime.now(timezone.utc)
        
        with caplog.at_level("ERROR"):
            await delete_agent._process_archive(now)
        
        assert "Archive file not found" in caplog.text

    @pytest.mark.asyncio
    async def test_process_archive_invalid_json(self, delete_agent, tmp_path, caplog):
        """Test _process_archive with invalid JSON."""
        archive_file = tmp_path / "tweets.js"
        archive_file.write_text("invalid json")
        delete_agent.archive_path = archive_file
        
        now = datetime.now(timezone.utc)
        
        with caplog.at_level("ERROR"):
            await delete_agent._process_archive(now)
        
        assert "Failed to process archive" in caplog.text

    @pytest.mark.asyncio
    async def test_process_archive_empty(self, delete_agent, tmp_path):
        """Test _process_archive with empty array."""
        archive_file = tmp_path / "tweets.js"
        archive_file.write_text('window.YTD.tweets.part0 = []')
        delete_agent.archive_path = archive_file
        
        now = datetime.now(timezone.utc)
        await delete_agent._process_archive(now)
        
        # Should process without errors
        assert delete_agent.stats["deleted"] == 0

    @pytest.mark.asyncio
    async def test_process_archive_multiple_tweets(self, delete_agent, tmp_path):
        """Test _process_archive with multiple tweets."""
        archive_file = tmp_path / "tweets.js"
        tweets = [
            {"tweet": {"id": "1", "created_at": "Wed Oct 24 10:00:00 +0000 2018", "full_text": "Tweet 1", "favorite_count": "0", "retweet_count": "0"}},
            {"tweet": {"id": "2", "created_at": "Wed Oct 24 10:00:00 +0000 2018", "full_text": "Tweet 2", "favorite_count": "0", "retweet_count": "0"}},
        ]
        archive_file.write_text(f'window.YTD.tweets.part0 = {json.dumps(tweets)}')
        delete_agent.archive_path = archive_file
        delete_agent._process_tweet = AsyncMock()
        
        now = datetime.now(timezone.utc)
        await delete_agent._process_archive(now)
        
        # Should have processed 2 tweets
        assert delete_agent._process_tweet.call_count == 2


class TestProcessLiveAPI:
    """Tests for _process_live_api method."""

    @pytest.mark.asyncio
    async def test_process_live_api_empty_tweets(self, delete_agent):
        """Test _process_live_api with empty tweet list."""
        delete_agent.x_service.get_user_tweets_v1 = AsyncMock(return_value=[])
        
        now = datetime.now(timezone.utc)
        await delete_agent._process_live_api(now)
        
        # Should not process any tweets
        assert delete_agent.stats["deleted"] == 0

    @pytest.mark.asyncio
    async def test_process_live_api_with_tweets(self, delete_agent):
        """Test _process_live_api with tweets."""
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.created_at = datetime.now(timezone.utc) - timedelta(days=366)
        mock_tweet.favorite_count = 0
        mock_tweet.retweet_count = 0
        mock_tweet.in_reply_to_status_id = None
        mock_tweet.full_text = "Test tweet"
        mock_tweet.entities = {}
        mock_tweet.extended_entities = {}
        
        delete_agent.x_service.get_user_tweets_v1 = AsyncMock(return_value=[mock_tweet])
        delete_agent._process_tweet = AsyncMock()
        
        now = datetime.now(timezone.utc)
        await delete_agent._process_live_api(now)
        
        delete_agent._process_tweet.assert_called_once()

    @pytest.mark.asyncio
    async def test_process_live_api_unauthorized(self, delete_agent, caplog):
        """Test _process_live_api with unauthorized error."""
        mock_response = MagicMock()
        mock_response.status_code = 401
        delete_agent.x_service.get_user_tweets_v1 = AsyncMock(
            side_effect=tweepy.errors.Unauthorized(mock_response)
        )
        
        now = datetime.now(timezone.utc)
        
        with caplog.at_level("WARNING"):
            await delete_agent._process_live_api(now)
        
        assert "Reached API access limit" in caplog.text

    @pytest.mark.asyncio
    async def test_process_live_api_generic_error(self, delete_agent, caplog):
        """Test _process_live_api with generic error."""
        delete_agent.x_service.get_user_tweets_v1 = AsyncMock(
            side_effect=Exception("Network error")
        )
        
        now = datetime.now(timezone.utc)
        
        with caplog.at_level("ERROR"):
            await delete_agent._process_live_api(now)
        
        assert "Error fetching tweets" in caplog.text


class TestProcessTweet:
    """Tests for _process_tweet method."""

    @pytest.mark.asyncio
    async def test_process_tweet_already_deleted(self, delete_agent):
        """Test _process_tweet skips already deleted tweets."""
        delete_agent.deleted_tweet_ids = {123}
        
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        
        now = datetime.now(timezone.utc)
        await delete_agent._process_tweet(mock_tweet, now)
        
        assert delete_agent.stats["deleted"] == 1  # Counted as deleted

    @pytest.mark.asyncio
    async def test_process_tweet_protected_id(self, delete_agent, caplog):
        """Test _process_tweet skips protected IDs."""
        delete_agent.protected_ids = {123}
        
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.created_at = datetime.now(timezone.utc) - timedelta(days=366)
        mock_tweet.favorite_count = 0
        mock_tweet.retweet_count = 0
        mock_tweet.in_reply_to_status_id = None
        mock_tweet.full_text = "Test"
        mock_tweet.entities = {}
        mock_tweet.extended_entities = {}
        
        now = datetime.now(timezone.utc)
        
        with caplog.at_level("INFO"):
            await delete_agent._process_tweet(mock_tweet, now)
        
        assert "KEEP [Protected]" in caplog.text
        assert delete_agent.stats["skipped"] == 1

    @pytest.mark.asyncio
    async def test_process_tweet_grace_period(self, delete_agent, caplog):
        """Test _process_tweet skips recent tweets (grace period)."""
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.created_at = datetime.now(timezone.utc) - timedelta(days=3)
        mock_tweet.favorite_count = 0
        mock_tweet.retweet_count = 0
        mock_tweet.in_reply_to_status_id = None
        mock_tweet.full_text = "Test"
        mock_tweet.entities = {}
        mock_tweet.extended_entities = {}
        
        now = datetime.now(timezone.utc)
        
        with caplog.at_level("INFO"):
            await delete_agent._process_tweet(mock_tweet, now)
        
        assert "KEEP [Recent]" in caplog.text
        assert delete_agent.stats["skipped"] == 1

    @pytest.mark.asyncio
    async def test_process_tweet_old_retweet(self, delete_agent, caplog):
        """Test _process_tweet deletes old retweets."""
        delete_agent._delete_tweet = AsyncMock()
        
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.created_at = datetime.now(timezone.utc) - timedelta(days=45)
        mock_tweet.favorite_count = 0
        mock_tweet.retweet_count = 0
        mock_tweet.in_reply_to_status_id = None
        mock_tweet.full_text = "RT @user Test"
        mock_tweet.entities = {}
        mock_tweet.extended_entities = {}
        
        now = datetime.now(timezone.utc)
        await delete_agent._process_tweet(mock_tweet, now)
        
        delete_agent._delete_tweet.assert_called_once()
        call_args = delete_agent._delete_tweet.call_args
        assert "old retweet" in call_args[0][3]  # reason parameter

    @pytest.mark.asyncio
    async def test_process_tweet_thread(self, delete_agent, caplog):
        """Test _process_tweet keeps threads."""
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.created_at = datetime.now(timezone.utc) - timedelta(days=366)
        mock_tweet.favorite_count = 0
        mock_tweet.retweet_count = 0
        mock_tweet.in_reply_to_status_id = None
        mock_tweet.full_text = "This is a thread 1/5"
        mock_tweet.entities = {}
        mock_tweet.extended_entities = {}
        
        now = datetime.now(timezone.utc)
        
        with caplog.at_level("INFO"):
            await delete_agent._process_tweet(mock_tweet, now)
        
        assert "KEEP [Thread]" in caplog.text
        assert delete_agent.stats["skipped"] == 1

    @pytest.mark.asyncio
    async def test_process_tweet_with_media(self, delete_agent, caplog):
        """Test _process_tweet keeps tweets with media."""
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.created_at = datetime.now(timezone.utc) - timedelta(days=366)
        mock_tweet.favorite_count = 0
        mock_tweet.retweet_count = 0
        mock_tweet.in_reply_to_status_id = None
        mock_tweet.full_text = "Test"
        mock_tweet.entities = {"media": [{"id": 1}]}
        mock_tweet.extended_entities = {}
        
        now = datetime.now(timezone.utc)
        
        with caplog.at_level("INFO"):
            await delete_agent._process_tweet(mock_tweet, now)
        
        assert "KEEP [Media]" in caplog.text
        assert delete_agent.stats["skipped"] == 1

    @pytest.mark.asyncio
    async def test_process_tweet_critical_age(self, delete_agent, caplog):
        """Test _process_tweet deletes tweets older than 1 year."""
        delete_agent._delete_tweet = AsyncMock()
        
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.created_at = datetime.now(timezone.utc) - timedelta(days=366)
        mock_tweet.favorite_count = 0
        mock_tweet.retweet_count = 0
        mock_tweet.in_reply_to_status_id = None
        mock_tweet.full_text = "Old tweet"
        mock_tweet.entities = {}
        mock_tweet.extended_entities = {}
        
        now = datetime.now(timezone.utc)
        await delete_agent._process_tweet(mock_tweet, now)
        
        delete_agent._delete_tweet.assert_called_once()
        call_args = delete_agent._delete_tweet.call_args
        assert "older than 365 days" in call_args[0][3]  # reason parameter

    @pytest.mark.asyncio
    async def test_process_tweet_popular(self, delete_agent, caplog):
        """Test _process_tweet keeps popular tweets."""
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.created_at = datetime.now(timezone.utc) - timedelta(days=180)
        mock_tweet.favorite_count = 25  # Above POPULAR_TWEET_THRESHOLD (20)
        mock_tweet.retweet_count = 0
        mock_tweet.in_reply_to_status_id = None
        mock_tweet.full_text = "Popular tweet"
        mock_tweet.entities = {}
        mock_tweet.extended_entities = {}
        
        now = datetime.now(timezone.utc)
        
        with caplog.at_level("INFO"):
            await delete_agent._process_tweet(mock_tweet, now)
        
        assert "KEEP [Popular" in caplog.text
        assert delete_agent.stats["skipped"] == 1

    @pytest.mark.asyncio
    async def test_process_tweet_popular_reply(self, delete_agent, caplog):
        """Test _process_tweet keeps popular replies."""
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.created_at = datetime.now(timezone.utc) - timedelta(days=180)
        mock_tweet.favorite_count = 6  # Above POPULAR_REPLY_THRESHOLD (5)
        mock_tweet.retweet_count = 0
        mock_tweet.in_reply_to_status_id = 456  # This is a reply
        mock_tweet.full_text = "Popular reply"
        mock_tweet.entities = {}
        mock_tweet.extended_entities = {}
        
        now = datetime.now(timezone.utc)
        
        with caplog.at_level("INFO"):
            await delete_agent._process_tweet(mock_tweet, now)
        
        assert "KEEP [Pop-Reply" in caplog.text
        assert delete_agent.stats["skipped"] == 1

    @pytest.mark.asyncio
    async def test_process_tweet_low_engagement(self, delete_agent):
        """Test _process_tweet deletes low engagement tweets."""
        delete_agent._delete_tweet = AsyncMock()
        
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.created_at = datetime.now(timezone.utc) - timedelta(days=180)
        mock_tweet.favorite_count = 1  # Below POPULAR_TWEET_THRESHOLD (20)
        mock_tweet.retweet_count = 0
        mock_tweet.in_reply_to_status_id = None
        mock_tweet.full_text = "Low engagement tweet"
        mock_tweet.entities = {}
        mock_tweet.extended_entities = {}
        
        now = datetime.now(timezone.utc)
        await delete_agent._process_tweet(mock_tweet, now)
        
        delete_agent._delete_tweet.assert_called_once()
        call_args = delete_agent._delete_tweet.call_args
        assert "low engagement" in call_args[0][3]  # reason parameter


class TestDeleteTweet:
    """Tests for _delete_tweet method."""

    @pytest.mark.asyncio
    async def test_delete_tweet_dry_run(self, delete_agent, caplog):
        """Test _delete_tweet in dry run mode."""
        delete_agent.dry_run = True
        
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.full_text = "Test tweet"
        mock_tweet.created_at = datetime.now(timezone.utc)
        
        with caplog.at_level("INFO"):
            await delete_agent._delete_tweet(mock_tweet, 0, False, "test reason")
        
        assert "DELETE" in caplog.text
        assert delete_agent.stats["deleted"] == 1
        assert 123 not in delete_agent.deleted_tweet_ids

    @pytest.mark.asyncio
    async def test_delete_tweet_success(self, delete_agent):
        """Test _delete_tweet with successful deletion."""
        delete_agent.x_service.delete_tweet = AsyncMock(return_value=True)
        delete_agent.db.log_deleted_tweet = MagicMock()
        
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.full_text = "Test tweet"
        mock_tweet.created_at = datetime.now(timezone.utc)
        
        await delete_agent._delete_tweet(mock_tweet, 10, False, "test reason")
        
        delete_agent.x_service.delete_tweet.assert_called_once_with(123)
        delete_agent.db.log_deleted_tweet.assert_called_once()
        assert delete_agent.stats["deleted"] == 1
        assert 123 in delete_agent.deleted_tweet_ids

    @pytest.mark.asyncio
    async def test_delete_tweet_failure(self, delete_agent):
        """Test _delete_tweet with failed deletion."""
        delete_agent.x_service.delete_tweet = AsyncMock(return_value=False)
        
        mock_tweet = MagicMock()
        mock_tweet.id = 123
        mock_tweet.full_text = "Test tweet"
        mock_tweet.created_at = datetime.now(timezone.utc)
        
        await delete_agent._delete_tweet(mock_tweet, 0, False, "test reason")
        
        assert delete_agent.stats["errors"] == 1
        assert 123 not in delete_agent.deleted_tweet_ids


class TestGenerateReport:
    """Tests for _generate_report method."""

    def test_generate_report_empty(self, delete_agent):
        """Test _generate_report with no stats."""
        report = delete_agent._generate_report()
        
        assert "Delete Agent Report" in report
        assert "Tweets Processed: 0" in report
        assert "Tweets Deleted:   0" in report
        assert "Tweets Skipped:   0" in report
        assert "Errors:           0" in report

    def test_generate_report_with_stats(self, delete_agent):
        """Test _generate_report with stats."""
        delete_agent.stats = {"deleted": 5, "skipped": 10, "errors": 2}
        
        report = delete_agent._generate_report()
        
        assert "Tweets Processed: 17" in report
        assert "Tweets Deleted:   5" in report
        assert "Tweets Skipped:   10" in report
        assert "Errors:           2" in report


class TestMockStatus:
    """Tests for MockStatus class."""

    def test_mock_status_basic(self):
        """Test MockStatus with basic data."""
        data = {
            "id": "123",
            "favorite_count": "10",
            "retweet_count": "5",
            "full_text": "Test tweet",
        }
        created_at = datetime(2023, 1, 1, 12, 0, 0, tzinfo=timezone.utc)
        
        status = MockStatus(data, created_at)
        
        assert status.id == 123
        assert status.favorite_count == 10
        assert status.retweet_count == 5
        assert status.full_text == "Test tweet"
        assert status.text == "Test tweet"
        assert status.created_at == created_at

    def test_mock_status_with_entities(self):
        """Test MockStatus with entities."""
        data = {
            "id": "123",
            "favorite_count": "0",
            "retweet_count": "0",
            "full_text": "Test",
            "entities": {"urls": [{"url": "http://example.com"}]},
            "extended_entities": {"media": [{"id": 1}]},
            "in_reply_to_status_id": "456",
        }
        created_at = datetime(2023, 1, 1, 12, 0, 0, tzinfo=timezone.utc)
        
        status = MockStatus(data, created_at)
        
        assert status.entities == {"urls": [{"url": "http://example.com"}]}
        assert status.extended_entities == {"media": [{"id": 1}]}
        assert status.in_reply_to_status_id == "456"

    def test_mock_status_missing_fields(self):
        """Test MockStatus with missing optional fields."""
        data = {"id": "123"}
        created_at = datetime(2023, 1, 1, 12, 0, 0, tzinfo=timezone.utc)
        
        status = MockStatus(data, created_at)
        
        assert status.id == 123
        assert status.favorite_count == 0
        assert status.retweet_count == 0
        assert status.full_text == ""
        assert status.text == ""
        assert status.entities == {}
        assert status.extended_entities == {}
        assert status.in_reply_to_status_id is None
