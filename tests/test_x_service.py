import pytest
import tweepy.asynchronous
import tweepy
from unittest.mock import MagicMock, AsyncMock, patch
from src.x_agent.services.x_service import XService


@pytest.fixture(autouse=True)
def mock_env_vars():
    """Mocks the settings object."""
    with patch("src.x_agent.services.x_service.settings") as mock_settings:
        mock_settings.x_api_key = "test_api_key"
        mock_settings.x_api_key_secret = "test_api_key_secret"
        mock_settings.x_access_token = "test_access_token"
        mock_settings.x_access_token_secret = "test_access_token_secret"
        yield mock_settings


@pytest.fixture
def mock_async_client():
    """Mocks tweepy.asynchronous.AsyncClient."""
    client = MagicMock(spec=tweepy.asynchronous.AsyncClient)
    client.get_me = AsyncMock(
        return_value=MagicMock(data=MagicMock(id=12345, username="testuser"))
    )
    return client


@pytest.fixture
def mock_api_v1():
    """Mocks tweepy.API."""
    api = MagicMock(spec=tweepy.API)
    api.get_blocked_ids.return_value = ([], (None, 0))
    api.destroy_block = MagicMock()
    return api


@pytest.fixture
def x_service(mock_async_client, mock_api_v1):
    """Fixture for XService with mocked AsyncClient and API v1.1."""
    with (
        patch(
            "src.x_agent.services.x_service.tweepy.asynchronous.AsyncClient",
            return_value=mock_async_client,
        ),
        patch(
            "src.x_agent.services.x_service.tweepy.OAuth1UserHandler",
            return_value=MagicMock(),
        ),
        patch(
            "src.x_agent.services.x_service.tweepy.API",
            return_value=mock_api_v1,
        ),
    ):
        service = XService()
        yield service


@pytest.mark.asyncio
async def test_initialize(x_service, mock_async_client):
    """Test initialize authenticates and sets user_id."""
    await x_service.initialize()

    mock_async_client.get_me.assert_awaited_once()
    assert x_service.user_id == 12345


@pytest.mark.asyncio
async def test_get_blocked_user_ids(x_service, mock_api_v1):
    """Test fetching blocked IDs using v1.1 API."""
    mock_api_v1.get_blocked_ids.side_effect = [
        ([101, 102], (None, 0)),
    ]

    ids = await x_service.get_blocked_user_ids()

    assert ids == {101, 102}
    mock_api_v1.get_blocked_ids.assert_called_once_with(cursor=-1)


@pytest.mark.asyncio
async def test_unblock_user_success(x_service, mock_api_v1):
    """Test unblock_user success."""
    result = await x_service.unblock_user(999)

    mock_api_v1.destroy_block.assert_called_once_with(user_id=999)
    assert result == "SUCCESS"


@pytest.mark.asyncio
async def test_unblock_user_not_found(x_service, mock_api_v1):
    """Test unblock_user returns NOT_FOUND on 404."""
    mock_response = MagicMock()
    mock_response.status_code = 404
    mock_api_v1.destroy_block.side_effect = tweepy.errors.NotFound(mock_response)

    result = await x_service.unblock_user(999)

    assert result == "NOT_FOUND"


@pytest.mark.asyncio
async def test_unblock_user_failure(x_service, mock_api_v1):
    """Test unblock_user returns FAILED on generic error."""
    mock_api_v1.destroy_block.side_effect = Exception("Generic error")

    result = await x_service.unblock_user(999)

    assert result == "FAILED"
