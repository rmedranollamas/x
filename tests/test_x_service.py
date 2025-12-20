import os
import time
import pytest
import tweepy
from unittest.mock import MagicMock, patch
from src.x_agent.services.x_service import XService


@pytest.fixture
def mock_tweepy_client_v2():
    """Mocks tweepy.Client for v2 API interactions."""
    mock_client = MagicMock()  # Removed spec to allow session mocking
    mock_client.get_me.return_value = MagicMock(data=MagicMock(id="12345"))
    # Ensure request is mockable
    mock_client.request = MagicMock()
    mock_client.session = MagicMock()
    return mock_client


@pytest.fixture
def mock_tweepy_api_v1():
    """Mocks tweepy.API for v1.1 API interactions."""
    mock_api = MagicMock(spec=tweepy.API)
    return mock_api


@pytest.fixture(autouse=True)
def mock_env_vars():
    """Mocks environment variables for API credentials."""
    with patch.dict(
        os.environ,
        {
            "X_API_KEY": "test_api_key",
            "X_API_KEY_SECRET": "test_api_key_secret",
            "X_ACCESS_TOKEN": "test_access_token",
            "X_ACCESS_TOKEN_SECRET": "test_access_token_secret",
        },
    ):
        yield


@pytest.fixture
def x_service(mock_tweepy_client_v2, mock_tweepy_api_v1):
    """Fixture for XService with mocked Tweepy clients."""
    with patch(
        "src.x_agent.services.x_service.tweepy.Client",
        return_value=mock_tweepy_client_v2,
    ):
        with patch(
            "src.x_agent.services.x_service.tweepy.API", return_value=mock_tweepy_api_v1
        ):
            service = XService()
            service.api_v1 = mock_tweepy_api_v1
            service.client_v2 = mock_tweepy_client_v2
            service.authenticated_user_id = 12345
            yield service


def test_x_service_initialization(x_service, mock_tweepy_client_v2, mock_tweepy_api_v1):
    """Test XService initializes correctly with mocked clients."""
    assert x_service.api_v1 == mock_tweepy_api_v1
    assert x_service.client_v2 == mock_tweepy_client_v2
    assert x_service.authenticated_user_id == 12345
    mock_tweepy_client_v2.get_me.assert_called_once()


def test_countdown(x_service, caplog):
    """Test countdown waits and logs message."""
    with patch("time.sleep") as mock_sleep:
        with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
            x_service._countdown(1, "Test message")
            mock_sleep.assert_called_once_with(1)
            assert "Test message" in caplog.text


def test_countdown_zero_seconds(x_service, caplog):
    """Test countdown does not wait for zero seconds."""
    with patch("time.sleep") as mock_sleep:
        with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
            x_service._countdown(0, "Test message")
            mock_sleep.assert_not_called()
            assert "Test message" not in caplog.text


def test_handle_rate_limit_with_reset_time(x_service, caplog):
    """Test rate limit handling with a valid reset time."""
    mock_response = MagicMock()
    mock_response.headers.get.return_value = str(int(time.time()) + 60)
    mock_error = MagicMock(spec=tweepy.errors.TooManyRequests, response=mock_response)

    with patch("time.sleep") as mock_sleep:
        with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
            x_service._handle_rate_limit(mock_error)
            mock_sleep.assert_called_once_with(pytest.approx(60, abs=2))
            assert "Rate limit reached. Waiting for ~1 minutes." in caplog.text


def test_handle_rate_limit_unknown_reset_time(x_service, caplog):
    """Test rate limit handling with unknown reset time (fallback)."""
    mock_response = MagicMock()
    mock_response.headers.get.return_value = None
    mock_error = MagicMock(spec=tweepy.errors.TooManyRequests, response=mock_response)

    with patch("time.sleep") as mock_sleep:
        with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
            x_service._handle_rate_limit(mock_error)
            mock_sleep.assert_called_once_with(15 * 60)
            assert "Rate limit reached, but reset time is unknown." in caplog.text


def test_get_blocked_user_ids_success_v1(x_service, caplog):
    """Test fetching blocked user IDs successfully using V1.1 Cursor."""
    # V1.1 get_blocked_ids returns a cursor of IDs (integers)
    # Cursor().pages() yields lists of IDs
    page1 = [101, 102]
    page2 = [103]
    page3 = []

    with patch("src.x_agent.services.x_service.tweepy.Cursor") as MockCursor:
        mock_cursor_instance = MockCursor.return_value
        mock_cursor_instance.pages.return_value = [page1, page2, page3]

        with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
            blocked_ids = x_service.get_blocked_user_ids()
            assert blocked_ids == [101, 102, 103]
            MockCursor.assert_called_once_with(x_service.api_v1.get_blocked_ids)
            assert "Found 3 blocked account IDs..." in caplog.text
            assert (
                "Finished fetching. Found a total of 3 blocked account IDs."
                in caplog.text
            )


