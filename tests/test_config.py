from x_agent.config import Settings


def test_settings_default_env(monkeypatch):
    monkeypatch.delenv("X_AGENT_ENV", raising=False)
    settings = Settings(
        x_api_key="k",
        x_api_key_secret="ks",
        x_access_token="t",
        x_access_token_secret="ts",
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
    )
    assert settings.environment == "production"
    assert settings.is_dev is False
    assert settings.db_name == "insights.db"
