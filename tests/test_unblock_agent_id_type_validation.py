import pytest
from unittest.mock import MagicMock
from src.x_agent.agents.unblock_agent import UnblockAgent
from src.x_agent.services.x_service import XService


def test_unblock_agent_init_with_invalid_user_id_type():
    """
    Test that UnblockAgent's __init__ method raises TypeError when user_id is not an integer.
    """
    mock_x_service = MagicMock(spec=XService)

    # Test with a string user_id
    with pytest.raises(TypeError, match="User ID must be an integer"):
        UnblockAgent(x_service=mock_x_service, user_id="invalid")

    # Test with a float user_id
    with pytest.raises(TypeError, match="User ID must be an integer"):
        UnblockAgent(x_service=mock_x_service, user_id=123.45)

    # Test with a list user_id
    with pytest.raises(TypeError, match="User ID must be an integer"):
        UnblockAgent(x_service=mock_x_service, user_id=[123])

    # Test with None user_id (if explicit validation for None is desired)
    # The current implementation of UnblockAgent's __init__ sets user_id to None by default
    # if not provided. If a non-None type check is added, this test case might need adjustment.
    # For now, let's assume None is handled by the default behavior and not explicitly checked for type in __init__.
    # If the default value is changed, this test might need an adjustment to verify it as well.
    # with pytest.raises(TypeError, match="User ID must be an integer"):
    #     UnblockAgent(x_service=mock_x_service, user_id=None)

    # Test with a valid integer user_id (should not raise an error)
    try:
        UnblockAgent(x_service=mock_x_service, user_id=12345)
    except TypeError:
        pytest.fail(
            "UnblockAgent __init__ raised TypeError for a valid integer user_id"
        )
