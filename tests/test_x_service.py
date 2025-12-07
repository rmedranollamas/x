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
    # Ensure request is mockable
    mock_client.request = MagicMock()
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
            service.authenticated_user_id = 12345
            yield service


def test_x_service_initialization(x_service, mock_tweepy_client_v2, mock_tweepy_api_v1):
    """Test XService initializes correctly with mocked clients."""
    assert x_service.api_v1 == mock_tweepy_api_v1
    assert x_service.client_v2 == mock_tweepy_client_v2
    assert x_service.authenticated_user_id == 12345
    mock_tweepy_client_v2.get_me.assert_called_once()


# ... existing tests for countdown and rate limit ...


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


def test_get_blocked_user_ids_success_v2(x_service, caplog):
    """Test fetching blocked user IDs successfully using V2 Paginator."""

    # Mock Response objects with data attribute containing User objects
    mock_user1 = MagicMock(id=101)
    mock_user2 = MagicMock(id=102)
    mock_user3 = MagicMock(id=103)

    response1 = MagicMock()
    response1.data = [mock_user1, mock_user2]

    response2 = MagicMock()
    response2.data = [mock_user3]

    response3 = MagicMock()  # Empty/Last response
    response3.data = []

    # Paginator is instantiated, then iterated over.
    # We mock the class so that the instance returned is iterable.
    with patch("src.x_agent.services.x_service.tweepy.Paginator") as MockPaginator:
        mock_paginator_instance = MockPaginator.return_value
        # Make the instance iterable yielding our responses
        mock_paginator_instance.__iter__.return_value = iter(
            [response1, response2, response3]
        )

        with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
            blocked_ids = x_service.get_blocked_user_ids()

            assert blocked_ids == [101, 102, 103]
            MockPaginator.assert_called_once_with(
                x_service.client_v2.get_blocked, max_results=1000, user_auth=True
            )
            assert "Found 3 blocked account IDs..." in caplog.text
            assert (
                "Finished fetching. Found a total of 3 blocked account IDs."
                in caplog.text
            )


def test_get_blocked_user_ids_unexpected_error_v2(x_service, caplog):
    """Test V2 fetching handles unexpected errors."""

    with patch("src.x_agent.services.x_service.tweepy.Paginator") as MockPaginator:
        mock_paginator_instance = MockPaginator.return_value
        mock_paginator_instance.__iter__.side_effect = Exception("Network error")

        with pytest.raises(SystemExit) as excinfo:
            with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
                x_service.get_blocked_user_ids()
                assert "An unexpected error occurred while fetching" in caplog.text
        assert excinfo.value.code == 1


def test_unblock_user_success(x_service, mock_tweepy_client_v2):
    """Test unblocking a user successfully using V2."""
    mock_response = MagicMock()
    mock_response.status_code = 200
    mock_response.errors = []

    # Mock session.delete
    mock_tweepy_client_v2.session = MagicMock()
    mock_tweepy_client_v2.session.delete.return_value = mock_response

    result = x_service.unblock_user(123)
    mock_tweepy_client_v2.session.delete.assert_called_once_with(
        "https://api.twitter.com/2/users/12345/blocking/123"
    )
    assert result is True


def test_unblock_user_not_found(x_service, mock_tweepy_client_v2, caplog):
    """Test unblocking a user that is not found (V2)."""
    mock_response = MagicMock()
    mock_response.status_code = 404
    mock_response.headers = {"x-access-level": "read-write"}
    mock_response.text = '{"errors":[{"detail":"User not found"}]}'

    mock_tweepy_client_v2.session = MagicMock()
    mock_tweepy_client_v2.session.delete.return_value = mock_response

    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        result = x_service.unblock_user(123)
        assert result == "NOT_FOUND"
        # Note: URL matches the one constructed in code
        assert (
            'User ID 123 not found or not blocked (404). URL: https://api.twitter.com/2/users/12345/blocking/123. Access Level: read-write. Response: {"errors":[{"detail":"User not found"}]}. Skipping.'
            in caplog.text
        )


def test_unblock_user_generic_error(x_service, mock_tweepy_client_v2, caplog):
    """Test unblocking a user handles generic errors (V2)."""
    mock_tweepy_client_v2.session = MagicMock()
    mock_tweepy_client_v2.session.delete.side_effect = Exception("Generic API error")

    with caplog.at_level(os.environ.get("LOG_LEVEL", "INFO")):
        result = x_service.unblock_user(123)
        assert result is None
        assert (
            "Could not unblock user ID 123. Exception: Generic API error" in caplog.text
        )
