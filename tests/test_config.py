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
