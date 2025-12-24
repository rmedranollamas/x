import pytest
from unittest.mock import MagicMock
from src.x_agent.agents.unblock_agent import UnblockAgent
from src.x_agent.services.x_service import XService


@pytest.fixture
def mock_x_service():
    """Fixture for a mocked XService instance."""
    return MagicMock(spec=XService)


def test_unblock_agent_init_with_none_user_id(mock_x_service):
    """
    Test that UnblockAgent's __init__ allows user_id to be None.
    This validates the explicit type checking logic allows None.
    """
    agent = UnblockAgent(x_service=mock_x_service, user_id=None)
    assert agent.user_id is None


def test_unblock_agent_init_defaults_to_none_user_id(mock_x_service):
    """
    Test that UnblockAgent's __init__ defaults user_id to None if not provided.
    """
    agent = UnblockAgent(x_service=mock_x_service)
    assert agent.user_id is None


def test_unblock_agent_init_with_valid_user_id(mock_x_service):
    """
    Test that UnblockAgent's __init__ allows user_id to be an integer.
    """
    agent = UnblockAgent(x_service=mock_x_service, user_id=12345)
    assert agent.user_id == 12345


@pytest.mark.parametrize(
    "user_id",
    [
        "invalid",
        123.45,
        [123],
        {"id": 123},
    ],
)
def test_unblock_agent_init_with_invalid_user_id_type(mock_x_service, user_id):
    """
    Test that UnblockAgent's __init__ raises TypeError when user_id is not an integer or None.
    """
    with pytest.raises(TypeError, match="User ID must be an integer"):
        UnblockAgent(x_service=mock_x_service, user_id=user_id)
