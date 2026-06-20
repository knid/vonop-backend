# Daytona Sandbox System Redesign

## Goal

Replace the current E2B-based sandbox and preview system with a backend-owned Daytona control plane that both the user interface and the LLM agent use. The new system must make sandbox state, process state, preview state, logs, terminals, diagnostics, and recovery explicit so the LLM can manage the same live runtime the user sees.

## Current Problems

The current sandbox system is split across the backend, frontend Next.js API routes, Caddy, and agent tools. The frontend owns most provider SDK calls through `/api/e2b/*`; the backend stores project state and proxies some agent operations back to the frontend; the LLM sees only coarse `sandbox_status`, `sandbox_exec`, and log-tail tools.

This creates several product and engineering failures:

- The LLM cannot reliably understand or repair preview state.
- User preview state and agent runtime state are not represented as one authoritative state machine.
- Processes are inferred from app type and port instead of tracked as named runtime resources.
- Logs are coarse text tails rather than structured process output.
- Recovery relies on heuristic reconnect/resync/restart behavior.
- Public preview URLs leak provider-specific sandbox IDs.
- The system underuses sandbox capabilities such as PTY sessions, snapshots, LSP diagnostics, and provider lifecycle state.

## Sources

Daytona documentation reviewed: <https://www.daytona.io/docs/llms-full.txt>.

Relevant Daytona capabilities from the docs:

- Go SDK support.
- Sandbox lifecycle state, start, stop, archive, delete, and recoverability metadata.
- File system operations.
- Process execution and long-running sessions.
- PTY terminal sessions.
- Git operations.
- LSP servers and diagnostics.
- Preview/proxy URLs for sandbox ports.
- Snapshots and archives.
- Computer-use primitives for future browser or desktop automation.

## Non-Goals

- Do not make Daytona the source of truth for project source code.
- Do not preserve the current frontend-owned `/api/e2b/*` architecture.
- Do not keep E2B compatibility routes or compatibility naming in the new system.
- Do not require preview authentication in this phase.
- Do not redesign the agent's Git-backed file editing model.

## Core Decisions

### Source of Truth

Wemob's current Git/Gitea-backed project storage remains authoritative.

Daytona is runtime state only. A Daytona sandbox can be deleted, recreated, and resynced from backend source state without losing app source.

```text
Authoritative state:
  Wemob DB
  Git/Gitea repo
  app settings
  env vars

Runtime state:
  Daytona sandbox filesystem
  named processes
  preview URLs
  logs
  terminals
  runtime snapshots
```

### Runtime Topology

Each app has one shared live Daytona sandbox used by both:

- the user's live preview
- LLM sandbox tools

Separate ephemeral sandboxes are reserved for later or special cases:

- risky experiments
- parallel agent runs
- clean validation from source
- heavy artifact generation

The first implementation should focus on the shared live sandbox.

### Ownership

The backend owns sandbox lifecycle and control. The frontend becomes a client of backend sandbox endpoints.

The frontend must not import or call the Daytona SDK. Agent tools must not proxy through frontend API routes. Both user actions and LLM tools call the same backend control plane.

```text
Frontend
  -> backend sandbox API
  -> backend sandbox event stream

LLM tools
  -> backend SandboxManager

Backend SandboxManager
  -> Daytona Go SDK
```

### Migration Strategy

This is a big-bang migration.

Remove E2B as part of the first implementation:

- remove frontend `/api/e2b/start`
- remove frontend `/api/e2b/files`
- remove frontend `/api/e2b/logs`
- remove frontend `/api/e2b/agent`
- remove backend `internal/services/e2b`
- remove E2B template build scripts and E2B runtime dependency
- replace E2B-specific comments, docs, settings, and tests

All supported app types are in scope for the first cut:

- Next.js web apps
- Vite React split-web apps
- mobile native apps using Expo web and Expo Go
- backend-only apps
- split frontend plus backend apps

## Architecture

### Backend Package

Add `internal/services/sandbox`.

The package should expose a provider-neutral control plane with Daytona as the only provider implementation.

```text
internal/services/sandbox/
  manager.go
  types.go
  provider.go
  daytona/
    provider.go
    lifecycle.go
    filesystem.go
    process.go
    preview.go
    terminal.go
    diagnostics.go
    snapshots.go
  runtimeplan/
    plan.go
    nextjs.go
    vite.go
    expo.go
    backend.go
```

Provider-neutral names are required even though Daytona is the only implementation. This keeps the domain model clear and avoids repeating the E2B naming problem.

### Main Components

`SandboxManager` coordinates all sandbox operations for an app.

Responsibilities:

- create, start, stop, archive, delete, and recreate sandboxes
- sync source files from Git/Gitea into Daytona
- start, stop, restart, and inspect named processes
- resolve and persist active preview targets
- stream sandbox and process events
- provide logs and terminal sessions
- create and restore runtime snapshots
- run LSP diagnostics
- expose deterministic recovery operations

