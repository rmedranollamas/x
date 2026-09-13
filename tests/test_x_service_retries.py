import pytest
import tweepy
from unittest.mock import MagicMock, AsyncMock, patch
from x_agent.services.x_service import XService, is_transient_error


def test_is_transient_error():
    # 5xx errors should be transient (v1 status_code)
    mock_resp = MagicMock()
    mock_resp.status_code = 500
    mock_resp.json.return_value = {}
    err = tweepy.errors.HTTPException(mock_resp)
    assert is_transient_error(err) is True

    mock_resp.status_code = 503
    err = tweepy.errors.HTTPException(mock_resp)
    assert is_transient_error(err) is True

    # 5xx errors (v2 status attribute)
    mock_resp_v2 = MagicMock(spec=["status", "json", "reason", "text"])
    mock_resp_v2.status = 502
    mock_resp_v2.json.return_value = {}
    err_v2 = tweepy.errors.HTTPException(mock_resp_v2)
    assert is_transient_error(err_v2) is True

    # 4xx errors (except maybe 429 which is handled by Tweepy) are usually NOT transient
    mock_resp.status_code = 404
    err = tweepy.errors.HTTPException(mock_resp)
    assert is_transient_error(err) is False

    # Connection and Timeout errors wrapped in TweepyException
    err = tweepy.errors.TweepyException("Connection timed out")
    assert is_transient_error(err) is True

    err_timeout = tweepy.errors.TweepyException("Request Timeout occurred")
    assert is_transient_error(err_timeout) is True

    # TweepyException without 5xx or connection/timeout message
    err_other_tweepy = tweepy.errors.TweepyException("Invalid user ID")
    assert is_transient_error(err_other_tweepy) is False

    # AttributeError handling for tweepy async client side-effect
    attr_err_transient = AttributeError("'NoneType' object has no attribute 'items'")
    assert is_transient_error(attr_err_transient) is True

    attr_err_other = AttributeError("'NoneType' object has no attribute 'foo'")
    assert is_transient_error(attr_err_other) is False

    # Generic Exception fallback return False
    assert is_transient_error(Exception("Foo")) is False
    assert is_transient_error(Exception("Connection timed out")) is False
    assert is_transient_error(ValueError("Invalid value")) is False


@pytest.mark.asyncio
async def test_x_service_retry_logic():
    """Verify that XService methods actually retry on transient errors."""

    # We need to patch the retry decorator parameters to make it fast
    # OR we can just mock the underlying api_v1 call to fail then succeed.

    with (
        patch("x_agent.services.x_service.settings"),
        patch("x_agent.services.x_service.tweepy.asynchronous.AsyncClient"),
        patch("x_agent.services.x_service.tweepy.OAuth1UserHandler"),
        patch("x_agent.services.x_service.tweepy.API") as mock_api_cls,
    ):
        mock_api = mock_api_cls.return_value
        service = XService()

        # Mock get_blocked_ids to fail twice then succeed
        mock_resp = MagicMock()
        mock_resp.status_code = 500
        transient_err = tweepy.errors.HTTPException(mock_resp)

        mock_api.get_blocked_ids.side_effect = [
            transient_err,
            transient_err,
            ([123], (None, 0)),
        ]

        # We need to speed up tenacity for this test
        # Tenacity decorators are applied at definition time.
        # This makes it hard to change them.
        # But we can patch asyncio.sleep to return immediately.

        with patch("asyncio.sleep", AsyncMock()) as mock_sleep:
            ids = await service.get_blocked_user_ids()

            assert ids == {123}
            assert mock_api.get_blocked_ids.call_count == 3
            assert mock_sleep.call_count == 2


@pytest.mark.asyncio
async def test_x_service_retry_exhaustion():
    """Verify that XService eventually gives up and re-raises."""
    with (
        patch("x_agent.services.x_service.settings"),
        patch("x_agent.services.x_service.tweepy.asynchronous.AsyncClient"),
        patch("x_agent.services.x_service.tweepy.OAuth1UserHandler"),
        patch("x_agent.services.x_service.tweepy.API") as mock_api_cls,
    ):
        mock_api = mock_api_cls.return_value
        service = XService()

        mock_resp = MagicMock()
        mock_resp.status_code = 500
        transient_err = tweepy.errors.HTTPException(mock_resp)

        mock_api.get_blocked_ids.side_effect = transient_err

        with patch("asyncio.sleep", AsyncMock()):
            with pytest.raises(tweepy.errors.HTTPException):
                await service.get_blocked_user_ids()

            # Should have tried 3 times (default in my code)
            assert mock_api.get_blocked_ids.call_count == 3
