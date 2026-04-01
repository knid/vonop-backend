# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
make build          # Build binary to bin/app
make run            # Build and run
make dev            # Run with go run (no binary)
go build -o bin/app cmd/main.go   # Manual build
```

**Docker (dev):** `docker-compose -f docker-compose-dev.yaml up` — uses Air for hot-reload (`.air.toml`), Postgres on localhost:5432, env from `.env.dev`

**Docker (prod):** `docker-compose up` — multi-stage Alpine build with Caddy reverse proxy

**No test suite exists.** There are no `_test.go` files in this project.

## Architecture

Go monolith (module `github.com/knid/wemob`) serving a REST API for the Wemob mobile app builder platform. Single entrypoint at `cmd/main.go`.

### Layer Structure (all under `internal/`)

**views** → **controllers** → **database** (with **models**, **requests**, **responses** as data types)

- **views/**: HTTP handlers. Each method lives on `HttpViews` struct (wraps `Controller`). Responsible for extracting auth tokens, parsing request bodies, calling controller methods, and returning JSON via `utils.JSONResponse`/`utils.JSONErrorResponse`.
- **controllers/**: Business logic. `Controller` struct holds all dependencies (DB, services, task queue, storage). Created in `main()` with all injected deps.
- **database/**: Data access layer using [Bun ORM](https://bun.uptrace.dev/) with PostgreSQL. Thin wrapper `Database` struct over `*bun.DB`.
- **models/**: Bun ORM models with `bun:` struct tags. Tables auto-created via `createSchema()` in `main.go` using `IfNotExists()`.
- **requests/**: Incoming JSON request structs with `validate` tags (go-playground/validator) and custom `errormgs` tags for error messages. Validation uses the generic `utils.ValidateFunc[T]()`.
- **responses/**: Outgoing JSON response structs.
- **services/**: External service integrations behind interfaces:
  - `secret.SecretManager` — Infisical (secrets management)
  - `email.EmailService` — Brevo
  - `crm.CRMService` — Brevo
  - `storage.S3Storage` — AWS S3 (two buckets: assets + user API keys)
  - `services.SlackService` — Slack notifications
- **tasks/**: Async task payloads for [Asynq](https://github.com/hibiken/asynq) (Redis-backed). Task types: `build:android`, `deploy:android`, plus iOS equivalents.
- **constants/**: App-wide constants (build limits, LLM model, Brevo templates, Slack channels, proxy settings).
- **utils/**: Shared helpers (auth token extraction, JSON parsing/response, validation, logging, image handling, session management).

### Key Patterns

- **Auth**: Bearer token extracted from `Authorization` header via `utils.ExtractTokenFromHeader()`. No middleware-based auth — each view handler extracts and validates the token inline.
- **Routing**: chi/v5 router. All API routes under `/v1/`. Webhook routes at `/webhooks/stripe`. Health check at `/health`.
- **HTTP server**: Main API on `:8001`. Caddy sits in front in production, routing `api.wemob.io` → `:8001`.
- **Secrets**: All secrets fetched from Infisical at startup via `SecretManager`, not from env vars directly (env vars only hold Infisical credentials).
- **Billing**: Stripe integration with webhooks, checkout, customer portal, subscriptions.
- **Background tasks**: Asynq client enqueues build/deploy tasks to Redis workers (workers are external).
- **Schema migration**: Auto-create tables in `createSchema()` in `main.go`. No formal migration tool.
- **Logging**: `slog` with BetterStack handler and `httplog` middleware for request logging.

### Request Flow Pattern

```
View handler:
  1. Extract token → utils.ExtractTokenFromHeader(r)
  2. Authenticate  → controller.GetUser(token) or controller.GetUserFromReq(r)
  3. Parse body    → utils.ParseJSONBody(r, &req)
  4. Validate      → req.Validate(validator) (when defined)
  5. Business logic → controller.SomeMethod(...)
  6. Respond       → utils.JSONResponse(w, data, status) or utils.JSONErrorResponse(w, msg, status)
```
