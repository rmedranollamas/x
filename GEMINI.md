# Gemini Project Context: X Agent Framework

## Project Overview

Standalone, high-performance Go CLI framework for X (Twitter) account management via modular agents.

## Go Architecture (`github.com/rmedranollamas/x-agent`)

- **cmd/x-agent:** Entry point with Cobra CLI, stream separation (stdout for pipeable data, stderr for banners/logs), and environment visibility headers.
- **internal/config:** Configuration loader supporting `.env` and environment variables (`X_API_KEY`, `X_AGENT_ENV`, SMTP, etc.) with fail-fast validation.
- **internal/db:** Pure Go SQLite persistence (`modernc.org/sqlite`, zero CGO), single-connection pool (`SetMaxOpenConns(1)`), automated pre-migration backups, and idempotent migrations (`m001`–`m004`).
- **internal/xapi:** Lightweight dual-API client (`dghubble/oauth1` + standard `net/http`) covering v1.1/v2 endpoints, 15m/24h rate limit detection, and 3-tier zombie unblock recovery.
- **internal/agents:** 5 core agents (`unblock`, `insights`, `blocked-ids`, `unfollow`, `delete`) and pure Go SMTP reporting.

## Key Technologies

- Go 1.23+ (`/usr/local/go/bin/go`)
- `modernc.org/sqlite` (Zero-CGO SQLite persistence)
- `github.com/spf13/cobra` (CLI)
- `github.com/dghubble/oauth1` (OAuth 1.0a authentication)
- `github.com/caarlos0/env/v11` & `github.com/joho/godotenv` (Configuration)
- `github.com/cenkalti/backoff/v4` (Exponential backoff retries)

## Running & Building the Go Application

1. Build binary: `make build` (outputs to `dist/x-agent` and `./x-agent`)
1. Multi-arch build: `make build-all` (generates `linux/amd64` and `linux/arm64`)
1. Install: `make install` (installs to `$GOPATH/bin`)
1. Run tests: `make test` or `make test-e2e`
1. Execute: `./x-agent [insights|unblock|unfollow|delete|blocked-ids|db] [flags]`
