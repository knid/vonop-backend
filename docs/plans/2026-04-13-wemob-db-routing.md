# Wemob DB Routing — Default to Built-In, Respect Explicit Provider Asks

**Date:** 2026-04-13
**Owner:** knid
**Status:** in-progress

## Why

Reproduction: PostHog LLM trace
[`27efe84e-22d8-418d-b601-6df952117938`](https://eu.posthog.com/project/94328/llm-observability/traces/27efe84e-22d8-418d-b601-6df952117938)

A Turkish blog project (no DB connected, `project.Settings.has_database` false)
got the user message **"veritabanı kullan"** ("use a database"). The agent
responded **"Şimdi Supabase entegrasyonunu kuruyorum"** ("Now I'm setting up
Supabase") and went on to scaffold `lib/supabase.ts`, `schema.sql`,
`lib/db.ts`, and `app/api/admin/*` routes across 25 LLM calls and ~337s.

Root cause:

1. `internal/agent/prompt.go` `BasePrompt` documents `### Supabase` (and
   Firebase) under `## Integrations` as the canonical "how to wire a database"
   reference. There is no competing block telling the agent that wemob has a
   built-in database.
2. `DatabaseAddendum` only injects when `project.Settings["has_database"] ==
   true`, which is never set for a regular project — only template-copy with
   DataModels flips it (`controllers/app_template_controller.go:314`).
3. The agent has no `provision_database` tool, so even if it wanted to use
   wemob db on demand it has no path. `controllers/neon_controller.go:24`
   `provisionNeonDatabase` exists and works — it just isn't reachable from
   the agent loop.

The product call: when the user says something **vague** (any "add a db",
"store data", "make it dynamic" phrasing in any language), default to wemob
db. When they **explicitly** name a provider (Supabase, Firebase, "I have a
Postgres URL", etc.), respect it. The Supabase wiring docs in BasePrompt stay
as a reference for the explicit-ask path.

## Phase 1 — Prompt routing rule (this PR, prompt-only)

Edits:

1. `internal/agent/prompt.go`
   - Insert a new `## Database Choice` section under `# CONSTRAINTS`,
     immediately above `### Supabase`.
   - That section contains:
     - A **routing rule**: explicit provider name → use the per-integration
       wiring docs below; vague request → wemob db.
     - A **`### Wemob DB`** subsection telling the agent: this is the default,
       call `provision_database` with a tables array, then write code against
       `@/lib/db`. User-facing language is always "wemob db" / "your
       database" — never Neon, Postgres, Drizzle, Supabase, Firebase.
   - Leave `### Supabase`, `### Firebase`, and the rest of the integration
     blocks intact — they're now the explicit-ask reference.

2. `internal/agent/prompt_addenda.go`
   - Rebrand `DatabaseAddendum` so user-facing language says "wemob db" /
     "your database". Keep all the Drizzle / `DATABASE_URL` /
     `lib/db/schema.ts` technical instructions exactly as-is — those teach
     code generation and are not user-facing.

Phase 1 alone teaches the model the rule but the model has no tool to actually
provision. To avoid a regression where the model calls a non-existent tool,
Phase 2 ships in the same session.

## Phase 2 — `provision_database` tool + always-on `execute_sql`

Edits:

1. `internal/agent/tools_provision_db.go` (new)
   - `ProvisionDatabaseTool` with input
     `{description: string, tables: [{name, columns: [{name, type, nullable, primary_key, references, default}]}]}`.
   - Calls `Deps.ProvisionDatabase(ctx, rc.Project, tables)`.
   - On success, writes the `lib/db.ts` template into `rc.FileMap` so the
     model can `import { db } from '@/lib/db'` immediately, and returns a
     result string telling the model: tables created, lib/db.ts injected,
     write your own `lib/schema.ts` matching the Drizzle pattern, and the
     `execute_sql` tool is now functional.

2. `internal/agent/dependencies.go`
   - Add `ProvisionDatabase func(ctx, *models.AppProject, []models.DataModel) error`
     callback to `RunDependencies`.

3. `internal/controllers/agent_deps.go`
   - Wire the callback to a new `provisionDatabaseForAgent` helper that:
     - Calls `c.provisionNeonDatabase(ctx, project, tables, userLimits, noopEvent)`
     - Calls `c.storeNeonIntegration(project, connStr, neonProjectID)`
     - Sets `project.Settings["has_database"] = true`
     - `c.DB.UpdateAppProject(project)`

4. `internal/agent/registry.go`
   - Register `ProvisionDatabaseTool` unconditionally.
   - Register `ExecuteSQLTool` unconditionally too — its existing internal
     check (`if connStr == "" { return "this project does not have a database
     connected" }`) is enough to gate it. This drops the static
     `projectHasDatabase` gate so the tool list is stable across the run.

## Phase 3 — Out of scope here

- Mid-run system-prompt rebuild so `DatabaseAddendum` injects after a
  successful provision in the *same* run instead of the next one. Not blocking
  — the inline tool result + the `lib/db.ts` injection is enough for the
  model to keep going within the run.
- `lib/schema.ts` auto-generation from DataModels. The model writes it for
  now; we can codegen it later if it's a quality issue.
- Dashboard UX for "your wemob db is now active". Backend-only here.

## Verification

- Build: `go build ./internal/agent/... ./internal/controllers/...`
- Manual repro: replay the blog scenario from the trace above. Expect:
  agent calls `provision_database`, returns a wemob-db-branded message in
  Turkish, never says "Supabase".
- Explicit-ask check: prompt with "use Supabase for the database" — expect
  the agent to follow the existing `### Supabase` wiring docs, write
  `lib/supabase.ts`, etc.
