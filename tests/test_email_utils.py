import pytest
from unittest.mock import patch, AsyncMock
import aiosmtplib
from x_agent.utils.email_utils import send_report_email


@pytest.fixture
def mock_smtp_send():
    with patch("aiosmtplib.send", new_callable=AsyncMock) as mock:
        yield mock


@pytest.mark.asyncio
async def test_send_report_email_success(mock_smtp_send):
    # Setup settings for email
    with patch("x_agent.utils.email_utils.settings") as mock_settings:
        mock_settings.smtp_host = "smtp.test.com"
        mock_settings.smtp_port = 587
        mock_settings.smtp_user = "user@test.com"
        mock_settings.smtp_password = "password"
        mock_settings.report_recipient = "target@test.com"
        mock_settings.environment = "test"
        mock_settings.smtp_use_tls = False
        mock_settings.smtp_start_tls = True
        mock_settings.check_email_config.return_value = None

        report_text = "Test Report Content"
        await send_report_email(report_text)

        mock_smtp_send.assert_called_once()
        call_args = mock_smtp_send.call_args[1]
        assert call_args["hostname"] == "smtp.test.com"
        assert call_args["username"] == "user@test.com"

        # Verify message content
        message = mock_smtp_send.call_args[0][0]
        assert message["To"] == "target@test.com"

        # For multipart, we check the plain text part
        body = message.get_body(preferencelist=("plain",)).get_content()
        assert report_text in body


@pytest.mark.asyncio
async def test_send_report_email_smtp_error(mock_smtp_send, caplog):
    mock_smtp_send.side_effect = aiosmtplib.SMTPException("Connection error")

    with patch("x_agent.utils.email_utils.settings") as mock_settings:
        mock_settings.smtp_user = "user@test.com"
        mock_settings.smtp_password = "password"
        mock_settings.report_recipient = "target@test.com"
        mock_settings.check_email_config.return_value = None

        await send_report_email("Test content")
        assert "SMTP error occurred: Connection error" in caplog.text


@pytest.mark.asyncio
async def test_send_report_email_missing_config(mock_smtp_send, caplog):
    with patch("x_agent.utils.email_utils.settings") as mock_settings:
        mock_settings.check_email_config.side_effect = ValueError("Missing config")

        await send_report_email("Test content")
        mock_smtp_send.assert_not_called()
        assert "Email configuration error: Missing config" in caplog.text
