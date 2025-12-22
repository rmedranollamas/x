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


settings = Settings()