`DaytonaProvider` wraps the Daytona Go SDK.

Responsibilities:

- translate provider-neutral requests into Daytona SDK calls
- hide provider-specific URL shapes
- normalize provider errors
- return structured state for lifecycle, filesystem, process, PTY, preview, snapshot, and diagnostics operations

`RuntimePlan` describes how a project type runs in a sandbox.

Responsibilities:

- define source roots
- define sync targets
- define named processes
- define ports and readiness probes
- define install commands
- define restart behavior
- define preview kinds

## Data Model

Keep `app_projects.sandbox_id`, but introduce provider-neutral metadata. Exact storage can be either columns or JSON settings depending on the existing migration pattern.

Required fields:

```text
sandbox_provider: "daytona"
sandbox_id: string
sandbox_state: missing | creating | syncing | installing | starting | ready | degraded | stopped | archived | error
sandbox_last_sync_commit: string
sandbox_last_error: string
sandbox_updated_at: timestamp
```

Persist active preview target:

```json
{
  "url": "https://todo.preview.wemob.app",
  "label": "App preview",
  "kind": "app",
  "set_by": "user|agent|system",
  "updated_at": "2026-06-20T00:00:00Z"
}
```

Runtime process state can be computed live from Daytona and cached in Redis or DB only if needed for UI responsiveness.

## Runtime Plan

Every app maps to a runtime plan.

Common roots:

```text
/workspace/project
/workspace/backend
/workspace/artifacts
/tmp
```

Common preview ports:

```text
app frontend: 3000
backend API: 3001
Expo web: 8081
Expo Go Metro: 19000
artifacts: 7000
```

### Next.js Web

```text
root: /workspace/project
install: npm install --prefer-offline --no-audit --no-fund
process frontend:
  command: npm run dev -- --hostname 0.0.0.0 --port 3000
  port: 3000
  readiness: HTTP probe /
preview kind: app
```

### Vite React Split-Web

```text
frontend root: /workspace/project
backend root: /workspace/backend

process frontend:
  command: npm run dev -- --config vite.config.preview.ts --host 0.0.0.0 --port 3000
  port: 3000
  readiness: HTTP probe /

process backend:
  command: npm run dev -- -p 3001
  port: 3001
  readiness: HTTP probe /

preview kinds:
  app
  backend
```

The backend writes Vite preview proxy config during sync or setup when split backend proxying is needed.

### Mobile Native / Expo

```text
root: /workspace/project
install: npm install --prefer-offline --no-audit --no-fund

process expo_web:
  command: CI=1 npx expo start --web --clear --port 8081
  port: 8081
  readiness: HTTP probe /

process expo_go:
  command: CI=1 EXPO_GO_MODE=1 npx expo start --clear --port 19000
  port: 19000
  readiness: HTTP probe /

preview kinds:
  app
  expo
```

The Expo Go process must receive the public stable Expo preview URL where Metro needs to advertise reachable bundle URLs.

### Backend-Only

```text
root: /workspace/backend
install: npm install --prefer-offline --no-audit --no-fund

process backend:
  command: npm run dev -- -p 3001
  port: 3001
  readiness: HTTP probe /

preview kind:
  backend
```

The active preview for backend-only apps may default to `backend`.

### Artifacts

Artifact serving is available for all app types.

```text
root: /workspace/artifacts
process artifacts:
  command: python3 -m http.server 7000
  port: 7000
preview kind:
  artifacts
```

LLM writes under `/workspace/artifacts` may be served publicly.

## File Sync

All app source edits go through existing backend Git/Gitea-backed file tools first.

Normal source edit flow:

```text
LLM edit tool
  -> backend validates path and scope
  -> backend writes to Git/Gitea working repo
  -> backend records snapshot, milestone, and stream event
  -> backend syncs changed file into live Daytona sandbox
  -> backend restarts or reloads named processes if needed
  -> frontend receives sandbox events and preview updates
```

Daytona file writes are not used for app source. They are allowed only for:

- `/workspace/artifacts`
- `/tmp`
- generated diagnostic files
- temporary test fixtures
- runtime-only config generated by `SandboxManager`

On sandbox start or resume, backend performs a full or content-hash-aware sync from the current Git/Gitea file map.

If sync fails, source remains correct and the sandbox enters `degraded` or `error` state. The LLM and user can trigger `sandbox_sync` or `sandbox_recreate`.

## Process Model

Processes are named resources. The LLM and frontend must never guess commands or ports.

Required process names:

```text
frontend
backend
expo_web
expo_go
artifacts
```

Each process has:

```text
name
command
cwd
env
port
status: stopped | starting | ready | error
pid or provider process id
started_at
last_exit_code
last_error
readiness_probe
log_stream
restart_policy
```

