import pytest
from x_agent.config import Settings


def test_settings_default_env(monkeypatch):
    monkeypatch.delenv("X_AGENT_ENV", raising=False)
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="development",
    )
    assert settings.environment == "development"
    assert settings.is_dev is True
    assert settings.db_name == "insights_dev.db"


def test_settings_prod_env(monkeypatch):
    monkeypatch.setenv("X_AGENT_ENV", "production")
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="production",
    )
    assert settings.environment == "production"
    assert settings.is_dev is False
    assert settings.db_name == "insights.db"


def test_check_config_success():
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="development",
    )
    # Should not raise
    settings.check_config()


def test_check_config_failure(monkeypatch):
    # Clear environment variables that might interfere
    monkeypatch.setenv("X_API_KEY", "")
    monkeypatch.setenv("X_API_KEY_SECRET", "")
    monkeypatch.setenv("X_ACCESS_TOKEN", "")
    monkeypatch.setenv("X_ACCESS_TOKEN_SECRET", "")

    # But settings.check_config() is our manual check for empty strings.
    settings = Settings(
        x_api_key="",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="development",
    )
    with pytest.raises(
        ValueError, match="Missing required environment variables: X_API_KEY"
    ):
        settings.check_config()

    settings = Settings(
        x_api_key="",
        x_api_key_secret="",
        x_access_token="",
        x_access_token_secret="",
        environment="development",
    )
    with pytest.raises(
        ValueError,
        match="Missing required environment variables: X_API_KEY, X_API_KEY_SECRET, X_ACCESS_TOKEN, X_ACCESS_TOKEN_SECRET",
    ):
        settings.check_config()


def test_check_email_config_success(monkeypatch):
    """Test check_email_config with all required fields set."""
    monkeypatch.setenv("SMTP_USER", "user@example.com")
    monkeypatch.setenv("SMTP_PASSWORD", "password")
    monkeypatch.setenv("REPORT_SENDER", "sender@example.com")
    monkeypatch.setenv("REPORT_RECIPIENT", "recipient@example.com")
    
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="development",
    )
    # Should not raise
    settings.check_email_config()


def test_check_email_config_missing_smtp_user(monkeypatch):
    """Test check_email_config with missing SMTP user."""
    monkeypatch.setenv("SMTP_USER", "")
    monkeypatch.setenv("SMTP_PASSWORD", "password")
    monkeypatch.setenv("REPORT_SENDER", "sender@example.com")
    monkeypatch.setenv("REPORT_RECIPIENT", "recipient@example.com")
    
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="development",
    )
    with pytest.raises(ValueError, match="Email reporting requires: SMTP_USER"):
        settings.check_email_config()


def test_check_email_config_missing_smtp_password(monkeypatch):
    """Test check_email_config with missing SMTP password."""
    monkeypatch.setenv("SMTP_USER", "user@example.com")
    monkeypatch.setenv("SMTP_PASSWORD", "")
    monkeypatch.setenv("REPORT_SENDER", "sender@example.com")
    monkeypatch.setenv("REPORT_RECIPIENT", "recipient@example.com")
    
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="development",
    )
    with pytest.raises(ValueError, match="Email reporting requires: SMTP_PASSWORD"):
        settings.check_email_config()


def test_check_email_config_missing_report_sender(monkeypatch):
    """Test check_email_config with missing report sender."""
    monkeypatch.setenv("SMTP_USER", "user@example.com")
    monkeypatch.setenv("SMTP_PASSWORD", "password")
    monkeypatch.setenv("REPORT_SENDER", "")
    monkeypatch.setenv("REPORT_RECIPIENT", "recipient@example.com")
    
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="development",
    )
    with pytest.raises(ValueError, match="Email reporting requires: REPORT_SENDER"):
        settings.check_email_config()


def test_check_email_config_missing_report_recipient(monkeypatch):
    """Test check_email_config with missing report recipient."""
    monkeypatch.setenv("SMTP_USER", "user@example.com")
    monkeypatch.setenv("SMTP_PASSWORD", "password")
    monkeypatch.setenv("REPORT_SENDER", "sender@example.com")
    monkeypatch.setenv("REPORT_RECIPIENT", "")
    
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="development",
    )
    with pytest.raises(ValueError, match="Email reporting requires: REPORT_RECIPIENT"):
        settings.check_email_config()


def test_check_email_config_multiple_missing(monkeypatch):
    """Test check_email_config with multiple missing fields."""
    monkeypatch.setenv("SMTP_USER", "")
    monkeypatch.setenv("SMTP_PASSWORD", "")
    monkeypatch.setenv("REPORT_SENDER", "")
    monkeypatch.setenv("REPORT_RECIPIENT", "")
    
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="development",
    )
    with pytest.raises(
        ValueError,
        match="Email reporting requires: SMTP_USER, SMTP_PASSWORD, REPORT_SENDER, REPORT_RECIPIENT",
    ):
        settings.check_email_config()


def test_check_email_config_empty_strings(monkeypatch):
    """Test check_email_config with empty strings."""
    monkeypatch.setenv("SMTP_USER", "")
    monkeypatch.setenv("SMTP_PASSWORD", "")
    monkeypatch.setenv("REPORT_SENDER", "")
    monkeypatch.setenv("REPORT_RECIPIENT", "")
    
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="development",
    )
    with pytest.raises(
        ValueError,
        match="Email reporting requires: SMTP_USER, SMTP_PASSWORD, REPORT_SENDER, REPORT_RECIPIENT",
    ):
        settings.check_email_config()


def test_normalized_environment():
    """Test normalized_environment property."""
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="  Development  ",
    )
    assert settings.normalized_environment == "development"


def test_normalized_environment_lowercase(monkeypatch):
    """Test normalized_environment with already lowercase."""
    monkeypatch.setenv("X_AGENT_ENV", "production")
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
        environment="production",
    )
    assert settings.normalized_environment == "production"
