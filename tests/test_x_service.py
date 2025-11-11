import os
import time
import pytest
import tweepy
from unittest.mock import MagicMock, patch
from src.x_agent.services.x_service import XService


@pytest.fixture
def mock_tweepy_client_v2():
    """Mocks tweepy.Client for v2 API interactions."""
    mock_client = MagicMock(spec=tweepy.Client)
    mock_client.get_me.return_value = MagicMock(data=MagicMock(id="12345"))
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
            service.api_v1 = mock_tweepy_api_v1  # Ensure the fixture's mock is used
            service.client_v2 = (
                mock_tweepy_client_v2  # Ensure the fixture's mock is used
            )
            yield service


def test_x_service_initialization(x_service, mock_tweepy_client_v2, mock_tweepy_api_v1):
    """Test XService initializes correctly with mocked clients."""
    assert x_service.api_v1 == mock_tweepy_api_v1
    assert x_service.client_v2 == mock_tweepy_client_v2
    mock_tweepy_client_v2.get_me.assert_called_once()


def test_x_service_initialization_missing_credentials(mock_env_vars):
    """Test XService initialization fails with missing credentials."""
    with patch.dict(os.environ, {"X_API_KEY": ""}):  # Simulate missing key
        with pytest.raises(SystemExit) as excinfo:
            XService()
        assert excinfo.value.code == 1


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
            assert (
                "Test message" not in caplog.text
            )  # Message should not be logged if no wait


def test_handle_rate_limit_with_reset_time(x_service, caplog):
    """Test rate limit handling with a valid reset time."""
    mock_response = MagicMock()
    mock_response.headers.get.return_value = str(
        int(time.time()) + 60
    )  # 60 seconds from now
    mock_error = MagicMock(spec=tweepy.errors.TooManyRequests, response=mock_response)

    with patch("time.sleep") as mock_sleep:
        with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
            x_service._handle_rate_limit(mock_error)
            mock_sleep.assert_called_once_with(
                pytest.approx(60, abs=2)
            )  # Allow for slight time difference
            assert "Rate limit reached. Waiting for ~1 minutes." in caplog.text


def test_handle_rate_limit_unknown_reset_time(x_service, caplog):
    """Test rate limit handling with unknown reset time (fallback)."""
    mock_response = MagicMock()
    mock_response.headers.get.return_value = None  # Simulate unknown reset time
    mock_error = MagicMock(spec=tweepy.errors.TooManyRequests, response=mock_response)

    with patch("time.sleep") as mock_sleep:
        with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
            x_service._handle_rate_limit(mock_error)
            mock_sleep.assert_called_once_with(15 * 60)
            assert "Rate limit reached, but reset time is unknown." in caplog.text


def test_get_blocked_user_ids_success(x_service, mock_tweepy_api_v1, caplog):
    """Test fetching blocked user IDs successfully."""
    mock_tweepy_api_v1.get_blocked_ids.side_effect = [
        ([101, 102], (None, 1)),  # First call, returns some IDs and a next_cursor
        ([103], (None, 0)),  # Second call, returns more IDs and cursor 0 (end)
    ]

    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        blocked_ids = x_service.get_blocked_user_ids()
        assert blocked_ids == {101, 102, 103}
        assert mock_tweepy_api_v1.get_blocked_ids.call_count == 2
        assert (
            "Finished fetching. Found a total of 3 blocked account IDs." in caplog.text
        )


def test_get_blocked_user_ids_unexpected_error(x_service, mock_tweepy_api_v1, caplog):
    """Test fetching blocked user IDs handles unexpected errors."""
    mock_tweepy_api_v1.get_blocked_ids.side_effect = Exception("Network error")

    with pytest.raises(SystemExit) as excinfo:
        with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
            x_service.get_blocked_user_ids()
            assert "An unexpected error occurred while fetching" in caplog.text
    assert excinfo.value.code == 1


def test_unblock_user_success(x_service, mock_tweepy_api_v1):
    """Test unblocking a user successfully."""
    mock_user = MagicMock(spec=tweepy.User, screen_name="testuser")
    mock_tweepy_api_v1.destroy_block.return_value = mock_user

    result = x_service.unblock_user(123)
    mock_tweepy_api_v1.destroy_block.assert_called_once_with(user_id=123)
    assert result == mock_user


def test_unblock_user_not_found(x_service, mock_tweepy_api_v1, caplog):
    """Test unblocking a user that is not found."""
    mock_response_404 = MagicMock()
    mock_response_404.status_code = 404
    mock_response_404.headers = {}
    not_found_exception = tweepy.errors.NotFound(mock_response_404)

    mock_tweepy_api_v1.destroy_block.side_effect = not_found_exception

    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        result = x_service.unblock_user(123)
        assert result == "NOT_FOUND"
        assert "User ID 123 not found." in caplog.text


def test_unblock_user_rate_limit(x_service, mock_tweepy_api_v1, caplog):
    """Test unblocking a user handles rate limits and retries."""
    mock_response = MagicMock()
    mock_response.status_code = 429  # Too Many Requests
    mock_response.headers = {
        "x-rate-limit-reset": str(int(time.time()) + 1)
    }  # 1 second from now
    rate_limit_exception = tweepy.errors.TooManyRequests(mock_response)

    mock_user = MagicMock(spec=tweepy.User, screen_name="testuser")
    mock_tweepy_api_v1.destroy_block.side_effect = [
        rate_limit_exception,  # First call raises rate limit exception
        mock_user,  # Second call succeeds after wait
    ]

    with patch("time.sleep") as mock_sleep:
        with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
            result = x_service.unblock_user(123)
            assert result == mock_user
            assert mock_tweepy_api_v1.destroy_block.call_count == 2
            mock_sleep.assert_called_once()
            assert "Rate limit reached." in caplog.text


def test_unblock_user_generic_error(x_service, mock_tweepy_api_v1, caplog):
    """Test unblocking a user handles generic errors."""
    mock_tweepy_api_v1.destroy_block.side_effect = Exception("Generic API error")

    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        result = x_service.unblock_user(123)
        assert result is None
        assert "Could not unblock user ID 123. Reason: Generic API error" in caplog.text