Process operations:

- list
- start
- stop
- restart
- logs
- status

Daytona process sessions should be used for long-running dev servers where appropriate. Daytona PTY should be used for interactive terminal tools, not hidden preview daemons.

## Preview Model

The preview is a backend-managed state machine, not an iframe URL helper.

Preview kinds:

```text
app
backend
expo
artifacts
route
```

Backend maintains active preview target and emits changes to the frontend.

LLM tools can manage the user's preview through structured actions:

```text
preview_list
preview_set_target
preview_open_route
preview_open_artifact
```

Example `preview_open_route` behavior:

```text
1. validate route
2. resolve current app preview base URL
3. persist project active preview target
4. emit sandbox preview event
5. frontend iframe navigates to target URL
```

## Public Preview URLs

Keep `*.preview.wemob.app` as the public preview domain, but remove sandbox IDs from user-visible URLs.

Stable URL shape:

```text
https://{appSlug}.preview.wemob.app
https://{appSlug}-{kind}.preview.wemob.app
```

Examples:

```text
https://todo.preview.wemob.app
https://todo-backend.preview.wemob.app
https://todo-expo.preview.wemob.app
https://todo-artifacts.preview.wemob.app
```

Public previews stay unauthenticated in this phase.

Resolution:

```text
stable Wemob hostname
  -> backend preview resolver
  -> app/project
  -> active Daytona sandbox
  -> preview kind
  -> Daytona provider URL or reverse proxy target
```

Stable URLs must survive sandbox recreation. If a sandbox is missing or stopped, the URL should serve a Wemob-owned recovery page rather than a provider error.

Expired or missing behavior:

- inside Studio iframe: post `wemob:sandbox-expired` to parent so Studio can restart the sandbox
- direct visitor: show a public expired preview page with a path back to Wemob Studio

## Caddy and Preview Routing

The current Caddy config assumes provider-specific host labels such as `3000-{sandboxId}`. The new routing cannot rely on hostname labels alone.

Recommended routing:

```text
Caddy receives *.preview.wemob.app
  -> forwards request to backend preview resolver/proxy
  -> backend resolves host to project and provider target
  -> backend proxies to Daytona or returns recovery page
```

The backend resolver should support HTTP and WebSocket traffic because dev servers and HMR require WebSockets.

If direct backend reverse proxying is too complex in the first implementation, Caddy may call a backend internal lookup endpoint and route to the returned upstream. The preferred end state is still that backend owns the mapping.

## Backend API

Add backend endpoints under `/v2/apps/{appId}/sandbox`.

User-facing endpoints:

```text
GET    /v2/apps/{appId}/sandbox/status
POST   /v2/apps/{appId}/sandbox/start
POST   /v2/apps/{appId}/sandbox/stop
POST   /v2/apps/{appId}/sandbox/recreate
POST   /v2/apps/{appId}/sandbox/sync
GET    /v2/apps/{appId}/sandbox/events

GET    /v2/apps/{appId}/sandbox/processes
POST   /v2/apps/{appId}/sandbox/processes/{name}/start
POST   /v2/apps/{appId}/sandbox/processes/{name}/stop
POST   /v2/apps/{appId}/sandbox/processes/{name}/restart
GET    /v2/apps/{appId}/sandbox/processes/{name}/logs

GET    /v2/apps/{appId}/sandbox/previews
POST   /v2/apps/{appId}/sandbox/previews/target
POST   /v2/apps/{appId}/sandbox/previews/open-route

POST   /v2/apps/{appId}/sandbox/terminals
POST   /v2/apps/{appId}/sandbox/terminals/{id}/input
DELETE /v2/apps/{appId}/sandbox/terminals/{id}

POST   /v2/apps/{appId}/sandbox/snapshots
POST   /v2/apps/{appId}/sandbox/snapshots/{id}/restore

POST   /v2/apps/{appId}/sandbox/diagnostics
```

Internal preview resolver endpoints:

```text
GET /internal/preview/resolve?host={host}
ANY /internal/preview/proxy/{host}/{path...}
```

The exact routing shape can be refined during implementation, but the responsibility must remain backend-owned.

## Frontend Changes

Replace `useE2BPreview` with `useSandboxPreview`.

The hook should:

- start and refresh previews through backend endpoints
- subscribe to `/sandbox/events`
- render backend-managed active preview target
- show process and sync phases
- expose preview operations to existing Studio components
- preserve existing error parsing where useful

Frontend should not know Daytona SDK types or provider URL formats.

Frontend display states:

```text
missing
creating
syncing
installing
starting
ready
degraded
stopped
archived
error
```

The preview iframe source comes from backend `active_preview.url`.

## Agent Tools

Replace the current E2B-backed tools with provider-neutral sandbox tools.

