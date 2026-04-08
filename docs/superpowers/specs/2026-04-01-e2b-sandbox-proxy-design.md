# E2B Sandbox Proxy — Backend Endpoints for Mobile Preview

**Date:** 2026-04-01
**Status:** Approved

## Problem

The mobile "try with your phone" Flutter app needs to show app previews in a WebView. Previews run in E2B sandboxes, but sandboxes time out after 1 hour. Currently, only the dashboard (Next.js) can start sandboxes. The backend needs the ability to start sandboxes on behalf of mobile clients so the Flutter app can trigger a preview without the dashboard UI.

## Decision

The backend proxies sandbox start requests to the dashboard's existing `/api/e2b/start` endpoint. No direct E2B API integration. The dashboard already handles the full lifecycle: checking for existing sandboxes, reconnecting, creating fresh ones, writing files, installing dependencies, and starting the dev server.

## Endpoints

### `POST /v2/apps/{appId}/sandbox/start`

Starts (or reconnects to) a sandbox for the given app by proxying to the dashboard.

**Auth:** Bearer token (same as all app endpoints).

**Request:** No body required. The `appId` comes from the URL.

**Flow:**

1. Authenticate user, verify app ownership.
2. POST to `{DASHBOARD_URL}/api/e2b/start` with:
   - Header: `Authorization: Bearer {user_token}`
   - Body: `{ "appId": "{appId}" }`
3. Read the SSE stream from the dashboard. The stream emits events:
   - `{ type: "status", status: "reconnecting|fetching|creating|installing|starting" }`
   - `{ type: "ready", previewUrl: "...", sandboxId: "..." }`
   - `{ type: "error", message: "..." }`
4. Wait for either `ready` or `error`.
5. On `ready`: save `sandboxId` to DB, return success response.
6. On `error`: return error response.

**Success Response (200):**

```json
{
  "status": "ready",
  "preview_url": "https://3000-abc123.preview.wemob.app",
  "sandbox_id": "abc123"
}
```

**Error Response (502):**

```json
{
  "error": "Dev server failed to start"
}
```

**Timeout:** 5 minutes (matches dashboard's `maxDuration = 300`). If the SSE stream doesn't produce a terminal event within 5 minutes, return a timeout error.

### `GET /v2/apps/{appId}/sandbox/status`

Returns the current sandbox state from the database. No external calls.

**Auth:** Bearer token.

**Flow:**

1. Authenticate user, verify app ownership.
2. Read `sandbox_id` from the app project record.
3. If empty: return stopped.
4. If present: construct preview URL and return ready.

**Note:** This does not verify the sandbox is actually alive with E2B. If the sandbox has timed out, the mobile app will get a dead URL and should call `/start` to restart it. This is acceptable because:
- Avoids the backend needing an E2B API key.
- The start endpoint handles reconnection automatically (the dashboard checks liveness).
- The mobile app can detect a dead WebView and trigger start.

**Response when sandbox exists (200):**

```json
{
  "status": "ready",
  "preview_url": "https://3000-abc123.preview.wemob.app",
  "sandbox_id": "abc123"
}
```

**Response when no sandbox (200):**

```json
{
  "status": "stopped",
  "preview_url": "",
  "sandbox_id": ""
}
```

## Architecture

```
Flutter App ──→ Backend ──→ Dashboard ──→ E2B
                  │
                  ├─ POST /sandbox/start  (proxy to dashboard, parse SSE)
                  └─ GET  /sandbox/status  (DB lookup only)
```

### New Package: `internal/services/e2b/`

Single file `e2b.go` containing:

- `type Service struct` — holds `dashboardURL` and `previewDomain` strings, plus an `*http.Client`.
- `StartSandbox(authToken, appId string) (*StartResult, error)` — POSTs to dashboard, parses SSE stream, returns `StartResult{Status, PreviewURL, SandboxID}` or error.
- `PreviewURL(sandboxID string) string` — constructs `https://3000-{sandboxId}.{previewDomain}`.

### Integration

- Service instantiated in `main.go` with secrets `DASHBOARD_URL` and `E2B_PREVIEW_DOMAIN` from Infisical.
- Added to `Controller` struct.
- Two new view handlers in `internal/views/app_deploy_views.go` (alongside existing `GetAppSandbox`/`SetAppSandbox`).
- Routes added under existing `/{appId}/sandbox/` group in `main.go`.

### Secrets Required (Infisical)

| Secret | Example | Purpose |
|---|---|---|
| `DASHBOARD_URL` | `https://dashboard.wemob.io` | Base URL for proxying to dashboard API routes |
| `E2B_PREVIEW_DOMAIN` | `preview.wemob.app` | Domain for constructing sandbox preview URLs |

No `E2B_API_KEY` needed in the backend.

## What This Does NOT Do

- No sandbox stop/kill endpoint. Not needed for the mobile preview use case.
- No direct E2B API calls. All sandbox orchestration goes through the dashboard.
- No SSE streaming to the mobile client. The backend waits for the result and returns a single JSON response. The Flutter app shows a loading spinner.
- No log streaming. Mobile preview doesn't need dev server logs.
