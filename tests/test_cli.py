import pytest
from typer.testing import CliRunner
from unittest.mock import patch, MagicMock
from x_agent.cli import app

runner = CliRunner()


@pytest.fixture
def mock_x_service():
    with patch("x_agent.cli.XService") as mock:
        yield mock


@pytest.fixture
def mock_agents():
    with (
        patch("x_agent.cli.UnblockAgent") as mock_unblock,
        patch("x_agent.cli.InsightsAgent") as mock_insights,
        patch("x_agent.cli.BlockedIdsAgent") as mock_blocked_ids,
    ):
        yield mock_unblock, mock_insights, mock_blocked_ids


def test_unblock_command(mock_x_service, mock_agents):
    mock_unblock_cls, _, _ = mock_agents
    mock_unblock_instance = mock_unblock_cls.return_value

    with patch.object(mock_unblock_instance, "execute", new_callable=MagicMock):
        with patch("x_agent.cli.asyncio.run") as mock_run:
            result = runner.invoke(app, ["unblock"])

            assert result.exit_code == 0
            mock_unblock_cls.assert_called_once_with(
                mock_x_service.return_value, user_id=None, refresh=False
            )
            mock_run.assert_called_once()


def test_unblock_command_with_user_id(mock_x_service, mock_agents):
    mock_unblock_cls, _, _ = mock_agents
    mock_unblock_instance = mock_unblock_cls.return_value

    with patch.object(mock_unblock_instance, "execute", new_callable=MagicMock):
        with patch("x_agent.cli.asyncio.run") as mock_run:
            result = runner.invoke(app, ["unblock", "--user-id", "12345"])

            assert result.exit_code == 0
            mock_unblock_cls.assert_called_once_with(
                mock_x_service.return_value, user_id=12345, refresh=False
            )
            mock_run.assert_called_once()


def test_insights_command(mock_x_service, mock_agents):
    _, mock_insights_cls, _ = mock_agents
    mock_insights_instance = mock_insights_cls.return_value

    with patch.object(mock_insights_instance, "execute", new_callable=MagicMock):
        with patch("x_agent.cli.asyncio.run") as mock_run:
            result = runner.invoke(app, ["insights", "--debug"])

            assert result.exit_code == 0
            mock_insights_cls.assert_called_once()
            mock_run.assert_called_once()


def test_blocked_ids_command(mock_x_service, mock_agents):
    _, _, mock_blocked_ids_cls = mock_agents
    mock_blocked_ids_instance = mock_blocked_ids_cls.return_value

    with patch.object(mock_blocked_ids_instance, "execute", new_callable=MagicMock):
        with patch("x_agent.cli.asyncio.run") as mock_run:
            result = runner.invoke(app, ["blocked-ids"])

            assert result.exit_code == 0
            mock_blocked_ids_cls.assert_called_once()
            mock_run.assert_called_once()


def test_unfollow_command(mock_x_service, mock_agents):
    with patch("x_agent.cli.UnfollowAgent") as mock_unfollow_cls:
        mock_unfollow_instance = mock_unfollow_cls.return_value
        with patch.object(mock_unfollow_instance, "execute", new_callable=MagicMock):
            with patch("x_agent.cli.asyncio.run") as mock_run:
                result = runner.invoke(app, ["unfollow"])

                assert result.exit_code == 0
                mock_unfollow_cls.assert_called_once_with(mock_x_service.return_value)
                mock_run.assert_called_once()


def test_invalid_command():
    result = runner.invoke(app, ["invalid"])
    assert result.exit_code != 0
    assert "No such command" in result.output
