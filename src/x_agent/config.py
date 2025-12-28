from pydantic_settings import BaseSettings, SettingsConfigDict
from pydantic import Field


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env", env_file_encoding="utf-8", extra="ignore"
    )

    x_api_key: str = Field(..., validation_alias="X_API_KEY")
    x_api_key_secret: str = Field(..., validation_alias="X_API_KEY_SECRET")
    x_access_token: str = Field(..., validation_alias="X_ACCESS_TOKEN")
    x_access_token_secret: str = Field(..., validation_alias="X_ACCESS_TOKEN_SECRET")

    environment: str = Field("development", validation_alias="X_AGENT_ENV")

    @property
    def is_dev(self) -> bool:
        return self.environment.lower() == "development"

    @property
    def db_name(self) -> str:
        return "insights_dev.db" if self.is_dev else "insights.db"


settings = Settings()