Required tools:

```text
sandbox_status
sandbox_start
sandbox_sync
sandbox_recreate
sandbox_stop

process_list
process_start
process_restart
process_stop
process_logs

terminal_create
terminal_input
terminal_close

preview_list
preview_set_target
preview_open_route
preview_open_artifact

diagnostics_run

snapshot_create
snapshot_restore
```

Keep source file writes in existing Git-backed tools:

```text
write_file
write_files
edit_file
multi_edit
delete_file
```

Sandbox tools should provide the agent with a concise state card:

```text
sandbox id
provider
state
last sync commit
active preview
preview kinds and URLs
process statuses
ready ports
recent errors
available logs
recommended recovery action
```

## LLM Management Contract

The LLM must manage the user's preview only through structured backend actions.

Allowed:

- inspect sandbox state
- sync source into sandbox
- restart named processes
- open a route in the user's preview
- switch active preview target
- read process logs
- create runtime snapshots before risky runtime actions
- run diagnostics
- use PTY terminal sessions for interactive debugging

Disallowed:

- edit app source directly in Daytona
- construct provider URLs manually
- guess ports when named previews exist
- bypass backend preview state
- expose secrets from sandbox paths

## Recovery Model

Recovery must be deterministic.

Common cases:

```text
missing sandbox:
  create sandbox -> sync source -> install -> start processes -> set preview

stopped or archived sandbox:
  start sandbox -> verify files -> sync if needed -> restart processes

file drift:
  resync from Git/Gitea -> restart affected processes

process dead:
  restart named process -> stream logs -> probe readiness

compile error:
  keep sandbox alive -> expose process logs and diagnostics -> agent fixes source -> sync delta

provider error:
  mark sandbox error -> expose last provider error -> allow recreate from source

preview URL hit with missing sandbox:
  serve recovery page -> Studio can request restart
```

## Security

Public app previews remain public in this phase.

Sandbox control remains authenticated:

- user endpoints require normal app authorization
- agent tools require existing agent authorization context
- internal preview resolver endpoints require internal network or shared token where appropriate

Path restrictions:

- app source writes only through Git-backed file tools
- sandbox writes restricted to `/workspace/artifacts`, `/tmp`, and backend-generated runtime config
- secret-like paths blocked from read/write

Command restrictions:

- LLM direct process execution should remain policy-gated
- named process operations should be preferred over raw shell commands
- package manager and install commands should come from runtime plans, not arbitrary agent input

## Observability

Add structured events for:

- sandbox lifecycle transitions
- sync start/finish/failure
- process start/ready/error/exit
- preview target changes
- terminal session lifecycle
- snapshot create/restore
- diagnostics runs
- recovery operations

These events should feed:

- Studio SSE
- agent timeline
- backend logs
- PostHog or internal analytics where useful

## Testing

Backend unit tests:

- runtime plan selection for all app types
- preview hostname parsing and resolution
- source path normalization and sync target mapping
- process state transitions
- agent tool policy and validation
- recovery decision logic

Backend integration tests with fake Daytona provider:

- start sandbox for each app type
- sync full bundle and delta writes
- restart named processes
- open route and persist active preview
- recreate sandbox from source after provider error

Frontend tests:

- `useSandboxPreview` state mapping
- preview target event handling
- expired preview event handling
- Studio iframe URL update behavior

End-to-end smoke tests:

- Next.js preview starts and route opens
- Vite split-web preview starts with backend proxy
- backend-only preview starts
- mobile native Expo web preview starts
- Expo Go preview URL is generated
- agent edits a file and user preview updates
- agent opens a route in the user preview
- sandbox recreation keeps stable preview URL working

## Rollout

This is a big-bang migration, but implementation should still be internally staged behind a short-lived development flag until complete.

Cutover criteria:

- all E2B frontend routes removed
- backend no longer imports `internal/services/e2b`
- frontend no longer imports E2B SDK or calls `/api/e2b/*`
- all app types pass smoke tests
- Caddy preview routing resolves stable hostnames
- agent tools use `SandboxManager`
- public stable preview URLs work after sandbox recreation

## Open Follow-Ups

These are intentionally out of scope for the first spec but should be revisited after Daytona migration:

- authenticated or unlisted previews
- separate ephemeral sandboxes for risky agent experiments
- parallel agent run isolation
- clean validation sandboxes before deploy
- browser/computer-use verification from inside Daytona
- richer LSP-powered auto-fix workflows
- persistent terminal UI for users

## Final Shape

The finished system should make this true:

```text
Git/Gitea is the source of truth.
Daytona is the live runtime.
Backend owns sandbox state.
Frontend renders backend state.
LLM controls the same preview the user sees.
Stable Wemob preview URLs hide provider details.
Every sandbox can be recreated from source.
```
