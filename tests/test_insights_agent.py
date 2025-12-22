import pytest
from unittest.mock import MagicMock, patch
from src.x_agent.agents.insights_agent import InsightsAgent
from src.x_agent.services.x_service import XService


@pytest.fixture
def mock_x_service():
    return MagicMock(spec=XService)


@pytest.fixture
def insights_agent(mock_x_service):
    return InsightsAgent(mock_x_service)


def test_execute_success(insights_agent, mock_x_service):
    """Test successful execution of the insights agent."""
    # Mock API response
    mock_me = MagicMock()
    mock_me.public_metrics = {"followers_count": 100, "following_count": 50}
    mock_x_service.get_me.return_value = mock_me

    with patch("src.x_agent.agents.insights_agent.database") as mock_db:
        # Mock database behavior
        mock_db.get_latest_insight.return_value = {"followers": 90, "following": 45}

        # Run agent
        insights_agent.execute()

        # Verify interactions
        mock_db.initialize_database.assert_called_once()
        mock_x_service.get_me.assert_called_once()
        mock_db.get_latest_insight.assert_called_once()
        mock_db.add_insight.assert_called_once_with(100, 50)


def test_execute_no_metrics(insights_agent, mock_x_service):
    """Test execution when user metrics cannot be retrieved."""
    mock_x_service.get_me.return_value = None

    with patch("src.x_agent.agents.insights_agent.database") as mock_db:
        insights_agent.execute()

        mock_db.initialize_database.assert_called_once()
        mock_x_service.get_me.assert_called_once()
        mock_db.add_insight.assert_not_called()


def test_execute_first_run(insights_agent, mock_x_service):
    """Test execution when there are no previous insights."""
    mock_me = MagicMock()
    mock_me.public_metrics = {"followers_count": 100, "following_count": 50}
    mock_x_service.get_me.return_value = mock_me

    with patch("src.x_agent.agents.insights_agent.database") as mock_db:
        mock_db.get_latest_insight.return_value = None

        with patch("builtins.print") as mock_print:
            insights_agent.execute()

            mock_db.add_insight.assert_called_once_with(100, 50)
            # Check if print was called (verifying report generation)
            assert mock_print.called