def test_get_blocked_user_ids_unexpected_error_v1(x_service, caplog):
    """Test V1.1 fetching handles unexpected errors."""
    with patch("src.x_agent.services.x_service.tweepy.Cursor") as MockCursor:
        mock_cursor_instance = MockCursor.return_value
        mock_cursor_instance.pages.side_effect = Exception("Network error")

        with pytest.raises(SystemExit) as excinfo:
            with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
                x_service.get_blocked_user_ids()
                assert "An unexpected error occurred while fetching" in caplog.text
        assert excinfo.value.code == 1


def test_unblock_user_success(x_service):
    """Test unblocking a user successfully using V1.1 (Tweepy)."""
    # Arrange
    x_service.api_v1.destroy_block.return_value = MagicMock()

    # Act
    result = x_service.unblock_user(123)

    # Assert
    x_service.api_v1.destroy_block.assert_called_once_with(user_id=123)
    assert result is True


def test_unblock_user_not_found(x_service, caplog):
    """Test unblocking a user that is truly not found (Ghost Block)."""
    # Arrange
    # Simulate 404 Not Found from Tweepy on unblock
    mock_response = MagicMock()
    mock_response.status_code = 404
    x_service.api_v1.destroy_block.side_effect = tweepy.errors.NotFound(mock_response)
    
    # Simulate 404 from get_user (Confirming user is missing)
    x_service.api_v1.get_user.side_effect = tweepy.errors.NotFound(mock_response)

    # Act
    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        result = x_service.unblock_user(123)

    # Assert
    assert result == "NOT_FOUND"
    assert "User ID 123 not found (404) on V1" in caplog.text
    assert "confirmed missing" in caplog.text
    assert "Skipping (Ghost Block)." in caplog.text


def test_unblock_user_zombie_block(x_service, caplog):
    """Test unblocking a 'Zombie' user (Active but V1 unblock fails)."""
    # Arrange
    mock_response_404 = MagicMock()
    mock_response_404.status_code = 404
    
    # 1. V1 Unblock fails with 404
    x_service.api_v1.destroy_block.side_effect = tweepy.errors.NotFound(mock_response_404)
    
    # 2. V1 get_user succeeds (User exists)
    x_service.api_v1.get_user.return_value = MagicMock(id=123)
    
    # 3. V2 Unblock raw request setup
    # x_service uses client_v2.request.
    # We mock it to return success or raise error.
    # Let's assume it succeeds.
    x_service.client_v2.request.return_value = MagicMock()

    # Act
    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        result = x_service.unblock_user(123)

    # Assert
    assert result is True
    assert "User ID 123 not found (404) on V1" in caplog.text
    assert "User ID 123 EXISTS. Attempting V2 Unblock" in caplog.text
    assert "V2 Unblock raw request successful" in caplog.text
    x_service.client_v2.request.assert_called_once()


def test_unblock_user_zombie_block_v2_fail_404(x_service, caplog):
    """Test unblocking a 'Zombie' user where V2 ALSO fails with 404."""
    # Arrange
    mock_response_404 = MagicMock()
    mock_response_404.status_code = 404
    
    # 1. V1 Unblock fails with 404
    x_service.api_v1.destroy_block.side_effect = tweepy.errors.NotFound(mock_response_404)
    
    # 2. V1 get_user succeeds (User exists)
    x_service.api_v1.get_user.return_value = MagicMock(id=123)
    
    # 3. V2 Unblock fails with 404
    x_service.client_v2.request.side_effect = tweepy.errors.NotFound(mock_response_404)

    # Act
    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        result = x_service.unblock_user(123)

    # Assert
    assert result == "NOT_FOUND" # Should mark as handled (skipped)
    assert "V2 Unblock ALSO returned 404" in caplog.text
    assert "Unblock impossible. Skipping." in caplog.text


def test_unblock_user_suspended_is_ghost(x_service, caplog):
    """Test that suspended users (403) during existence check are treated as ghosts."""
    # Arrange
    mock_response_404 = MagicMock()
    mock_response_404.status_code = 404
    mock_response_403 = MagicMock()
    mock_response_403.status_code = 403
    
    # 1. V1 Unblock fails with 404
    x_service.api_v1.destroy_block.side_effect = tweepy.errors.NotFound(mock_response_404)
    
    # 2. V1 get_user fails with 403 Forbidden (Suspended)
    x_service.api_v1.get_user.side_effect = tweepy.errors.Forbidden(mock_response_403)

    # Act
    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        result = x_service.unblock_user(123)

    # Assert
    assert result == "NOT_FOUND" # Should treat as ghost
    assert "is suspended (Forbidden). Treating as Ghost Block." in caplog.text


def test_unblock_user_generic_error(x_service, caplog):
    """Test unblocking a user handles generic errors (V1.1 Tweepy)."""
    # Arrange
    x_service.api_v1.destroy_block.side_effect = Exception("Generic API error")

    # Act
    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        result = x_service.unblock_user(123)

    # Assert
    assert result is None
    assert "Could not unblock user ID 123. Exception: Generic API error" in caplog.text
