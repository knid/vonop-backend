# Ship UX — Phase 1 · Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the invisible foundation for the Ship UX redesign — rename credentials to connections, add `deployment_events` + `build_annotations` tables, wire iOS auto-sign into the build webhook, seed 5 build annotations, backfill per-project build defaults, and add `fastlane precheck` to the iOS deploy lane. Zero user-visible behavior change; the existing Publish Mission Control keeps working throughout.

**Architecture:** Backend-only Go + SQL + one Fastfile edit. All changes live in `wemob-backend/` + `wemob-flutter-template/codemagic.yaml`. No frontend changes in this phase. Migrations piggyback on the existing ad-hoc `ALTER TABLE` list in `cmd/main.go`. New models follow the established Bun ORM pattern. New HTTP routes sit next to existing ones and call the same handlers until Phase 2 swaps callers.

**Tech stack:** Go 1.22+, Bun ORM, Postgres, chi/v5 router, Asynq (Redis task queue), Codemagic CI, fastlane.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md`

---

## Test strategy

This codebase has no test suite (see `CLAUDE.md`). Verification is by:
- `go build ./...` must succeed at the end of every task that touches Go.
- `go vet ./...` must be clean.
- Manual smoke checks via `psql` against the dev database and `curl` against the running API.
- Every task has explicit `Verify:` commands.

Introducing a test suite is out of scope for Phase 1.

## File structure overview

### New files

```
internal/models/deployment_event.go      — append-only event stream per deployment
internal/models/build_annotation.go      — pattern-matched failure annotations
internal/database/annotations_seed.go    — idempotent seed of 5 initial patterns
internal/database/build_defaults_backfill.go — one-shot backfill
```

### Renamed files

```
internal/models/credentials.go           — keep filename, rename structs inside
internal/views/credentials.go            — keep filename, rename handlers inside
internal/controllers/credentials.go      — keep filename, rename methods inside
internal/requests/credentials.go         — keep filename, rename types inside
internal/responses/credentials.go        — keep filename, rename types inside
```

### Modified files

```
cmd/main.go                              — schema + migrations + routes + startup hooks
internal/controllers/reports.go          — auto-chain iOS sign + annotate failures
internal/controllers/deployment.go       — new helper: sign-from-build
../wemob-flutter-template/codemagic.yaml — add fastlane precheck to ios-deploy
```

## Why these boundaries

- **Rename structs in-place, keep filenames.** `git log` continuity matters; renaming files gains nothing and loses blame history.
- **New models get their own files.** `deployment_event.go` and `build_annotation.go` are small, focused, and will accrete methods during Phase 5 (timeline query helpers, pattern match scoring). Better to start focused.
- **Seed + backfill go in `internal/database/`**, not in `cmd/main.go`. Keeps `main.go` readable. Both are called once from startup.
- **Auto-sign lives in `reports.go`**, not a new orchestrator file. It's a 15-line branch on the existing `UpdateBuildTaskStatus` code path — factoring it out now is premature.
- **Legacy routes call the same handlers** — no code duplication, just extra route registrations.

---

## Task 1: Rename credential structs to connection structs

**Why:** Matches design's "Connection" mental model. Required before any downstream renames compile.

**Files:**
- Modify: `internal/models/credentials.go`

- [ ] **Step 1: Rename structs and table names**

Replace the contents of `internal/models/credentials.go`:

```go
package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// AppleConnection is a user's App Store Connect API key and team metadata.
// Used to submit iOS builds to the App Store under the user's own account.
// Table name kept as "app_store_credentials" for now; Task 2 renames it.
type AppleConnection struct {
	bun.BaseModel `bun:"table:app_store_credentials,alias:ac"`

	ID            uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID        uuid.UUID `bun:",notnull,type:uuid"`
	User          Project   `bun:"rel:belongs-to,join:user_id=id"`
	Name          string    `bun:",notnull"`
	KeyID         string    `bun:",notnull"`
	TeamID        string    `bun:",notnull"`
	IssuerID      string    `bun:",notnull"`
	P8KeyPath     string    `bun:",notnull"`
	APNSP8KeyPath string    `bun:",notnull"`
	APNSP8KeyID   string    `bun:",notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt     time.Time `bun:",soft_delete,nullzero"`
}

// GooglePlayConnection is a user's Google Play Console service account key.
// Used to submit Android builds to Play Store under the user's own account.
// Table name kept as "google_play_credentials" for now; Task 2 renames it.
type GooglePlayConnection struct {
	bun.BaseModel `bun:"table:google_play_credentials,alias:gpc"`

	ID                 uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID             uuid.UUID `bun:",notnull,type:uuid"`
	User               Project   `bun:"rel:belongs-to,join:user_id=id"`
	Name               string    `bun:",notnull"`
	ServiceAccountPath string    `bun:",notnull"`
	CreatedAt          time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt          time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt          time.Time `bun:",soft_delete,nullzero"`
}
```

Key change: added explicit `bun.BaseModel` with `table:` tag (matches other models in the repo like `AppStoreListing`) so the table name is explicit even as the struct name changes.

- [ ] **Step 2: Update all references across the codebase**

Run:
```bash
cd /Users/knid/Projects/wemob/wemob-backend
grep -rln "AppStoreCredentials\|GooglePlayCredentials" internal/ cmd/
```

For every file returned, replace:
- `models.AppStoreCredentials` → `models.AppleConnection`
- `models.GooglePlayCredentials` → `models.GooglePlayConnection`
- Struct field like `AppStoreCredentials *AppStoreCredentials` → `AppleConnection *AppleConnection` (e.g., in `internal/models/deployment.go`)
- Any variables named `appStoreCreds` → `appleConn`; `googlePlayCreds` → `googlePlayConn` (for readability — not strictly required).

This includes `cmd/main.go`'s `createSchema` models slice around line 83:
```go
(*models.GooglePlayCredentials)(nil),   // → (*models.GooglePlayConnection)(nil),
(*models.AppStoreCredentials)(nil),     // → (*models.AppleConnection)(nil),
```

Important: the FK column names on `Deployment` (`app_store_credentials_id`, `google_play_credentials_id`) must stay unchanged in the `bun:"rel:..."` tag joins so DB lookups still work:

```go
AppStoreCredentialsID   uuid.UUID              `bun:",type:uuid"`
GooglePlayCredentialsID uuid.UUID              `bun:",type:uuid"`
AppStoreCredentials     *AppleConnection       `bun:"rel:belongs-to,join:app_store_credentials_id=id"`
GooglePlayCredentials   *GooglePlayConnection  `bun:"rel:belongs-to,join:google_play_credentials_id=id"`
```

Rename the Go field names to match the new struct (`AppStoreCredentials` → `AppleConnection`, `GooglePlayCredentials` → `GooglePlayConnection`) but keep the join column names as-is (Task 2 renames columns).

- [ ] **Step 3: Verify build**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Expected: both commands succeed with no output.

- [ ] **Step 4: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/ cmd/
git commit -m "refactor(models): rename credential structs to connection structs

Step 1 of the Ship UX rename. Struct names now match the user-facing
'Connection' terminology. Table names and FK columns remain
unchanged in this commit — Task 2 renames them in one SQL transaction."
```

---

## Task 2: Rename DB tables and FK columns

**Why:** Make the database match the struct names. Single SQL transaction.

**Files:**
- Modify: `cmd/main.go` (migrations block around lines 170-280)
- Modify: `internal/models/credentials.go` (update `bun:"table:..."` tags)
- Modify: `internal/models/deployment.go` (update `bun:"rel:..."` join column names)

- [ ] **Step 1: Add rename migrations to the migrations block in `cmd/main.go`**

Find the block in `cmd/main.go` that runs a slice of `ALTER TABLE` statements (around line 170). Look for the last entry in the slice (check lines 260-300 range for the closing). Append these entries, in this exact order:

```go
// Ship UX Phase 1: rename credentials → connections
`ALTER TABLE IF EXISTS app_store_credentials RENAME TO apple_connections`,
`ALTER TABLE IF EXISTS google_play_credentials RENAME TO google_play_connections`,
// Deployment FK columns
`ALTER TABLE IF EXISTS deployments RENAME COLUMN app_store_credentials_id TO apple_connection_id`,
`ALTER TABLE IF EXISTS deployments RENAME COLUMN google_play_credentials_id TO google_play_connection_id`,
```

The `IF EXISTS` / `IF NOT EXISTS` idempotency matters — the migrations block re-runs on every startup.

- [ ] **Step 2: Update `bun:"table:..."` tags in `internal/models/credentials.go`**

Change both structs' `BaseModel` tags:

```go
// AppleConnection
bun.BaseModel `bun:"table:apple_connections,alias:ac"`

// GooglePlayConnection
bun.BaseModel `bun:"table:google_play_connections,alias:gpc"`
```

- [ ] **Step 3: Update FK column names in `internal/models/deployment.go`**

Change the Deployment struct so FK column + relation join names match the new column names:

```go
AppleConnectionID        uuid.UUID              `bun:",type:uuid"`
GooglePlayConnectionID   uuid.UUID              `bun:",type:uuid"`
AppleConnection          *AppleConnection       `bun:"rel:belongs-to,join:apple_connection_id=id"`
GooglePlayConnection     *GooglePlayConnection  `bun:"rel:belongs-to,join:google_play_connection_id=id"`
```

- [ ] **Step 4: Update all references to the renamed Deployment fields across the codebase**

Run:
```bash
cd /Users/knid/Projects/wemob/wemob-backend
grep -rln "AppStoreCredentialsID\|GooglePlayCredentialsID\|\.AppStoreCredentials\b\|\.GooglePlayCredentials\b" internal/ cmd/
```

In each file, replace:
- `AppStoreCredentialsID` → `AppleConnectionID`
- `GooglePlayCredentialsID` → `GooglePlayConnectionID`
- Field accesses `d.AppStoreCredentials` → `d.AppleConnection`; `d.GooglePlayCredentials` → `d.GooglePlayConnection`

Do not touch anything under `internal/tasks/` or Codemagic YAML yet — those use the `CM_SERVICE_ACCOUNT_S3` and `CM_P8_KEY_S3_PATH` env var names which are separate from Go field names.

- [ ] **Step 5: Verify build**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Expected: clean.

- [ ] **Step 6: Run migrations against dev DB and verify**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d postgres
go run cmd/main.go &
SERVER_PID=$!
sleep 5
kill $SERVER_PID

# Verify the renames landed
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "\dt apple_connections google_play_connections"
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "\d deployments" | grep -E "apple_connection_id|google_play_connection_id"
```

Expected output: both tables exist with their new names; the `deployments` table has `apple_connection_id` and `google_play_connection_id` columns.

- [ ] **Step 7: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add cmd/main.go internal/models/credentials.go internal/models/deployment.go internal/
git commit -m "refactor(db): rename credential tables and FK columns to connections

Renames app_store_credentials → apple_connections, google_play_credentials
→ google_play_connections, and the two Deployment FK columns to match.
Struct bun:\"table:...\" tags updated to point at the new table names.

Old table names are gone — callers must use the new names from here on.
Route aliases in Task 3 preserve the public API surface."
```

---

## Task 3: Rename controller methods and keep legacy HTTP routes as aliases

**Why:** New route names for the new mental model. Old routes stay alive so any in-flight frontend requests don't break during rollout.

**Files:**
- Modify: `internal/controllers/credentials.go` (rename methods)
- Modify: `internal/views/credentials.go` (rename handlers)
- Modify: `internal/requests/credentials.go` (rename request types)
- Modify: `internal/responses/credentials.go` (rename response types)
- Modify: `internal/database/credentials.go` if it exists (DB wrapper methods)
- Modify: `cmd/main.go` (route registration — add new routes alongside existing)

- [ ] **Step 1: Rename controller methods**

In `internal/controllers/credentials.go`, rename:
- `CreateAppStoreCredentials` → `CreateAppleConnection`
- `ListAppStoreCredentials` → `ListAppleConnections`
- `GetAppStoreCredential` → `GetAppleConnection`
- `UpdateAppStoreCredential` → `UpdateAppleConnection`
- `DeleteAppStoreCredential` → `DeleteAppleConnection`
- `CreateGooglePlayCredentials` → `CreateGooglePlayConnection`
- `ListGooglePlayCredentials` → `ListGooglePlayConnections`
- `GetGooglePlayCredential` → `GetGooglePlayConnection`
- `UpdateGooglePlayCredential` → `UpdateGooglePlayConnection`
- `DeleteGooglePlayCredential` → `DeleteGooglePlayConnection`

If the actual method names differ from this list, run:

```bash
grep -n "func (c \*Controller)" /Users/knid/Projects/wemob/wemob-backend/internal/controllers/credentials.go
```

and rename each method so the word `Credential`/`Credentials` becomes `Connection`/`Connections`, preserving the Apple/Google prefix.

- [ ] **Step 2: Rename request and response types**

In `internal/requests/credentials.go`:
- `CreateAppStoreCredentialRequest` → `CreateAppleConnectionRequest`
- `UpdateAppStoreCredentialRequest` → `UpdateAppleConnectionRequest`
- `CreateGooglePlayCredentialRequest` → `CreateGooglePlayConnectionRequest`
- `UpdateGooglePlayCredentialRequest` → `UpdateGooglePlayConnectionRequest`

In `internal/responses/credentials.go`:
- `AppStoreCredentialResponse` → `AppleConnectionResponse`
- `GooglePlayCredentialResponse` → `GooglePlayConnectionResponse`

If the actual type names differ, run:

```bash
grep -n "type.*Credential" /Users/knid/Projects/wemob/wemob-backend/internal/requests/credentials.go /Users/knid/Projects/wemob/wemob-backend/internal/responses/credentials.go
```

and apply the same Credential→Connection rename.

- [ ] **Step 3: Rename HTTP handlers**

In `internal/views/credentials.go`, rename each handler function so `Credential`/`Credentials` becomes `Connection`/`Connections`. Example:

```go
// Before
func (v *HttpViews) CreateAppStoreCredential(w http.ResponseWriter, r *http.Request) { ... }

// After
func (v *HttpViews) CreateAppleConnection(w http.ResponseWriter, r *http.Request) { ... }
```

Keep behavior identical. Inside each handler, the controller call now uses the new method name.

Also rename any handlers in `internal/views/storage_presign.go` that match — `PutP8KeyToS3Presign`, `PutAPNsKeyToS3Presign`, `PutServiceAccountToS3Presign` stay as-is (these are storage, not connections).

- [ ] **Step 4: Register new routes and keep old routes as aliases**

In `cmd/main.go`, find the block starting with `r.Route("/credentials"` around line 1179. Replace the entire `/credentials` block with the following, which adds new `/connections` routes and keeps `/credentials` as aliases pointing at the same handler:

```go
// New route names (preferred from Phase 1 onward)
r.Route("/connections", func(r chi.Router) {
    r.Route("/google-play", func(r chi.Router) {
        r.Get("/", httpViews.ListGooglePlayConnections)
        r.Post("/", httpViews.CreateGooglePlayConnection)
        r.Route("/{connectionId}", func(r chi.Router) {
            r.Get("/", httpViews.GetGooglePlayConnection)
            r.Put("/", httpViews.UpdateGooglePlayConnection)
            r.Delete("/", httpViews.DeleteGooglePlayConnection)
        })
    })
    r.Route("/apple", func(r chi.Router) {
        r.Get("/", httpViews.ListAppleConnections)
        r.Post("/", httpViews.CreateAppleConnection)
        r.Route("/{connectionId}", func(r chi.Router) {
            r.Get("/", httpViews.GetAppleConnection)
            r.Put("/", httpViews.UpdateAppleConnection)
            r.Delete("/", httpViews.DeleteAppleConnection)
        })
    })
})

// Legacy aliases — kept for 2 releases so existing frontend keeps working.
// Remove after Phase 2 ships the updated dashboard.
r.Route("/credentials", func(r chi.Router) {
    r.Route("/google", func(r chi.Router) {
        r.Get("/", httpViews.ListGooglePlayConnections)
        r.Post("/", httpViews.CreateGooglePlayConnection)
        r.Route("/{credentialId}", func(r chi.Router) {
            r.Get("/", httpViews.GetGooglePlayConnection)
            r.Put("/", httpViews.UpdateGooglePlayConnection)
            r.Delete("/", httpViews.DeleteGooglePlayConnection)
        })
    })
    r.Route("/appstore", func(r chi.Router) {
        r.Get("/", httpViews.ListAppleConnections)
        r.Post("/", httpViews.CreateAppleConnection)
        r.Route("/{credentialId}", func(r chi.Router) {
            r.Get("/", httpViews.GetAppleConnection)
            r.Put("/", httpViews.UpdateAppleConnection)
            r.Delete("/", httpViews.DeleteAppleConnection)
        })
    })
})
```

Both blocks point at the same handler functions. URL param name difference (`{credentialId}` vs `{connectionId}`) is cosmetic — the handler should read the URL param by either name via chi's context. Update the handler to try both param names if needed:

```go
// Inside, e.g., GetAppleConnection
id := chi.URLParam(r, "connectionId")
if id == "" {
    id = chi.URLParam(r, "credentialId")
}
```

Do this at the top of each `{connectionId}`-using handler.

- [ ] **Step 5: Verify build**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Expected: clean.

- [ ] **Step 6: Smoke test both route sets locally**

Start server, hit both new and old routes with the same valid bearer token:

```bash
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
sleep 5
TOKEN="<valid-bearer-from-a-real-login-session>"
# New route
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8001/v1/connections/apple/ | head -c 200
echo
# Legacy alias
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8001/v1/credentials/appstore/ | head -c 200
echo
```

Expected: both return the same JSON list (empty array or existing connections). Neither returns 404.

- [ ] **Step 7: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/controllers/credentials.go internal/views/credentials.go internal/requests/credentials.go internal/responses/credentials.go internal/database/credentials.go cmd/main.go
git commit -m "refactor(api): add /v1/connections routes, keep /v1/credentials aliased

New canonical paths:
  /v1/connections/apple/*
  /v1/connections/google-play/*

Legacy paths /v1/credentials/appstore/* and /v1/credentials/google/* stay
alive as aliases pointing at the same handlers. Both param names
({connectionId} and {credentialId}) are accepted inside the handlers.

Handlers, controller methods, request/response types all renamed to
use 'Connection' terminology."
```

---

## Task 4: Add `DeploymentEvent` model and schema

**Why:** Replace the opaque `deployment.status` string with a durable event stream. Phase 5 builds the timeline UI on top of this; Phase 1 just lays the table down.

**Files:**
- Create: `internal/models/deployment_event.go`
- Modify: `cmd/main.go` (add to `createSchema` + migrations block)

- [ ] **Step 1: Create the model**

Write `internal/models/deployment_event.go`:

```go
package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Event types recorded on a Deployment. Strings (not Go enum) so that
// new event types can be added without a migration. The set below
// covers the Phase 5 timeline design; we may add more later.
const (
	DeploymentEventSubmitted          = "submitted"
	DeploymentEventTriaged            = "triaged"            // managed track: human picked it up
	DeploymentEventUploadedToStore    = "uploaded_to_store"
	DeploymentEventStoreReviewStarted = "store_review_started"
	DeploymentEventStoreLive          = "store_live"
	DeploymentEventStoreRejected      = "store_rejected"
	DeploymentEventCanceled           = "canceled"
	DeploymentEventFailed             = "failed"
)

// Actor type — who caused the event.
const (
	DeploymentEventActorUser     = "user"      // the project owner
	DeploymentEventActorWemob    = "wemob"     // Wemob team member (managed track)
	DeploymentEventActorSystem   = "system"    // automated (Codemagic webhook, poll worker)
	DeploymentEventActorStoreAPI = "store_api" // Apple/Google API pushed a state change
)

// DeploymentEvent is an append-only log entry for a deployment.
// Timeline views are rendered from the ordered event stream, not from
// a derived "status" field.
type DeploymentEvent struct {
	bun.BaseModel `bun:"table:deployment_events,alias:de"`

	ID           uuid.UUID              `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	DeploymentID uuid.UUID              `bun:",notnull,type:uuid" json:"deployment_id"`
	EventType    string                 `bun:",notnull" json:"event_type"`
	ActorType    string                 `bun:",notnull" json:"actor_type"`
	ActorID      *uuid.UUID             `bun:",type:uuid,nullzero" json:"actor_id,omitempty"`
	Metadata     map[string]interface{} `bun:",type:jsonb,default:'{}'" json:"metadata"`
	OccurredAt   time.Time              `bun:",notnull,default:current_timestamp" json:"occurred_at"`
}
```

- [ ] **Step 2: Register the model in `createSchema`**

In `cmd/main.go`, find the `models := []interface{}{ ... }` slice inside `createSchema(db *bun.DB) error` (around line 63). Append one line:

```go
(*models.DeploymentEvent)(nil),
```

Place it near the other deployment-related entries so the grouping stays sensible.

- [ ] **Step 3: Add an index migration**

In the migrations block in `cmd/main.go`, append:

```go
`CREATE INDEX IF NOT EXISTS idx_deployment_events_deployment_id_occurred_at ON deployment_events (deployment_id, occurred_at)`,
```

This makes timeline queries cheap once Phase 5 renders them.

- [ ] **Step 4: Verify build and startup**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go run cmd/main.go &
SERVER_PID=$!
sleep 5
kill $SERVER_PID
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "\d deployment_events"
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "\di deployment_events*"
```

Expected: `deployment_events` table exists with the right columns; index `idx_deployment_events_deployment_id_occurred_at` is present.

- [ ] **Step 5: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/models/deployment_event.go cmd/main.go
git commit -m "feat(deployments): add DeploymentEvent model for append-only timeline

Replaces the opaque deployment.status string as the source of truth for
what happened to a submission. Phase 5 renders the managed-submission
timeline from this stream. No writers yet — Task 7 adds the first
writer (automated build-completed hook)."
```

---

## Task 5: Add `BuildAnnotation` model, seed five patterns, and schema

**Why:** We need the registry table before we can map failure messages to annotations (Task 6).

**Files:**
- Create: `internal/models/build_annotation.go`
- Create: `internal/database/annotations_seed.go`
- Modify: `cmd/main.go` (add model to `createSchema` + call seed at startup)

- [ ] **Step 1: Create the model**

Write `internal/models/build_annotation.go`:

```go
package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// BuildAnnotation is a known failure-pattern entry. When a build fails,
// the report handler walks the annotation table, tries each LogRegex
// against the build's error_message, and attaches the first match's ID
// to the AppBuildTask so the UI can render the human-readable
// explanation + suggested fix instead of a raw log dump.
//
// Annotations are seeded at startup from annotations_seed.go and can
// also be edited by operators via the admin routes added in a later
// phase.
type BuildAnnotation struct {
	bun.BaseModel `bun:"table:build_annotations,alias:ba"`

	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	Name           string    `bun:",notnull,unique" json:"name"`              // stable key, e.g. "pod_install_failed"
	LogRegex       string    `bun:",notnull" json:"log_regex"`
	HumanMessage   string    `bun:",notnull,type:text" json:"human_message"`
	SuggestedFix   string    `bun:",notnull,type:text" json:"suggested_fix"`
	RetryStrategy  string    `bun:",nullzero" json:"retry_strategy,omitempty"` // env-var set, e.g. "POD_REPO_UPDATE=1"
	SuccessCount   int       `bun:",notnull,default:0" json:"success_count"`
	FailureCount   int       `bun:",notnull,default:0" json:"failure_count"`
	Enabled        bool      `bun:",notnull,default:true" json:"enabled"`
	CreatedAt      time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt      time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}
```

- [ ] **Step 2: Register the model in `createSchema`**

In `cmd/main.go`, append to the `models := []interface{}{ ... }` slice:

```go
(*models.BuildAnnotation)(nil),
```

- [ ] **Step 3: Write the seed function**

Create `internal/database/annotations_seed.go`:

```go
package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// SeedBuildAnnotations inserts the initial five known failure patterns.
// Idempotent — uses the unique Name column to avoid duplicate inserts on
// re-runs. Existing rows are left untouched so operator-tuned entries
// survive a redeploy.
func SeedBuildAnnotations(ctx context.Context, db *bun.DB) error {
	seeds := []models.BuildAnnotation{
		{
			Name:          "pod_install_failed",
			LogRegex:      `(?i)(Error running pod install|CocoaPods could not find compatible versions)`,
			HumanMessage:  "CocoaPods couldn't install — usually a Flutter plugin version conflict.",
			SuggestedFix:  "Retry with repo update: sets POD_REPO_UPDATE=1 so CocoaPods refreshes its spec repo before installing.",
			RetryStrategy: "POD_REPO_UPDATE=1",
			Enabled:       true,
		},
		{
			Name:          "bundle_id_mismatch",
			LogRegex:      `(?i)(No profiles for .* were found|The provisioning profile is not compatible|bundle identifier .* does not match)`,
			HumanMessage:  "The bundle ID on this build doesn't match what's registered in App Store Connect.",
			SuggestedFix:  "Check the bundle ID in Identity. It must exactly match the identifier you registered at https://developer.apple.com/account/resources/identifiers/list.",
			RetryStrategy: "",
			Enabled:       true,
		},
		{
			Name:          "keystore_corrupt",
			LogRegex:      `(?i)(keystore was tampered with|Invalid keystore format|Wrong keystore password)`,
			HumanMessage:  "Your Android keystore file couldn't be read.",
			SuggestedFix:  "Retry with key regeneration: we'll generate a new upload keystore. Warning: if this app is already on Play Store, you must enroll in Play App Signing so Google keeps the app-signing key.",
			RetryStrategy: "REGEN_KEYSTORE=1",
			Enabled:       true,
		},
		{
			Name:          "flutter_version_conflict",
			LogRegex:      `(?i)(requires Dart SDK version|Flutter SDK version .* is incompatible|Because .* requires SDK version)`,
			HumanMessage:  "A Flutter plugin needs a different Flutter or Dart SDK version than the build environment provides.",
			SuggestedFix:  "Retry pinning Flutter to 3.41.6 (our tested version) via FLUTTER_VERSION env var.",
			RetryStrategy: "FLUTTER_VERSION=3.41.6",
			Enabled:       true,
		},
		{
			Name:          "xcode_missing_entitlement",
			LogRegex:      `(?i)(Provisioning profile .* doesn't include the .* entitlement|Missing entitlement|Capability .* is not enabled)`,
			HumanMessage:  "The app uses a capability (push, associated domains, HealthKit) that isn't turned on in your App Store Connect App ID.",
			SuggestedFix:  "Open https://developer.apple.com/account/resources/identifiers/list, find your bundle ID, and enable the capability flagged in the log. Then retry.",
			RetryStrategy: "",
			Enabled:       true,
		},
	}

	for _, seed := range seeds {
		_, err := db.NewInsert().
			Model(&seed).
			On("CONFLICT (name) DO NOTHING").
			Exec(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Call the seed from main startup**

In `cmd/main.go`, after `createSchema(bundb)` and after the `ALTER TABLE` migrations run, add:

```go
if err := database.SeedBuildAnnotations(ctx, bundb); err != nil {
    slog.Error("seed build annotations failed", "err", err)
    // non-fatal: server still starts; operator can retry by restart
}
```

(If the file uses a different logger, match the existing pattern.)

Make sure `"github.com/knid/wemob/internal/database"` is imported in `cmd/main.go` — it likely already is.

- [ ] **Step 5: Verify build and seed**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go run cmd/main.go &
SERVER_PID=$!
sleep 5
kill $SERVER_PID
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "SELECT name, enabled FROM build_annotations ORDER BY name"
```

Expected: 5 rows, all enabled.

Run again to confirm idempotency:

```bash
go run cmd/main.go &
SERVER_PID=$!
sleep 5
kill $SERVER_PID
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "SELECT count(*) FROM build_annotations"
```

Expected: still 5 rows, not 10.

- [ ] **Step 6: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/models/build_annotation.go internal/database/annotations_seed.go cmd/main.go
git commit -m "feat(builds): add BuildAnnotation model + seed 5 known failure patterns

Each annotation maps a log regex to a human-readable explanation, a
suggested fix, and an optional retry strategy (env vars passed to
Codemagic on retry-with-fix).

Seeded patterns: pod_install_failed, bundle_id_mismatch,
keystore_corrupt, flutter_version_conflict, xcode_missing_entitlement.

Task 6 wires the matcher into the build report webhook."
```

---

## Task 6: Match annotations on build failure

**Why:** Turn raw log tails into actionable UI without users needing to dig through logs.

**Files:**
- Modify: `internal/models/app_build_task.go` (add `AnnotationID` FK)
- Modify: `cmd/main.go` (add column migration)
- Modify: `internal/controllers/reports.go` (match on failure)

- [ ] **Step 1: Add `AnnotationID` field to `AppBuildTask`**

In `internal/models/app_build_task.go`, add after `ErrorMessage`:

```go
// AnnotationID is set by the build-completed webhook when the
// ErrorMessage matches a known pattern in build_annotations. Nil means
// either the build succeeded, or no pattern matched.
AnnotationID *uuid.UUID `bun:",type:uuid,nullzero"`
Annotation   *BuildAnnotation `bun:"rel:belongs-to,join:annotation_id=id"`
```

- [ ] **Step 2: Add the column migration**

In `cmd/main.go`'s migrations block, append:

```go
`ALTER TABLE app_build_tasks ADD COLUMN IF NOT EXISTS annotation_id uuid`,
`CREATE INDEX IF NOT EXISTS idx_app_build_tasks_annotation_id ON app_build_tasks (annotation_id) WHERE annotation_id IS NOT NULL`,
```

- [ ] **Step 3: Add an annotation-matcher helper**

Create `internal/database/annotations_match.go`:

```go
package database

import (
	"context"
	"regexp"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// MatchBuildAnnotation walks the enabled annotations and returns the
// first one whose LogRegex matches errorMessage, or (uuid.Nil, nil) if
// none match. Regex compilation is done per-call for simplicity — the
// registry is small (tens of rows) and failures are infrequent, so this
// isn't hot-path. Revisit if the registry grows past ~200.
func MatchBuildAnnotation(ctx context.Context, db *bun.DB, errorMessage string) (uuid.UUID, error) {
	var annotations []models.BuildAnnotation
	err := db.NewSelect().
		Model(&annotations).
		Where("enabled = ?", true).
		Scan(ctx)
	if err != nil {
		return uuid.Nil, err
	}

	for _, a := range annotations {
		re, err := regexp.Compile(a.LogRegex)
		if err != nil {
			// Bad regex in the registry — skip it rather than fail the
			// whole match; operator will see the bad one via metrics.
			continue
		}
		if re.MatchString(errorMessage) {
			return a.ID, nil
		}
	}
	return uuid.Nil, nil
}
```

- [ ] **Step 4: Call the matcher in `UpdateBuildTaskStatus`**

Open `internal/controllers/reports.go` and find `UpdateBuildTaskStatus`. When the incoming status represents a failure (`"error"`, `"failed"` — match whatever the function already branches on), after the error_message is saved onto the task, add:

```go
if isErrorStatus(reportBuildReq.Status) && reportBuildReq.Message != "" {
    annotationID, annotErr := database.MatchBuildAnnotation(ctx, c.DB.Db, reportBuildReq.Message)
    if annotErr == nil && annotationID != uuid.Nil {
        task.AnnotationID = &annotationID
        // Persist. Use whichever DB update pattern the file already uses.
        if _, uerr := c.DB.Db.NewUpdate().
            Model(task).
            Column("annotation_id").
            Where("id = ?", task.ID).
            Exec(ctx); uerr != nil {
            slog.Warn("persist annotation failed", "task_id", task.ID, "err", uerr)
        }
    }
}
```

Notes:
- `isErrorStatus` — if the function doesn't already have a helper, use the existing check (probably `reportBuildReq.Status == "error"` or `strings.EqualFold(reportBuildReq.Status, "error")`).
- `c.DB.Db` — this is the bun.DB on the controller's DB wrapper. The exact field name may be different; match what other controllers use.
- If the task object is written back in a batch at the bottom of the function, just set `task.AnnotationID` and let the existing batch-write pick it up instead of a separate update.

- [ ] **Step 5: Verify build and that existing build-report flow still passes**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

Then run the server and POST a fake failed-build report:

```bash
go run cmd/main.go &
SERVER_PID=$!
sleep 5

SECRET="<value of REPORT_API_SECRET from .env.dev>"
TASK_ID="<an existing app_build_tasks.id from your dev DB, or create one manually>"
curl -s -X POST http://localhost:8001/v1/reports/build \
  -H "Content-Type: application/json" \
  -d "{\"status\":\"error\",\"message\":\"Error running pod install\",\"secret\":\"$SECRET\",\"task_id\":\"$TASK_ID\"}"

PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "SELECT id, annotation_id FROM app_build_tasks WHERE id = '$TASK_ID'"
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "SELECT name FROM build_annotations WHERE id = (SELECT annotation_id FROM app_build_tasks WHERE id = '$TASK_ID')"

kill $SERVER_PID
```

Expected: the task row now has a non-null `annotation_id` and the matched annotation's `name` is `pod_install_failed`.

- [ ] **Step 6: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/models/app_build_task.go internal/database/annotations_match.go internal/controllers/reports.go cmd/main.go
git commit -m "feat(builds): match failed builds against annotation registry

When Codemagic reports a failed build, walk the enabled annotations,
try each LogRegex against the error_message, and attach the first
match's ID to the app_build_tasks row.

Front-ends will read task.annotation_id and render the
human_message + suggested_fix instead of the raw log tail.
Unknown failures stay untagged and render the generic 'show full log'
template."
```

---

## Task 7: Auto-chain iOS signing on successful iOS build

**Why:** The design moves the separate Sign button away from the UI. The build webhook becomes the orchestrator.

**Files:**
- Modify: `internal/controllers/reports.go` (`UpdateBuildTaskStatus`)
- Modify: `internal/controllers/deployment.go` (add helper if none exists)

- [ ] **Step 1: Inspect the existing sign-trigger path**

Read `internal/controllers/deployment.go` around `SignUnsignedIPATask` (line 446). Note the arguments it takes (`user *models.User`, `signReq requests.SignIPARequest`). The request struct lives in `internal/requests/`.

You'll reuse this function, not reimplement signing.

- [ ] **Step 2: Add an "auto-sign after build" helper on the controller**

In `internal/controllers/deployment.go`, add below `SignUnsignedIPATask`:

```go
// AutoSignIOSBuildIfPossible is called from the build-completed webhook.
// If the completed build is iOS and the owner has at least one active
// Apple connection, it enqueues a sign task using that connection.
// Silent no-op otherwise — this is a convenience path, not a guarantee.
//
// Returns (true, nil) when a sign was enqueued.
func (c *Controller) AutoSignIOSBuildIfPossible(ctx context.Context, task *models.AppBuildTask) (bool, error) {
    if task.Platform != "ios" {
        return false, nil
    }
    // Load the project to get the user.
    var project models.Project
    if err := c.DB.Db.NewSelect().
        Model(&project).
        Where("id = ?", task.ProjectID).
        Scan(ctx); err != nil {
        return false, err
    }
    var user models.User
    if err := c.DB.Db.NewSelect().
        Model(&user).
        Where("id = ?", project.UserID).
        Scan(ctx); err != nil {
        return false, err
    }
    // Pick the user's most-recently-used Apple connection.
    var conn models.AppleConnection
    err := c.DB.Db.NewSelect().
        Model(&conn).
        Where("user_id = ?", user.ID).
        Order("updated_at DESC").
        Limit(1).
        Scan(ctx)
    if err != nil {
        // No connection → graceful exit (user is on managed, or hasn't
        // connected yet; the UI will show 'Built (unsigned)' and prompt
        // them to connect).
        return false, nil
    }
    signReq := requests.SignIPARequest{
        ProjectID:     task.ProjectID.String(),
        BuildID:       task.BuildID.String(),
        CredentialsID: conn.ID.String(),
    }
    if _, err := c.SignUnsignedIPATask(&user, signReq); err != nil {
        return false, err
    }
    return true, nil
}
```

Adjust field names if they differ (`task.Platform`, `task.BuildID`, `SignIPARequest` field names — match what the existing `SignUnsignedIPATask` already accepts).

- [ ] **Step 3: Call the helper from `UpdateBuildTaskStatus`**

In `internal/controllers/reports.go`, at the end of `UpdateBuildTaskStatus`, after all existing work is done and the status has been persisted, add:

```go
if isBuiltStatus(reportBuildReq.Status) {
    signed, err := c.AutoSignIOSBuildIfPossible(ctx, task)
    if err != nil {
        slog.Warn("auto-sign enqueue failed", "task_id", task.ID, "err", err)
    } else if signed {
        slog.Info("auto-sign enqueued", "task_id", task.ID)
    }
}
```

`isBuiltStatus` — if no helper exists, inline `strings.EqualFold(reportBuildReq.Status, "Built")`.

- [ ] **Step 4: Verify build**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

- [ ] **Step 5: End-to-end check against a dev build**

Easiest way: seed a fake built iOS task row and trigger the report path.

```bash
# Open psql and insert a fake iOS AppBuildTask with status='Built' for an existing project
# whose user has an AppleConnection row. Grab the task id.
# Then hit the report endpoint:
SECRET="<REPORT_API_SECRET>"
TASK_ID="<the fake task id>"
curl -s -X POST http://localhost:8001/v1/reports/build \
  -H "Content-Type: application/json" \
  -d "{\"status\":\"Built\",\"message\":\"iOS build completed\",\"secret\":\"$SECRET\",\"task_id\":\"$TASK_ID\"}"
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "SELECT id, build_id, status FROM sign_ipa_tasks ORDER BY created_at DESC LIMIT 1"
```

Expected: a fresh row in `sign_ipa_tasks` for that build, created within seconds of the curl.

- [ ] **Step 6: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/controllers/reports.go internal/controllers/deployment.go
git commit -m "feat(ios): auto-enqueue sign task when iOS build completes

On successful iOS build, the report webhook looks up the project owner's
most-recently-used Apple connection and enqueues a sign task for that
build. If the user has no Apple connection (managed track, or not
connected yet), the build stays in Built state and the UI surfaces
'Connect Apple to sign'.

The existing /v1/deployments/tools/sign_ipa endpoint still works for
manual re-signing and is unaffected."
```

---

## Task 8: Add `build_defaults` to project settings + backfill

**Why:** The new Launchpad's one-click "New build" needs per-project defaults. No more re-entering app name / icon / splash per build.

**Files:**
- Create: `internal/database/build_defaults_backfill.go`
- Modify: `cmd/main.go` (call the backfill at startup, gated)

- [ ] **Step 1: Write the backfill**

Create `internal/database/build_defaults_backfill.go`:

```go
package database

import (
	"context"
	"log/slog"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// BackfillBuildDefaults scans each AppProject and writes a
// settings.build_defaults object derived from the project's
// most-recent successful Build row. Idempotent: skips projects whose
// settings already have a build_defaults key.
//
// Run once at startup. Logs progress and per-project errors at Warn
// level so a single bad row doesn't stall the rest.
func BackfillBuildDefaults(ctx context.Context, db *bun.DB) error {
	var projects []models.AppProject
	if err := db.NewSelect().Model(&projects).Scan(ctx); err != nil {
		return err
	}

	filled := 0
	skipped := 0
	for _, p := range projects {
		if p.Settings != nil {
			if _, hasDefaults := p.Settings["build_defaults"]; hasDefaults {
				skipped++
				continue
			}
		}
		// Find the latest successful build for this project. Builds are
		// linked to v1 projects (Build.ProjectID is v1 project id, not
		// AppProject id). Use app_project.settings.v1_project_id to
		// find the v1 project id if present; else skip.
		v1, ok := p.Settings["v1_project_id"].(string)
		if !ok || v1 == "" {
			skipped++
			continue
		}
		var latest models.Build
		err := db.NewSelect().
			Model(&latest).
			Where("project_id = ?", v1).
			Order("created_at DESC").
			Limit(1).
			Scan(ctx)
		if err != nil {
			// No builds for this project yet — nothing to backfill.
			skipped++
			continue
		}

		defaults := map[string]interface{}{
			"app_name":             latest.Title,
			"icon_s3_key":          latest.Icon,
			"splash_color":         latest.SplashScreenColor,
			"navbar_style":         latest.NavBarStyle,
			"remove_wemob_brand":   latest.RemoveWemobBranding,
		}
		if p.Settings == nil {
			p.Settings = map[string]interface{}{}
		}
		p.Settings["build_defaults"] = defaults

		if _, err := db.NewUpdate().
			Model(&p).
			Column("settings").
			Where("id = ?", p.ID).
			Exec(ctx); err != nil {
			slog.Warn("backfill build_defaults: update failed", "project_id", p.ID, "err", err)
			continue
		}
		filled++
	}
	slog.Info("backfill build_defaults complete", "filled", filled, "skipped", skipped)
	return nil
}
```

- [ ] **Step 2: Create the marker table via raw SQL in the migrations block**

In `cmd/main.go`'s migrations block, append:

```go
`CREATE TABLE IF NOT EXISTS schema_ship_markers (
    name text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT current_timestamp
)`,
```

- [ ] **Step 3: Gate the backfill at startup**

In `cmd/main.go`, after the seed annotations call, add:

```go
// Run the build_defaults backfill exactly once per deploy cluster.
// schema_ship_markers records the fact so restarts don't re-scan every
// app_project.
var markerExists bool
err := bundb.NewSelect().
    ColumnExpr("EXISTS (SELECT 1 FROM schema_ship_markers WHERE name = ?)", "build_defaults_backfill_v1").
    Scan(ctx, &markerExists)
if err != nil {
    slog.Error("check build_defaults marker failed", "err", err)
} else if !markerExists {
    if err := database.BackfillBuildDefaults(ctx, bundb); err != nil {
        slog.Error("backfill build_defaults failed", "err", err)
    } else {
        if _, err := bundb.NewRaw(
            "INSERT INTO schema_ship_markers (name) VALUES (?)",
            "build_defaults_backfill_v1",
        ).Exec(ctx); err != nil {
            slog.Warn("mark build_defaults_backfill_v1 failed", "err", err)
        }
    }
}
```

- [ ] **Step 4: Verify**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go run cmd/main.go &
SERVER_PID=$!
sleep 8
kill $SERVER_PID

# Verify backfill populated defaults on projects that had builds
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "
SELECT
  p.id,
  p.settings->'build_defaults' AS defaults
FROM app_projects p
WHERE p.settings ? 'build_defaults'
LIMIT 5;
"
# Verify marker
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "SELECT * FROM schema_ship_markers"
```

Expected: up to 5 rows with `build_defaults` populated; marker row present with `build_defaults_backfill_v1`.

Run `go run cmd/main.go` a second time — the backfill should NOT re-run (logs won't show "backfill complete"):

- [ ] **Step 5: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/database/build_defaults_backfill.go cmd/main.go
git commit -m "feat(projects): backfill settings.build_defaults from latest build

Writes {app_name, icon_s3_key, splash_color, navbar_style,
remove_wemob_brand} onto each AppProject.settings as 'build_defaults'.
Phase 2's one-click 'New build' reads these instead of asking the user
for app name + icon URL on every build.

Guarded by a schema_ship_markers row so the backfill runs exactly once
per deploy cluster. Projects that didn't have build_defaults can still
be patched manually by deleting the marker row and restarting."
```

---

## Task 9: Add `fastlane precheck` to the `ios-deploy` workflow

**Why:** Replace heuristic client-side pre-submit checks with Apple's own rule engine. Catches URL-in-description, private APIs, "beta" in copy — things our heuristics can't.

**Files:**
- Modify: `../wemob-flutter-template/codemagic.yaml` (the `ios-deploy` workflow)

- [ ] **Step 1: Insert the precheck step in `ios-deploy`**

Open `/Users/knid/Projects/wemob/wemob-flutter-template/codemagic.yaml`. Find the `ios-deploy` workflow section and locate the `Create Fastfile + deploy` script (around line 637). This script generates a Fastfile with a `deliver` lane.

Modify the generated Fastfile so the lane runs `precheck` *before* the binary-upload `deliver`. Replace the generated Fastfile body with:

```ruby
default_platform(:ios)
platform :ios do
  lane :deploy do
    api_key = app_store_connect_api_key(
      key_id: ENV['CM_KEY_ID'],
      issuer_id: ENV['CM_ISSUER_ID'],
      key_filepath: "/tmp/AuthKey.p8",
      duration: 1200,
      in_house: false,
    )

    # Pre-submit validation against Apple's metadata rules.
    # precheck exits non-zero on hard blockers (private APIs, future
    # dates, "beta"/"test" strings, URL-in-description), which fails the
    # Codemagic step before any binary/metadata upload runs.
    precheck(
      api_key: api_key,
      app_identifier: ENV['CM_PACKAGE_NAME'],
      team_id: ENV['CM_TEAM_ID'],
      negative_apple_sentiment: true,
      placeholder_text: true,
      future_functionality: true,
      other_platforms: true,
      free_stuff_in_iap: true,
      curse_words: true,
      unreachable_urls: true,
      default_rule_level: :error,
    )
```

(Keep the existing `if [ "$UPLOAD_BINARY" = "true" ]` / `deliver(...)` blocks that follow.)

- [ ] **Step 2: Handle precheck failure as a structured report**

In the publishing scripts at the bottom of `ios-deploy`, extend the Report result step so precheck failures are annotated:

Replace the existing "Report result" body with:

```bash
if [ -f ~/SUCCESS ]; then
  S="Deployed"; M="iOS deployed"; STAGE=""
else
  S="error"
  # If the Codemagic log contains precheck's rule list, surface that as
  # a distinct stage so the backend annotator can map it to a clearer
  # message ("App Store metadata precheck found issues").
  if grep -q "fastlane_version: 2" ~/deploy.log 2>/dev/null && grep -q "precheck" ~/deploy.log 2>/dev/null; then
    STAGE="precheck"
    M="App Store metadata precheck failed"
  else
    STAGE="deliver"
    M="iOS deploy failed"
  fi
fi
PAYLOAD=$(jq -n \
  --arg s "$S" \
  --arg m "$M" \
  --arg stage "$STAGE" \
  --arg secret "$REPORT_API_SECRET" \
  --arg dep "$CM_DEPLOYMENT_ID" \
  '{status:$s, message:$m, stage:$stage, secret:$secret, deployment_id:$dep, platform:"ios"}')
curl -sS -X POST "$REPORT_API_URL/v1/reports/deploy" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD" || true
```

Also `tee` the fastlane output to `~/deploy.log` in the `Create Fastfile + deploy` script so the grep above has content. Change the `fastlane ios deploy` line to `fastlane ios deploy 2>&1 | tee ~/deploy.log`.

- [ ] **Step 3: Commit the Codemagic yaml change**

```bash
cd /Users/knid/Projects/wemob/wemob-flutter-template
git add codemagic.yaml
git commit -m "feat(ci): run fastlane precheck before App Store deliver

precheck validates metadata against Apple's own rule set (private APIs,
future-dated content, 'beta'/'test' strings, unreachable URLs,
placeholder text) and exits non-zero on hard blockers, stopping the
upload before credits are spent on a doomed submission.

Failure is tagged with stage=\"precheck\" in the report payload so the
backend can render a targeted message instead of a generic 'deploy
failed'."
```

- [ ] **Step 4: Add a precheck annotation to the seed**

Back in `wemob-backend`, extend `internal/database/annotations_seed.go` with a sixth entry:

```go
{
    Name:          "app_store_precheck_failed",
    LogRegex:      `(?i)(App Store metadata precheck failed|precheck failed)`,
    HumanMessage:  "Apple's metadata precheck rejected the listing before upload.",
    SuggestedFix:  "Common causes: URL in description, the word 'beta' or 'test' in the title, placeholder copy, or a privacy/support URL that returns a non-200. Fix the listing in the Launchpad and re-submit.",
    RetryStrategy: "",
    Enabled:       true,
},
```

- [ ] **Step 5: Verify seed now has 6 entries**

Delete the pattern from the DB so the seed re-inserts, then restart:

```bash
cd /Users/knid/Projects/wemob/wemob-backend
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "DELETE FROM build_annotations WHERE name = 'app_store_precheck_failed'"
go run cmd/main.go &
SERVER_PID=$!
sleep 5
kill $SERVER_PID
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "SELECT count(*) FROM build_annotations WHERE enabled"
```

Expected: 6.

- [ ] **Step 6: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/database/annotations_seed.go
git commit -m "feat(builds): seed app_store_precheck_failed annotation

Paired with the codemagic.yaml change that runs fastlane precheck
before App Store upload — when precheck fails, the deploy report
carries stage=\"precheck\" and the regex on the annotation renders a
targeted message instead of a raw log dump."
```

---

## Task 10: End-to-end smoke

**Why:** Verify Phase 1 as a whole before declaring done.

- [ ] **Step 1: Verify both canonical and legacy connection routes respond identically**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
sleep 6
TOKEN="<valid-bearer>"

# List on both routes
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8001/v1/connections/apple/ > /tmp/new.json
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8001/v1/credentials/appstore/ > /tmp/old.json
diff /tmp/new.json /tmp/old.json

curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8001/v1/connections/google-play/ > /tmp/new-g.json
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8001/v1/credentials/google/ > /tmp/old-g.json
diff /tmp/new-g.json /tmp/old-g.json
```

Expected: both diffs show no output (identical).

- [ ] **Step 2: Verify the annotation match on a failed build report**

```bash
SECRET="<REPORT_API_SECRET>"
# Pick an existing app_build_tasks.id
TASK_ID="<id>"
curl -s -X POST http://localhost:8001/v1/reports/build \
  -H "Content-Type: application/json" \
  -d "{\"status\":\"error\",\"message\":\"Error running pod install\",\"secret\":\"$SECRET\",\"task_id\":\"$TASK_ID\"}"
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "
SELECT t.id, a.name AS annotation
FROM app_build_tasks t
LEFT JOIN build_annotations a ON a.id = t.annotation_id
WHERE t.id = '$TASK_ID';
"
```

Expected: `annotation = pod_install_failed`.

- [ ] **Step 3: Verify auto-sign enqueues on successful iOS build**

```bash
# Pick an iOS task id whose project owner has an apple_connections row.
TASK_ID="<id>"
curl -s -X POST http://localhost:8001/v1/reports/build \
  -H "Content-Type: application/json" \
  -d "{\"status\":\"Built\",\"message\":\"iOS build completed\",\"secret\":\"$SECRET\",\"task_id\":\"$TASK_ID\"}"
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "
SELECT id, build_id, status, created_at
FROM sign_ipa_tasks
ORDER BY created_at DESC
LIMIT 1;
"
```

Expected: a fresh sign_ipa_tasks row within the last few seconds, its `build_id` equal to the build_id on the triggered task.

- [ ] **Step 4: Verify deployment_events table exists and is queryable**

```bash
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "\d deployment_events"
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "SELECT count(*) FROM deployment_events"
```

Expected: table exists with the right columns; count is 0 (no writers yet — that lands in Phase 5).

- [ ] **Step 5: Verify build_defaults backfill**

```bash
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "
SELECT
  count(*) FILTER (WHERE settings ? 'build_defaults')    AS with_defaults,
  count(*) FILTER (WHERE NOT (settings ? 'build_defaults')) AS without_defaults
FROM app_projects;
"
PGPASSWORD=wemob psql -h localhost -U wemob -d wemob -c "SELECT * FROM schema_ship_markers"
```

Expected: `with_defaults` is > 0 for any project that has at least one build; `without_defaults` covers fresh/never-built projects. Marker row present.

- [ ] **Step 6: Final commit (if any stragglers)**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git status
# If anything is uncommitted:
git add -A
git commit -m "chore(ship): phase 1 smoke-test fixes"
```

---

## Phase completion criteria

Phase 1 is done when all of the following are true:

- `go build ./...` and `go vet ./...` are clean.
- Both `/v1/connections/*` and the legacy `/v1/credentials/*` routes return identical payloads.
- Triggering an `error` build report with "Error running pod install" tags the task with `annotation_id` pointing at `pod_install_failed`.
- Triggering a `Built` iOS report for a user with an Apple connection results in a new `sign_ipa_tasks` row.
- `deployment_events` and `build_annotations` tables exist.
- Seed has 6 rows in `build_annotations`.
- `schema_ship_markers` has `build_defaults_backfill_v1` and populated `app_projects.settings.build_defaults` for built projects.
- Codemagic `ios-deploy` workflow runs `fastlane precheck` before `deliver` (validate by inspecting the generated Fastfile on a real run, or by reading the merged yaml).

No user-visible behavior change. Phase 2 (Launchpad surface) is unblocked.
