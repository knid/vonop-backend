# AI Codegen + Chat/Edit Rewrite — `internal/agent/`

**Status:** Approved, ready for implementation planning
**Date:** 2026-04-13
**Author:** knid + Claude
**Supersedes:** the existing `StreamBuildApp` orchestrator and all related controllers, prompts, tools, parsers, and auto-fixers

---

## 1. Why

The current AI code generation and chat-edit system is ~23,000 LOC across 26 files. A full audit of `cmd/main.go`, `internal/views/`, `internal/controllers/`, `internal/constants/ai_*.go`, and `internal/utils/` found the following structural problems:

1. **One function, three code paths.** `StreamBuildApp` branches on `isInitialGeneration && ecommerce` → A, `isInitialGeneration` → B, `chat` → C. Each branch has its own orchestrator, its own prompt path, its own streaming format, its own rollback handling.
2. **Split-brain prompt system.** Two parallel chat prompt families coexist: `NEXTJS_BUILDER_CHAT_SYSTEM_PROMPT` and `NEXTJS_BUILDER_UNIFIED_CHAT_PROMPT` (`ai_builder.go:1070`, `:1179`), plus a `_LITE` variant and a second role constant `chatEditRole` that overrides `builderRole`. The "composable blocks" refactor produced more entry points instead of fewer.
3. **A 9-function auto-fixer layer in `app_codegen_controller.go`.** `autoFixImportMismatches`, `autoFixFontWeights`, `autoFixMissingUseClient`, `autoFixTSFilesWithJSX`, `autoFixMetadataInClientComponent` (with a hand-rolled JSX brace-walker at `splitLayoutIntoServerAndClient:3354`), `autoFixRootLayoutTags`, `autoFixClassNameConcat`, `autoFixUnterminatedJSXStrings`, `autoFixMissingForceDynamic`, plus `removeUseClientDirective`, `removeMetadataExport`, `stripForbiddenImports`, `repairTruncatedStructureJSON`, `fixUnescapedSQLQuotes`, `validateSeedSQL`. Each one is the code apologizing for the prompt.
4. **Format-collision hacks.** `leakedXMLRegex` (`app_builder_controller.go:31`) strips `</invoke>`, `</parameter>`, `</invoke>` from model output — the model is leaking its tool-calling XML into file blocks. `file_parser.go:28` admits the parser breaks if a file literally contains `</wemob_file>` and patches it with a same-line quoted-string heuristic. The `wemob_` prefix exists purely to dodge HTML/JSX/Anthropic collisions.
5. **Retry-on-empty sentinel hack.** `stopShortSentinel = "NO_CODE_NEEDED"` — when the model finishes "cleanly" without writing files, the system retries aggressively with a magic string so the retry can detect "actually I'm Q&A, don't replace my response."
6. **Tool sprawl.** 14 registered tool executors in `app_builder_tools.go` (2876 LOC): `preview_website`, `manage_store`, `seed_database`, `execute_sql`, `run_migrations`, `manage_env_vars`, `install_package`, `generate_image`, `take_screenshot`, `configure_mobile_app`, `search_components`, `manage_assets`, plus pre-gen helpers. Most are unused or could be expressed as code.
7. **Two streaming protocols.** Initial generation uses single-shot with `<wemob_file>` block parsing. Chat uses `StreamWithTools` with a tool loop where files arrive inside tool results AND inline. Two parsers, two state machines, two rollback paths.
8. **Pre-gen program-within-a-program.** Web search → classification → greeting → conversation loop → clarification job (separate `views/generation_jobs_view.go`) → resume. A chatbot bolted onto the front of a code generator.
9. **`utils/ai.go` is a 2291-LOC god module.** 8 different LLM call entry points, token tracker, cost estimator, rate-limit parser, tool loop, custom message marshaling, all in one file.
10. **`app_codegen_controller.go` is doing seven jobs.** Theme CSS builders, template registry, SQL parser/repair/validator, Neon provisioning, structure planning, env var detection, JSX auto-fixers. No coherent responsibility.

**TL;DR:** One mega-orchestrator branches three ways, two prompt families pretend to be one, the LLM produces broken output so 9 hand-rolled auto-fixers patch it after the fact, a sentinel-string retry hack covers model bails, format collisions force a custom tag prefix that still doesn't fully work. ~23k LOC for what is essentially "stream a prompt, apply file mutations, commit to git, snapshot/rollback."

---

## 2. Scope

