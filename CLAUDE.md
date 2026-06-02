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

**Run integration evals** (requires ANTHROPIC_API_KEY; ~$0.50 per full run):

```bash
go test -tags=integration ./internal/agent/eval/ -v -timeout 10m
```

Until the agent-runner harness lands (P1.1), the test skips with an "unimplemented" message.

**Run DB-backed integration tests** (requires Docker — testcontainers-go boots a fresh Postgres per test):

```bash
make test-integration              # all integration-tagged tests
go test -tags=integration ./...   # equivalent
```

Two reusable helpers live under `//go:build integration`:
- `database.NewTestDB(t, models...)` — fresh Postgres 16 container per test, creates the requested model tables via Bun, terminates on cleanup.
- `controllers.NewTestController(t, db, opts)` — wires a `*Controller` with fake `CredStore` (KMS+S3), fake `SecretManager`, fake `EventService`. Returns the controller plus a `*TestFakes` handle so tests can assert on captured events or peek at stored blobs.

> **Note on `go mod tidy`:** the testcontainers-go dependencies are only imported behind `//go:build integration`. Always run `go vet -tags=integration ./...` before `go mod tidy` so the deps stay listed in `go.mod`. CI / release scripts that call `go mod tidy` without exercising the integration build will silently prune the deps.

**Note:** A limited test suite exists under `internal/agent/` (context and mobile-config tests) and `internal/agent/eval/` (integration-only eval fixtures). The eval fixtures are build-tag gated and won't run in standard `go test ./...`.

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
  - `events.EventService` — Resend (email automation + contact sync)
  - `storage.S3Storage` — AWS S3 (assets, user API keys, Android keystores)
  - `services.SlackService` — Slack notifications
  - `codemagic.Service` — CI/CD builds and store submissions
  - `fly.Service` / `fly.Inspector` — Fly.io app deployment and DNS
  - `neon.Service` / `neon.Registry` — Neon database provisioning (free vs paid org routing)
  - `meta.Service` — Meta Conversions API (CAPI) ad attribution
  - `revenuecat.Service` — In-app purchase auto-provisioning
  - `gitea.Service` — Git repository hosting
  - `gitrepo.Service` — GitHub export and repo operations
  - `mcpclient.Service` — MCP (Model Context Protocol) client
  - `remoteconfig.Service` — Remote config for mobile apps
  - `e2b.Service` — E2B sandbox environments
  - `fal.Service` — FAL AI image generation
  - `caddy.Admin` — Caddy reverse proxy admin API
  - Additional: `giteabackup`, `integrations`, `jobstream`, `ota`, `storeauth`
- **tasks/**: Async task payloads for [Asynq](https://github.com/hibiken/asynq) (Redis-backed). Task types: `agent:run` (AI generation/chat), `deploy:web`, `fly:delete-app`, `billing:org-grace-teardown`, `user:account-purge`, `cron:credential-health-daily`, `cron:reap-stuck-agent-runs`, `cron:reap-stuck-build-tasks`, `cron:fly-bandwidth-sync`.
- **constants/**: App-wide constants (build limits, LLM model, event names for PostHog/Resend, Slack channels, proxy settings).
- **utils/**: Shared helpers (auth token extraction, JSON parsing/response, validation, logging, image handling, session management).

### Key Patterns

- **Auth**: Bearer token extracted from `Authorization` header via `utils.ExtractTokenFromHeader()`. No middleware-based auth — each view handler extracts and validates the token inline.
- **Routing**: chi/v5 router. API routes under `/v1/` (legacy) and `/v2/` (current). Webhook routes at `/webhooks/stripe`, `/webhooks/fly`, and `/v2/webhooks/` (ASC notifications, Play RTDN, RevenueCat). Health check at `/health`.
- **HTTP server**: Main API on `:8001`. Caddy sits in front in production, routing `api.wemob.io` → `:8001`.
- **Secrets**: All secrets fetched from Infisical at startup via `SecretManager`, not from env vars directly (env vars only hold Infisical credentials).
- **Billing**: Stripe integration with webhooks, checkout, customer portal, subscriptions.
- **Background tasks**: Asynq client enqueues deploy/agent/cron tasks to Redis workers. Long-running AI agent runs (`generate_app`, `app_chat`) are dispatched as `agent:run` Asynq tasks rather than in-process goroutines, so API container restarts don't interrupt customer generations. Mobile builds are triggered via the Codemagic API (synchronous HTTP call, not Asynq). SSE events stream from worker → API via Redis pub/sub on `job:events:<jobID>`; cancel signals travel back via `job:cancel:<jobID>`.
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
