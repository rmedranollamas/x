import pytest
import tweepy.asynchronous
import tweepy
from unittest.mock import MagicMock, AsyncMock, patch
from x_agent.services.x_service import XService


@pytest.fixture(autouse=True)
def mock_env_vars():
    """Mocks the settings object."""
    with patch("x_agent.services.x_service.settings") as mock_settings:
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
    client.request = AsyncMock()
    # Mocking session for close()
    client.session = MagicMock()
    client.session.close = AsyncMock()
    return client


@pytest.fixture
def mock_api_v1():
    """Mocks tweepy.API."""
    api = MagicMock(spec=tweepy.API)
    api.get_blocked_ids.return_value = ([], (None, 0))
    api.destroy_block = MagicMock()
    api.get_user = MagicMock()
    api.create_block = MagicMock()
    api.get_friend_ids.return_value = ([], (None, 0))
    api.get_follower_ids.return_value = ([], (None, 0))
    api.destroy_friendship = MagicMock()
    return api


@pytest.fixture
def x_service(mock_async_client, mock_api_v1):
    """Fixture for XService with mocked AsyncClient and API v1.1."""
    with (
        patch(
            "x_agent.services.x_service.tweepy.asynchronous.AsyncClient",
            return_value=mock_async_client,
        ),
        patch(
            "x_agent.services.x_service.tweepy.OAuth1UserHandler",
            return_value=MagicMock(),
        ),
        patch(
            "x_agent.services.x_service.tweepy.API",
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
    """Test unblock_user returns NOT_FOUND on 404 if user doesn't exist."""
    mock_api_v1.destroy_block.side_effect = tweepy.errors.NotFound(MagicMock())
    mock_api_v1.get_user.side_effect = tweepy.errors.NotFound(MagicMock())

    result = await x_service.unblock_user(999)

    assert result == "NOT_FOUND"


@pytest.mark.asyncio
async def test_unblock_user_zombie_fixed_v2(x_service, mock_api_v1, mock_async_client):
    """Test unblock_user handles Zombie Block using V2 fallback."""
    x_service.user_id = 12345
    mock_api_v1.destroy_block.side_effect = [
        tweepy.errors.NotFound(MagicMock()),  # Initial try
    ]
    mock_api_v1.get_user.return_value = MagicMock()  # User exists
    mock_async_client.request.return_value = MagicMock()  # V2 success

    result = await x_service.unblock_user(999)

    assert result == "SUCCESS"
    mock_async_client.request.assert_awaited_once_with(
        "DELETE", "/2/users/12345/blocking/999", params={}, user_auth=True
    )


@pytest.mark.asyncio
async def test_unblock_user_zombie_fixed_toggle(
    x_service, mock_api_v1, mock_async_client
):
    """Test unblock_user handles Zombie Block using Toggle fix."""
    x_service.user_id = 12345
    mock_api_v1.destroy_block.side_effect = [
        tweepy.errors.NotFound(MagicMock()),  # Initial try
        MagicMock(),  # Destroy block in toggle success
    ]
    mock_api_v1.get_user.return_value = MagicMock()  # User exists
    mock_async_client.request.side_effect = tweepy.errors.NotFound(
        MagicMock()
    )  # V2 fail
    mock_api_v1.create_block.return_value = MagicMock()  # Toggle success

    result = await x_service.unblock_user(999)

    assert result == "SUCCESS"
    mock_async_client.request.assert_awaited_once_with(
        "DELETE", "/2/users/12345/blocking/999", params={}, user_auth=True
    )
    mock_api_v1.create_block.assert_called_once_with(user_id=999)


@pytest.mark.asyncio
async def test_unblock_user_zombie_v2_json_error(
    x_service, mock_api_v1, mock_async_client
):
    """Test unblock_user handles V2 JSON decode error (HTML response) gracefully."""
    x_service.user_id = 12345
    mock_api_v1.destroy_block.side_effect = [
        tweepy.errors.NotFound(MagicMock()),  # Initial try
    ]
    mock_api_v1.get_user.return_value = MagicMock()  # User exists

    # Simulate the specific JSON decode error from Tweepy/simplejson
    mock_async_client.request.side_effect = Exception(
        "Attempt to decode JSON with unexpected mimetype: text/html;charset=utf-8"
    )

    # It should fall through to Strategy 2 (Toggle Block)
    mock_api_v1.create_block.return_value = MagicMock()
    mock_api_v1.destroy_block.side_effect = [
        tweepy.errors.NotFound(MagicMock()),  # Initial
        MagicMock(),  # Toggle fix destroy
    ]

    result = await x_service.unblock_user(999)

    # It succeeds because Strategy 2 works
    assert result == "SUCCESS"
    mock_async_client.request.assert_awaited_once()
    mock_api_v1.create_block.assert_called_once_with(user_id=999)


@pytest.mark.asyncio
async def test_unblock_user_failure(x_service, mock_api_v1):
    """Test unblock_user returns FAILED on generic error."""
    mock_api_v1.destroy_block.side_effect = Exception("Generic error")

    result = await x_service.unblock_user(999)

    assert result == "FAILED"


@pytest.mark.asyncio
async def test_get_following_user_ids(x_service, mock_api_v1):
    """Test fetching following IDs using v1.1 API."""
    mock_api_v1.get_friend_ids.return_value = ([201, 202], (None, 0))

    ids = await x_service.get_following_user_ids()

    assert ids == {201, 202}
    mock_api_v1.get_friend_ids.assert_called_once_with(cursor=-1)


@pytest.mark.asyncio
async def test_get_follower_user_ids(x_service, mock_api_v1):
    """Test fetching follower IDs using v1.1 API."""
    mock_api_v1.get_follower_ids.return_value = ([301, 302], (None, 0))

    ids = await x_service.get_follower_user_ids()

    assert ids == {301, 302}
    mock_api_v1.get_follower_ids.assert_called_once_with(cursor=-1)


@pytest.mark.asyncio
async def test_unfollow_user_success(x_service, mock_api_v1):
    """Test unfollow_user success."""
    result = await x_service.unfollow_user(888)

    mock_api_v1.destroy_friendship.assert_called_once_with(user_id=888)
    assert result == "SUCCESS"