**In scope (deleted and replaced):**
- `StreamBuildApp` + all three inner orchestrators (`streamSingleShotGeneration`, `streamEcommerceGeneration`, chat tool-loop)
- All 14 tool executors in `app_builder_tools.go`
- The entire prompt family in `constants/ai_*.go` (except what's needed by non-agent code)
- `FileStreamParser`, `apply_edit.go`, all 9+ auto-fixers
- Pre-gen conversation loop, clarification flow, structure planning, project memory extraction, prompt classification, mobile-intent classification, web-search pre-gen step
- `AppChatToolCall` table, `AppConversation.Messages` content (table kept; content truncated)
- All SSE events emitted by the legacy orchestrator

**Out of scope (untouched):**
- File CRUD endpoints (`GetAppFiles`, `CreateAppFile`, `UpdateAppFile`, `DeleteAppFile`)
- Manual snapshots (`AppSnapshot` table and its endpoints)
- Integrations (Neon, GitHub export, Stripe, etc.) — the models and the CRUD endpoints
- Env var management endpoints
- Deploy, domains, GitHub export, mobile builds, dashboards, ecommerce store admin
- Billing, credits, rate limiting, auth middleware
- `services/gitrepo`, `services/secret`, `services/email`, `services/crm`, `services/storage`, `services/slack`
- Prompt-caching logic in `utils/anthropic.go`
- Native Anthropic Messages API adapter
- `AppProject`, `AppGeneratedFile`, `AppSnapshot`, `MobileAppConfig`, `AppIntegration`, `AppEnvVar` models

---

## 3. Locked decisions

| # | Decision | Choice |
|---|---|---|
| Q1 | Scope | **B** — core + pre-gen. ~22k LOC delete, ~2.5k LOC new |
| Q2 | LLM interaction model | **C** — agent with tools via native Anthropic tool API |
| Q3 | Tool surface | **Balanced** — 7 base tools + conditional tools per project state |
| Q4 | Context for chat edits | **B** — file tree in system prompt, contents via `read_file` tool on demand |
| Q5 | Error feedback loop | **A** — sandbox-driven; frontend auto-posts errors as next chat message, no backend tsc |
| Q6 | Feature variants | **A** — data-driven: template files + prompt addendum + conditional tools per project type |
| Q7 | Data preservation | **B** — keep git repos + `AppProject` + file CRUD; drop chat history; new schema for agent runs |
| Q8 | Frontend compat | **A** — fresh SSE event schema; paired PR on `wemob-dashboard` |
| Cutover | Strategy | **Big bang** — one backend PR + paired dashboard PR, no feature flag |

---

## 4. Module layout

### Delete

| File | LOC |
|---|---|
| `controllers/app_builder_controller.go` | 1,709 |
| `controllers/app_builder_context.go` | 1,328 |
| `controllers/app_builder_conversation.go` | 902 |
| `controllers/app_builder_generation.go` | 905 |
| `controllers/app_builder_tools.go` | 2,876 |
| `controllers/app_builder_validation.go` | 276 |
| `controllers/app_codegen_controller.go` | 3,802 |
| `constants/ai_builder.go` | 1,212 |
| `constants/ai_conversation.go` | 247 |
| `constants/ai_structure.go` | 231 |
| `constants/ai_templates.go` | 188 |
| `constants/ai_ecommerce.go` | 185 |
| `constants/ai_ecommerce_i18n.go` | 592 |
| `constants/ai_validation.go` | 29 |
| `utils/file_parser.go` | 549 |
| `utils/apply_edit.go` | 173 |
| Dead functions in `utils/ai.go` | ~800 |
| Legacy routes in `cmd/main.go` | ~50 |
| **Total** | **~15,900** |

### Rescue before delete

Helpers that non-agent code still depends on. Move, don't delete, in the commit before `app_codegen_controller.go` is removed.

| Helper | From | To |
|---|---|---|
| `classifyProjectType`, `looksLikeEcommerce`, `fontWeightParam` | `app_codegen_controller.go` | `controllers/project_classify.go` (new) |
| `getTemplateContent`, `getEcommerceTemplateContent`, `getUITemplateContent`, `buildGlobalsCSSTheme`, `buildEcommerceGlobalsCSSTheme`, `buildEcommerceLayout`, `buildEcommerceProviders` | `app_codegen_controller.go` | `templates/content.go` (new, in existing `internal/templates/` package) |
| `DataModel`, `parseDataModels`, `marshalDataModels`, `mergeDataModelColumns` | `app_codegen_controller.go` | `models/data_model.go` (new) |
| `buildCreateTableSQL`, `buildDrizzleSchema`, `validateSeedSQL`, `splitSQLStatements`, `stripSQLStringLiterals`, `stripLeadingSQLComments`, `fixUnescapedSQLQuotes`, `isRawSQLDefault`, `neonConnStrWithEndpoint` | `app_codegen_controller.go` | `database/schema_gen.go` (new) |
| `provisionNeonDatabase`, `storeNeonIntegration`, `getNeonIntegration`, `seedNeonDatabase`, `generateAndStoreSeedImages` | `app_codegen_controller.go` | `controllers/neon_controller.go` (new) |
| `detectEnvVarsFromCode`, `computeMissingEnvVars`, `RequiredEnvVar` | `app_codegen_controller.go` | `controllers/env_detect.go` (new) |
| `augmentPackageJSON`, `isTemplateFile`, `isProtectedTemplateFile`, `isConfigFile`, `isPascalCase`, `toCamelCase` | `app_codegen_controller.go` | `templates/utils.go` (new) |

Auto-fixers (`autoFix*`, `strip*`, `repair*`, `remove*`), `runDeterministicChecks`, `PlanAppFileStructure`, `GenerateClarificationQuestions`, `PlanAppFileStructureFromScreenshot`, `ApplyStructureToProject`, `ClassifyMobileIntent`, `GenerateURLWrapperWelcome`, `splitLayoutIntoServerAndClient`, `extractInnerJSXContent`, `findClosingBrace`, `guessFileType`, `resolveFileContent`, `autoFixImportMismatches` — **not rescued, deleted outright**.

### Create

```
internal/agent/
  run.go              ~400  Run() orchestrator — single entry, initial + edit unified
  run_context.go       ~80  RunContext struct threaded through tool execution, SafeFileMap
  registry.go         ~200  Tool registry interface, base + conditional tool wiring
  tools_base.go       ~450  7 base tool executors (read/write/edit/delete/list/search/image)
  tools_db.go          ~80  Conditional: execute_sql
  tools_mobile.go      ~60  Conditional: configure_mobile_bridge
  context.go          ~150  BuildSystemPrompt, BuildFileTreeBlock, BuildToolList
  templates.go        ~120  projectType → template files + prompt addendum + extra tools
  events.go           ~180  SSE event types, EventEmitter, ring buffer for reconnect
  snapshot.go          ~80  Git pre-commit, post-commit, rollback via git reset --hard
  prompt.go           ~800  Base system prompt (mostly text)
  prompt_addenda.go   ~300  Ecommerce, mobile, auth, database addenda
  errors.go            ~60  Typed error codes for the error SSE event
```

**Total new:** ~2,960 LOC.
**Net delete:** ~12,940 LOC.

### Adapt

| File | Change |
|---|---|
| `views/app_chat_views.go` | `ChatApp` becomes thin wrapper calling `agent.Run` |
| `views/app_generate_stream_views.go` | `GenerateAppStream` same |
| `views/mobile_chat_views.go` | Same |
| `views/generation_jobs_view.go` | `StreamJob` → `StreamRun` reading from `AppAgentRun`. `ClarifyJob`, `ReplyJob` deleted along with their routes |
| `views/generation_stream_state.go` | Simplified to ~100 LOC — just the `handleStreamOrAsync` glue around the new `EventEmitter` |
| `controllers/controller.go` | Remove dead dependencies from `Controller` struct; keep only what the agent package consumes |
| `utils/ai.go` | Keep `StreamWithTools`, `Message`, `TokenTracker`, provider abstraction, `prepareChatRequest`, `buildLLMRequestBody`, `buildVisionRequestBody`, `parseLLMResponseBody`, rate-limit error parsing. Delete `GetJSONFromLLMWithMaxTokens`, `GetJSONFromLLMWithVision`, `StreamJSONFromLLMWithMessages`, `StreamTextFromLLMWithMessages` if unused by any rescued helper. Target final size: ~1,500 LOC |
| `cmd/main.go` | Remove clarification routes, remove old generation job routes, add the two new tables to `createSchema()` |

---

## 5. Agent flow (`agent.Run`)

### Signature

```go
func Run(
    ctx context.Context,
    deps *RunDependencies,
    project *models.AppProject,
    user *models.User,
    message string,
    images []string,
    emitter EventEmitter,
) error
```

`RunDependencies` holds the external services the agent package needs: `*database.Database`, `*secret.SecretManager`, `*gitrepo.GitRepoService`, `*storage.S3Storage`, `utils.LLMProvider`, credit checker factory, PostHog tracker. Constructed once in `main()` and passed through, not embedded into a god `Controller`.

### Setup phase (deterministic, no LLM)

1. **Open git repo.** `deps.GitRepo.OpenOrInit(project.ID)`.
2. **First-run template injection.** If `project.Status == "draft"` and the repo has no files yet, copy the template files for `project.ProjectType` into the repo via `templates.InjectFor(project, repo)`. Always includes `templates.BaseFilePaths` and `templates.UIFilePaths`. Ecommerce/mobile add their variant files. Auth/database add their files if `project.Settings.needs_auth` / `hasDatabase` is set.
3. **Load fileMap.** `repo.ReadAllFiles()` → `SafeFileMap` (mutex-protected map[string]string).
4. **Pre-snapshot.** `repo.Commit("agent: pre-run {runId}", allowEmpty=true)`. Save the returned SHA as `preCommit`.
5. **Create `AppAgentRun` row** with `status=running`, `preCommit`, `userMessageID=nil` (set after message insert).
6. **Insert user `AppChatMessage`** with the message, images (uploaded to S3 via `AssetsStorage.UploadFileBytePublic`), `runID` set to the new run.
7. **Update `AppAgentRun.UserMessageID`** with the inserted message ID.
8. **Load recent chat history.** `db.GetRecentChatMessages(project.ID, 30)` — last 30 messages, oldest-first.
9. **Build system prompt.** `context.BuildSystemPrompt(project)` → three cached system blocks:
   - Block 1: `BasePrompt` (stable across ALL projects of this type)
   - Block 2: `promptAddendum` (per-variant text, stable across this project's runs)
   - Block 3: `"FILE TREE\n" + BuildFileTreeBlock(fileMap)` (stable within this run)
10. **Build tool registry.** `registry.BuildFor(project, deps)` → base 7 tools + conditional tools (`execute_sql` if DB, `configure_mobile_bridge` if mobile).
11. **Build LLM message array.** `[system blocks]` + chat history + new user message (with images if vision-capable).
12. **Emit `run_start`** with `runId`, `preCommit`, `model`, `provider`, `maxTurns`, `tools`.

### Loop phase

```go
for turn := 1; turn <= maxTurns; turn++ {
    emitter.Emit("turn_start", TurnStartEvent{RunID: runID, Turn: turn})

    if !budgetOK() {
        return ErrBudgetExceeded
    }

    resp, err := utils.StreamWithTools(
        ctx, model, messages, registry.Schemas(),
        StreamCallbacks{
            OnNarration:    emitNarration,
            OnToolCallStart: emitToolCallStart,
            OnToolCallDelta: emitToolCallDelta,
            OnToolCallResult: emitToolCallResult,
        },
        registry.Execute,  // called by StreamWithTools when it needs a tool_result
    )
    if err != nil {
        return classifyErr(err)  // returns typed *AgentError
    }

    messages = append(messages, resp.AssistantMessage)
    messages = append(messages, resp.ToolResults...)

    emitter.Emit("turn_end", TurnEndEvent{
        RunID:      runID,
        Turn:       turn,
        StopReason: resp.StopReason,
        Tokens:     resp.Usage,
    })

    if resp.StopReason == "end_turn" {
        break  // model decided it's done
    }
    if ctx.Err() != nil {
        return ErrCanceled
    }
}
```

Tool execution is inline inside `StreamWithTools` — `registry.Execute(toolName, input)` is called as each `tool_use` block completes. The tool executor has access to `RunContext` (fileMap, emitter, project, user, etc.).

### Shutdown phase (success)

1. **Write fileMap to working tree.** `repo.WriteFileMap(fileMap)` — walks the map, writes each file to disk, deletes any files present on disk but missing from the map. Atomic per-file via tempfile+rename. This is the ONLY moment in the run where the git working tree is touched by the agent.
2. **Post-commit.** `repo.Commit("agent: {first_line_of_message}", allowEmpty=false)`. Save as `finalCommit`.
3. **Insert assistant `AppChatMessage`** with narration text, tool call array, `runID`.
4. **Update `AppAgentRun`**: `status=completed`, `finalCommit`, `turns`, `tokens`, `costUSD`, `durationMs`, `completedAt`.
5. **Deduct credits** via `deps.DeductCredits(user, credits, "app_chat"|"generate_app", project.ID)`.
6. **Emit `done`** with the run summary.
7. **Track PostHog event** `{action}_completed`.

### Shutdown phase (failure)

Any error inside setup/loop/shutdown:
1. **Discard the fileMap.** Tool mutations never touched disk, so dropping the in-memory map is the rollback for 99% of failures. No git operations needed.
2. **Defensive `repo.Reset(preCommit, hard=true)`** — only has an effect if the failure happened during or after the success-path write/commit (which can happen if `WriteFileMap` partially succeeded or `Commit` failed mid-write). Idempotent no-op otherwise.
3. Update `AppAgentRun`: `status=failed|canceled|budget_exceeded`, `error=err.Error()`, `completedAt`.
4. Emit `rollback` with `preCommit`, `reason`, `files_reverted` count.
5. Emit `error` with `phase`, `message`, typed `code`.
6. Still insert an assistant message ("Sorry, the run failed — your files are unchanged.") so the chat UI isn't orphaned.
7. Track PostHog event `{action}_failed`.
8. **Do not deduct credits** on failure (fixes a known bug in the old system).

Panic recovery via `defer recover()` maps to the failure path, logs stack trace.

### Budget limits

| Limit | Default | Rationale |
|---|---|---|
| `maxTurns` | 25 | Hard ceiling on agent iterations |
| `maxToolCallsPerTurn` | 20 | Prevents a single turn from exploding |
| `maxTokensPerRun` | 500,000 | Hard cost ceiling (≈$1.50 Sonnet uncached) |
| `maxWallclockMs` | 10 * 60 * 1000 | 10 minute cap |
| `maxFileSize` | 200 KB | Per-file write_file size cap |
| `maxFiles` | 200 | Max files in the project after the run |

Live credit checker (same helper as today: `MakeCreditBudgetChecker`) runs between turns and halts with `ErrBudgetExceeded` → rollback path.

### Concurrent runs

Only one `AppAgentRun` per project may be `running` at a time. Before creating a new run, `agent.Run` calls `db.GetActiveAgentRun(project.ID)`; if one exists, the call returns `ErrRunInProgress` and the view returns 409 Conflict. The frontend already handles this case via `GetAppActiveJob`.

### Reconnect

In-memory ring buffer: per `runID`, keep the last 500 events in a capped slice (guarded by a mutex). When a client reconnects via `GET /v2/apps/{appId}/runs/{runId}/stream`, replay the buffer, then tee live events. Buffer lives on `EventEmitter`. Cleared 10 minutes after run completion.

Backend restart: any `AppAgentRun` row with `status=running` on startup is marked `failed` with `error="server restarted"`. The user's next message starts a fresh run. We don't try to resume across restarts.

---

## 6. Tool surface

All tools use native Anthropic `tool_use` / `tool_result` blocks. Streaming tool input is enabled — the frontend sees file content as it arrives.

### Base tools (always present)

**`read_file`**
- Input: `{ path: string }`
- Output: `{ content: string, size: int }` or `{ error: "not_found" }` or `{ error: "too_large", size: int }`
- Reads from `RunContext.FileMap`, not disk. Truncates content > 200KB with a marker.
- Path sanitized via existing `SanitizeFilePath` helper. No traversal.
- Emits no SSE events beyond the standard `tool_call_*` trio.

**`write_file`**
- Input: `{ path: string, content: string, description?: string }`
- Output: `{ ok: true, bytes: int }` or `{ error }`
- Creates or overwrites. Writes to `RunContext.FileMap`. Emits `tool_call_delta` with content deltas as the input streams in. Emits `file_written` on completion.
- Path sanitized. Max content size 200KB.
- Protected paths (block with error): `package.json`, `tsconfig.json`, `next.config.ts`, `next.config.js`, `postcss.config.js`, `.env.local`. Model cannot overwrite core config.

**`edit_file`**
- Input: `{ path: string, old_string: string, new_string: string, description?: string }`
- Output: `{ ok: true, matched: 1 }` or `{ error: "not_found" | "no_match" | "ambiguous_match" }`
- Classic surgical edit. `old_string` must appear EXACTLY ONCE in the current file content. Error on zero matches (stale context) or multiple matches. No fuzzy matching.
- Emits `tool_call_delta` with `new_string` content. Emits `file_written` with the final content after replacement.

**`delete_file`**
- Input: `{ path: string, reason?: string }`
- Output: `{ ok: true }` or `{ error: "not_found" | "protected" }`
- Protected paths: same list as `write_file`.
- Emits `file_deleted`.

**`list_files`**
- Input: `{ glob?: string }` (e.g. `"app/**/*.tsx"`)
- Output: `{ files: [{ path, size }] }`
- Filters `RunContext.FileMap` by glob via `path.Match` (or `doublestar` lib for `**`).

**`web_search`**
- Input: `{ query: string, max_results?: int }` (max 5, default 5)
- Output: `{ results: [{ title, url, snippet }] }`
- Proxies to whatever search provider the existing `runPreGenWebSearch` used. Keep the same secret name and client code (rescued into `services/search/`).
- Results cached by query hash for 5 minutes (Redis or in-memory LRU).
- Max snippet 2KB.

**`generate_image`**
- Input: `{ prompt: string, width?: int, height?: int, style?: "photo" | "illustration" | "icon" }`
- Output: `{ url: string, width: int, height: int }`
- Calls the existing AI image gen service (keep the same secret + endpoint the old `executeChatGenerateImage` used). Uploads to S3 via `AssetsStorage.UploadFileBytePublic`. Returns public URL.
- Emits `tool_call_result` with the URL; model embeds directly in JSX.
- No placeholder markers, no post-processing.

### Conditional tools

**`execute_sql`** — injected if `project` has a connected Neon integration
- Input: `{ query: string, readonly: bool }`
- Output: `{ rows: [], row_count: int, truncated: bool }` or `{ error }`
- `readonly=true` uses `sql.TxOptions{ReadOnly: true}`. `readonly=false` requires the query to be a write (INSERT/UPDATE/DELETE/CREATE TABLE/ALTER TABLE/DROP TABLE). No deny-list — trust the readonly flag.
- 10 second timeout. Max 50 rows returned (higher results truncated with flag).
- Dispatched via `deps.Neon.Execute(project, query, readonly)` which wraps the existing Neon connection pool.

**`configure_mobile_bridge`** — injected if `project.ProjectType == "mobile_app"`
- Input: `{ method: string, enabled: bool, config?: object }`
- Output: `{ ok: true }` or `{ error: "unknown_method" }`
- Valid methods: `ALL_BRIDGE_METHODS` (keep the existing enum from `constants/ai_builder.go:672` — move to `constants/mobile_bridge.go`).
- Updates `MobileAppConfig` row directly via `deps.DB.UpdateMobileAppConfig`.

### NOT in the tool set (and why)

- **manage_store**: store state (products, categories, orders) is managed by the frontend store admin UI. The agent reaches it via the store SDK in code, not a tool.
- **install_package**: `package.json` is protected. Dependencies are pinned. New deps require a human change.
- **search_components**: component paths are listed in the system prompt. Model reads them with `read_file` if needed.
- **manage_assets**: user-uploaded assets are listed in the system prompt as a block. Model references by URL.
- **run_migrations**: model writes migration SQL via `execute_sql` with `readonly: false`.
- **manage_env_vars**: env vars are set by users in the dashboard UI. Agent references them by name in code.
- **take_screenshot**: pre-gen screenshot capture is gone (scope B).
- **preview_website**: the sandbox IS the preview. No tool needed.

### Tool executor interface

```go
type ToolExecutor interface {
    Name() string
    Schema() map[string]any  // JSON schema for Anthropic tools API
    Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (result string, isError bool, err error)
}

type Registry struct {
    tools map[string]ToolExecutor
}

func (r *Registry) Schemas() []anthropic.Tool { /* ... */ }
func (r *Registry) Execute(ctx, rc, name, input) (string, bool, error) { /* dispatch */ }

func BuildFor(project *models.AppProject, deps *RunDependencies) *Registry {
    r := NewRegistry()
    r.Register(&ReadFileTool{})
    r.Register(&WriteFileTool{})
    r.Register(&EditFileTool{})
    r.Register(&DeleteFileTool{})
    r.Register(&ListFilesTool{})
    r.Register(&WebSearchTool{deps: deps})
    r.Register(&GenerateImageTool{deps: deps})

    if projectHasDatabase(project) {
        r.Register(&ExecuteSQLTool{deps: deps})
    }
    if project.ProjectType == "mobile_app" {
        r.Register(&ConfigureMobileBridgeTool{deps: deps})
    }
    return r
}
```

---

## 7. Prompt architecture

### One base prompt, per-variant addenda

`agent/prompt.go` — single exported `BasePrompt` string constant, ~600 lines, organized as labeled sections:

```
# ROLE
# TECH STACK
# AVAILABLE TOOLS (rendered from the registry)
# DESIGN PRINCIPLES (condensed from the old builderDesignPhilosophy)
# OUTPUT PROTOCOL
    - Narrate briefly before tool calls
    - Never paste file content in narration
    - Prefer edit_file over write_file for existing files
    - Before editing a file you haven't seen this run, call read_file first
# IMAGES
    - Use generate_image for hero/product/illustration imagery
    - Never use picsum.photos, unsplash, stock URLs
    - Prefer gradients, SVG, icons, CSS patterns for decoration
# LANGUAGE (from old builderLanguageRules)
# CODE STYLE (Tailwind v4, shadcn, client/server split)
# CONSTRAINTS (protected files, max sizes, etc.)
```

`agent/prompt_addenda.go` — per-variant constants:

```go
const EcommerceAddendum = `...`    // ~40 lines: store SDK usage, cart/checkout are templates, don't rewrite
const MobileAddendum    = `...`    // ~40 lines: bridge SDK, viewport, single-column layouts
const AuthAddendum      = `...`    // ~30 lines: session helpers, protected routes
const DatabaseAddendum  = `...`    // ~40 lines: schema, execute_sql usage, common queries
```

### Composition (one function, linear, deterministic)

```go
func BuildSystemPrompt(project *models.AppProject) []anthropic.SystemBlock {
    // Block 1: base prompt (stable across all projects of this type)
    blocks := []anthropic.SystemBlock{
        { Text: BasePrompt, CacheControl: ephemeral },
    }

    // Block 2: variant addendum (stable across this project's runs)
    var addendum strings.Builder
    switch project.ProjectType {
    case "ecommerce":
        addendum.WriteString(EcommerceAddendum)
    case "mobile_app":
        addendum.WriteString(MobileAddendum)
    }
    if projectHasAuth(project) {
        addendum.WriteString("\n\n")
        addendum.WriteString(AuthAddendum)
    }
    if projectHasDatabase(project) {
        addendum.WriteString("\n\n")
        addendum.WriteString(DatabaseAddendum)
    }
    if addendum.Len() > 0 {
        blocks = append(blocks, anthropic.SystemBlock{
            Text: addendum.String(),
            CacheControl: ephemeral,
        })
    }
    return blocks
}

func BuildFileTreeBlock(fileMap *SafeFileMap) anthropic.SystemBlock {
    // Block 3: file tree (stable within this run, reused across turns)
    return anthropic.SystemBlock{
        Text: "FILE TREE\n" + formatFileTree(fileMap),
        CacheControl: ephemeral,
    }
}
```

That's the entire composition logic. No `NEXTJS_BUILDER_COMMON_RULES`, no `NEXTJS_ECOMMERCE_COMMON_RULES`, no `chatEditRole`, no `_LITE` variants.

### Prompt caching

Three cache breakpoints (max 4 on Anthropic; we use 3 to leave headroom):
1. After `BasePrompt` — stable across ALL runs, ALL projects of the same type → highest hit rate
2. After the combined prompt + addendum — stable within a project
3. After the file tree block — stable within a run, reused every turn

First run of a project type: all 3 cold. Second run same project: blocks 1+2 warm. Within a single run: all 3 warm across turns. Expected cache hit rate: 70–90% on input tokens after steady state.

### Prompt content seed

The `BasePrompt` content is NOT being invented from scratch. Seed content comes from condensing these existing constants:

| Old constant | Use |
|---|---|
| `builderRole` (`ai_builder.go:21`) | → `# ROLE` |
| `builderDesignPhilosophy` (`ai_builder.go:32`) | → `# DESIGN PRINCIPLES` |
| `builderFrameworkRulesBase` (`ai_builder.go:112`) | → `# TECH STACK` + `# CODE STYLE` |
| `builderImageRulesStandard` (`ai_builder.go:183`) | → `# IMAGES` |
| `builderLanguageRules` (`ai_builder.go:204`) | → `# LANGUAGE` |
| `builderToneRules` (`ai_builder.go:211`) | → folded into `# ROLE` |
| `builderIntegrationRules` (`ai_builder.go:219`) | → `# CONSTRAINTS` |
| `NEXTJS_BUILDER_TOOL_INSTRUCTIONS` | → DISCARDED (replaced by native tool API + `# OUTPUT PROTOCOL`) |
| `NEXTJS_BUILDER_SINGLESHOT_INSTRUCTIONS` | → DISCARDED |
| `NEXTJS_BUILDER_CHAT_INSTRUCTIONS` | → DISCARDED |
| `NEXTJS_BUILDER_UNIFIED_CHAT_INSTRUCTIONS` | → DISCARDED |
| `builderStandardDataRules` | → folded into `DatabaseAddendum` |
| `builderEcommerceDataRules` + `ecommerceStoreRules` + `builderFrameworkRulesEcommerce` + `builderImageRulesEcommerce` | → `EcommerceAddendum` |
| `NEXTJS_BUILDER_MOBILE_RULES` + `NEXTJS_BUILDER_MOBILE_CONFIG_TOOL_RULES` | → `MobileAddendum` |
| `NEXTJS_BUILDER_AUTH_RULES` | → `AuthAddendum` |
| `NEXTJS_BUILDER_DATABASE_RULES` | → `DatabaseAddendum` |

The implementation task will extract the actual text during prompt writing; the design does not freeze every line.

---

## 8. Data layer

### Tables to CREATE

```sql
CREATE TABLE app_chat_messages (
    id          uuid        PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id  uuid        NOT NULL REFERENCES app_projects(id) ON DELETE CASCADE,
    run_id      uuid        REFERENCES app_agent_runs(id) ON DELETE SET NULL,
    role        text        NOT NULL,   -- 'user' | 'assistant'
    content     text        NOT NULL,   -- plain text narration
    images      jsonb       NOT NULL DEFAULT '[]',  -- [{url, alt?}]
    tool_calls  jsonb       NOT NULL DEFAULT '[]',  -- [{id, name, input, result, is_error, duration_ms}]
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX app_chat_messages_project_created_idx ON app_chat_messages (project_id, created_at DESC);

CREATE TABLE app_agent_runs (
    id                uuid        PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id        uuid        NOT NULL REFERENCES app_projects(id) ON DELETE CASCADE,
    user_id           uuid        NOT NULL REFERENCES users(id),
    status            text        NOT NULL,  -- 'running' | 'completed' | 'failed' | 'canceled' | 'budget_exceeded'
    pre_commit        text        NOT NULL,
    final_commit      text,
    user_message_id   uuid        REFERENCES app_chat_messages(id) ON DELETE SET NULL,
    model             text        NOT NULL,
    provider          text        NOT NULL,
    turns             int         NOT NULL DEFAULT 0,
    tool_call_count   int         NOT NULL DEFAULT 0,
    prompt_tokens     int         NOT NULL DEFAULT 0,
    completion_tokens int         NOT NULL DEFAULT 0,
    cache_read_tokens int         NOT NULL DEFAULT 0,
    cache_write_tokens int        NOT NULL DEFAULT 0,
    cost_usd          numeric(10,6) NOT NULL DEFAULT 0,
    credits_used      int         NOT NULL DEFAULT 0,
    duration_ms       int         NOT NULL DEFAULT 0,
    error             text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz
);
CREATE INDEX app_agent_runs_project_created_idx ON app_agent_runs (project_id, created_at DESC);
CREATE INDEX app_agent_runs_active_idx ON app_agent_runs (project_id) WHERE status = 'running';
```

Both tables are added to `createSchema()` in `cmd/main.go` with `IfNotExists()`.

### Tables to DROP

- `app_chat_tool_calls` — dropped. Replaced by `app_chat_messages.tool_calls` JSONB.
- `app_conversations` — NOT dropped yet. Keep the table for one release cycle for rollback safety. Truncate its `messages` JSONB to `[]` as part of the SQL migration so the new system doesn't accidentally ingest legacy data via any remaining helper.

### Tables unchanged

`app_projects`, `app_generated_files`, `app_snapshots`, `app_integrations`, `app_env_vars`, `mobile_app_configs`, `app_stores`, billing/credits tables.

### DB accessors (new)

```go
// internal/database/chat_messages.go
func (d *Database) InsertChatMessage(m *models.AppChatMessage) error
func (d *Database) GetRecentChatMessages(projectID uuid.UUID, limit int) ([]models.AppChatMessage, error)
func (d *Database) ClearChatMessages(projectID uuid.UUID) error

// internal/database/agent_runs.go
func (d *Database) InsertAgentRun(r *models.AppAgentRun) error
func (d *Database) UpdateAgentRun(r *models.AppAgentRun) error
func (d *Database) GetActiveAgentRun(projectID uuid.UUID) (*models.AppAgentRun, error)
func (d *Database) GetAgentRun(runID uuid.UUID) (*models.AppAgentRun, error)
func (d *Database) MarkOrphanedRunsFailed() error  // called on server startup
```

### SQL migration script

`scripts/migrate-to-agent.sql`:

```sql
BEGIN;
-- Create new tables (idempotent, also in createSchema)
-- ... CREATE TABLE app_chat_messages ...
-- ... CREATE TABLE app_agent_runs ...

-- Wipe legacy chat state
TRUNCATE app_chat_tool_calls;
UPDATE app_conversations SET messages = '[]'::jsonb;

-- Drop legacy tool call table
DROP TABLE app_chat_tool_calls;

COMMIT;
```

Run manually via `psql` against prod as part of the deploy. Dev environment regenerates via `createSchema` on first boot.

### Revert script

`scripts/revert-to-legacy.sql` — committed alongside the migration for rollback safety. Recreates `app_chat_tool_calls` with its old schema (empty). Does NOT restore old conversation content (that data is gone). Worst case: users lose chat history but their code and projects survive.

---

## 9. SSE event schema

Envelope:
```
event: <type>
data: <json>
```

Every `data` payload includes `run_id`. 13 event types total:

| Event | When | Payload keys |
|---|---|---|
| `run_start` | First event of a run | `run_id, pre_commit, model, provider, max_turns, tools[]` |
| `turn_start` | Start of each agent turn | `run_id, turn` |
| `narration` | Streamed text deltas between tool calls | `run_id, turn, delta` |
| `tool_call_start` | Model began a tool call (input may still be streaming) | `run_id, turn, tool_call_id, tool_name, input_preview` |
| `tool_call_delta` | Progressive streaming of large tool inputs (content/new_string/old_string) | `run_id, tool_call_id, field, delta` |
| `tool_call_result` | Tool execution completed | `run_id, tool_call_id, tool_name, status, duration_ms, summary, is_error` |
| `file_written` | After successful `write_file`/`edit_file` | `run_id, path, operation, bytes, content` |
| `file_deleted` | After successful `delete_file` | `run_id, path` |
| `turn_end` | Turn completed | `run_id, turn, stop_reason, tokens{input,output,cache_read,cache_write}` |
| `rollback` | Run failed/canceled; git reset happened | `run_id, pre_commit, reason, files_reverted` |
| `error` | Any non-normal stop | `run_id, phase, message, code` |
| `done` | Run completed successfully | `run_id, final_commit, turns, tool_calls, files_changed, tokens, credits_used, duration_ms` |
| `keepalive` | Every 15s | `run_id, ts` |

12 run-lifecycle events + 1 keepalive. Old system emits 20+.

### Error codes (`error.code`)

- `CREDITS_EXHAUSTED`
- `RATE_LIMITED`
- `PROVIDER_ERROR`
- `BUDGET_EXCEEDED`
- `TOOL_FAILED`
- `GIT_ERROR`
- `INTERNAL`

### Emitter

```go
type EventEmitter interface {
    Emit(eventType string, data any) bool
    BufferForReconnect(runID uuid.UUID) []BufferedEvent
    Close()
}
```

One implementation backed by the SSE channel + an in-memory ring buffer keyed by `runID` (capacity 500 per run, shared LRU across concurrent runs).

### Routes

New:
- `GET /v2/apps/{appId}/runs/{runId}` — fetch a run's final state
- `GET /v2/apps/{appId}/runs/{runId}/stream` — replay buffer + live events for reconnect

Changed:
- `GET /v2/apps/{appId}/active-job` — returns `{ run_id, status, type }` from `app_agent_runs` where `status=running`
- `POST /v2/apps/generate/stream` — unchanged path, now calls `agent.Run`
- `POST /v2/apps/{appId}/chat` — unchanged path, now calls `agent.Run`

Removed:
- `POST /v2/apps/jobs/{jobId}/clarify`
- `POST /v2/apps/jobs/{jobId}/reply`
- Old `GET /v2/apps/jobs/{jobId}/stream` (superseded by `/runs/{runId}/stream`)

---

## 10. Cutover plan

Big bang. One backend PR + one dashboard PR landed together.

### Backend PR commit order

| Commit | Content | LOC | `go build` clean? |
|---|---|---|---|
| C1 | New models (`AppChatMessage`, `AppAgentRun`) + DB accessors + `createSchema()` entries | +400 | ✅ |
| C2 | New `internal/agent/` package, fully implemented, unused | +2,960 | ✅ |
| C3 | Rewire `views/app_chat_views.go`, `views/app_generate_stream_views.go`, `views/mobile_chat_views.go`, `views/generation_jobs_view.go`, `views/generation_stream_state.go`, `cmd/main.go` routes to call `agent.Run`. Remove clarification routes | +250, -400 | ✅ |
| C4 | Rescue helpers out of `app_codegen_controller.go` into new files (`controllers/project_classify.go`, `controllers/neon_controller.go`, `controllers/env_detect.go`, `templates/content.go`, `templates/utils.go`, `models/data_model.go`, `database/schema_gen.go`) | +2,100, -2,100 | ✅ |
| C5 | Delete the old system (all files from §4 "Delete" table) + dead functions in `utils/ai.go` + drop `AppConversation`/`AppChatToolCall` Go models + `scripts/migrate-to-agent.sql` + `scripts/revert-to-legacy.sql` | -15,900 | ✅ |

Each commit must `go build ./...` and `go vet ./...` cleanly. If C5 leaves dangling references, the fix goes in C4 and the stack is re-ordered.

### Dashboard PR

Separate repo, same day, same merge window. Content:
- New SSE client for the 11-event schema
- New chat UI with collapsible tool call cards, streaming file content renders
- Reconnect flow hitting `GET /v2/apps/{appId}/runs/{runId}/stream`
- Delete old event handlers in one commit

Merge order doesn't matter (both sides tolerate a ~minute gap).

### Pre-ship verification (manual, 12 scenarios)

Before clicking merge, run these on staging with the PR branch deployed:

1. Draft web project → first message → agent streams → files land → E2B preview loads
2. Chat edit with small change ("make the hero blue") → `edit_file` used, not `write_file`
3. Deliberately-broken request → `rollback` fires → project unchanged → credit NOT deducted
4. Draft ecommerce → template files injected → store SDK works in generated code
5. Draft mobile → mobile bridge template files land → mobile viewport rules apply
6. Draft database project → Neon provisioned → `execute_sql` tool available and works
7. E2B runtime error → frontend auto-posts → agent fixes in next turn
8. Cancel run mid-stream → `status=canceled`, git reset happens
9. Force maxTurns exceeded (toy prompt) → `budget_exceeded` fires
10. Disconnect mid-run, reconnect via `runs/{runId}/stream` → event replay works
11. `app_agent_runs` row has real tokens, cost, duration, commit hashes
12. Credits deducted correctly on success, NOT on failure

Any failure → fix in the PR branch before merging.

### Rollback of a bad deploy

Per user preference:
1. `git revert {merge_commit}` on wemob-backend
2. `git revert {merge_commit}` on wemob-dashboard
3. Apply `scripts/revert-to-legacy.sql` if `app_chat_tool_calls` is needed back

Schema migration is mostly forward-compatible; the reverse script handles the drops.

### Tests

Per CLAUDE.md: no automated test suite exists in this project, and adding one is out of scope for this rewrite. Verification is the 12 manual scenarios above plus post-deploy telemetry from `app_agent_runs` (success rate, cost distribution, duration distribution, turn distribution) compared against rolling historical baselines from the legacy system.

---

## 11. Open questions intentionally left for the implementation plan

- Exact text content of `BasePrompt` and the four addenda (content decision, not structural)
- Whether the in-memory event ring buffer needs Redis backing for multi-instance deployments
- Whether `generate_image` should batch multiple image requests in one turn (optimization, not correctness)
- Whether `read_file` output should include line numbers by default (style choice)
- Precise JSON schemas for each tool (will be written during implementation using Anthropic tool schema conventions)
- Whether to split the `AppAgentRun` token fields into a separate `app_agent_run_usage` table for normalization (tradeoff: query convenience vs row size)

None of these block the implementation plan.

---

## 12. What this buys

| Metric | Before | After | Notes |
|---|---|---|---|
| Total AI codegen LOC | ~23,000 | ~7,500 | −67% |
| Files in the system | 26 | 13 | −50% |
| LLM call entry points | 8 | 1 | `StreamWithTools` only |
| Chat prompt families | 2 (+ LITE) | 1 | One prompt, data-driven addenda |
| Orchestrator branches (init/chat/ecommerce) | 3 | 1 | Unified agent loop |
| Registered tools | 14 | 7 base + 2 conditional | −36% at max |
| Auto-fixers / output repairers | 9 + 6 repair helpers | 0 | Sandbox-driven repair |
| Streaming protocols | 2 | 1 | Native tool API only |
| SSE event types | 20+ | 13 | Schema documented |
| Format collision hacks | 3 | 0 | No `<wemob_*>`, no `leakedXMLRegex`, no `NO_CODE_NEEDED` sentinel |
| Tables touching chat state | 2 | 2 | Same count, cleaner shape |
| Pre-gen LLM calls | 3 (classify, search summary, structure plan) | 0 | Agent plans inline |
| Clarification flow endpoints | 2 | 0 | Deleted |
