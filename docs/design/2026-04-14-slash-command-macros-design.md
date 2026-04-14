# Slash Command Macros — Design Spec

**Date:** 2026-04-14
**Status:** Approved — ready for implementation plan
**Repos touched:** `wemob-backend`, `wemob-dashboard-v2`

## Problem

The Studio chat panel already supports `/`-prefixed commands (`/fix`, `/explain`,
`/refactor`, `/ui`, `/test`, `/deploy`, `/clear`). The five non-action commands
are currently **insert-type**: picking `/fix` replaces the textarea contents
with a long templated prompt which is then visible in the chat and sent
verbatim. This has three problems:

1. The long templated prompt clutters the chat history. A user who typed
   `/plan a blog` wants the log to read `/plan a blog`, not a 400-word
   instruction block.
2. The templates live in i18n JSON alongside UI copy, making it awkward to
   reference system-prompt context or keep them in sync with the agent's
   behavior.
3. The catalog doesn't cover what Studio users actually want: planning without
   building, focused bugfixes, design-only passes, SEO/a11y sweeps, etc.

## Goal

Rebuild slash commands as **server-side macros**. The frontend sends the
literal text (`/plan a blog`); the backend expands it into a full prompt
right before the LLM call. The persisted `AppChatMessage.Content` is always
the literal text, so the chat timeline — live and on reload — shows exactly
what the user typed.

## Non-goals

- No changes to the existing `/deploy` and `/clear` action commands.
- No database migration. `AppChatMessage` stores the literal text, unchanged.
- No test suite work. The project has no Go test files; verification is by
  manual end-to-end testing through the Studio UI.
- No internationalization of prompt templates. The expanded prompts are LLM
  input, not UI copy — they live in Go constants and stay in English.

## Architecture

```
[user types "/plan a blog"]
        │
        ▼
chat-panel.tsx
  • handleSend() sends literal text
        │
        ▼
use-app-chat.sendMessage()
  • addMessage("user", "/plan a blog")         ← local state: LITERAL
  • apiClient.chatApp(appId, "/plan a blog")   ← request body: LITERAL
        │
        ▼
POST /v2/apps/{id}/chat
  • agent.Run(project, user, message="/plan a blog", …)
        │
        ├── InsertChatMessage(Content="/plan a blog")   ← DB: LITERAL
        │
        ├── BuildSystemMessages(project, fileMap)
        │
        ├── convertHistory(history)
        │     └── for each user message:
        │           content = ExpandSlashCommand(content)   ← LLM: EXPANDED
        │
        ├── userTurn.Content = ExpandSlashCommand(message)  ← LLM: EXPANDED
        │
        └── (optional) inject system addendum for modal
            commands (/plan, /explain, /review) — current turn only
```

**Core invariant:** `AppChatMessage.Content` in the database is always the
literal user input. Expansion is a pure function applied at the boundary
between history/current-turn and the LLM. UI and persistence never see the
expanded form.

### Why current-turn-only system addendums

Modal commands like `/plan` need a stronger constraint than the expanded
prompt body alone can carry: "do NOT write any files this turn." If we
injected that addendum whenever historical `/plan` messages replayed, the
agent would be blocked from writing code even after the user said "ok,
build it now." Instead, the addendum is only added to the current turn, as
a temporary system block appended just before the current user turn in
`llmMessages`. Historical `/plan` messages contribute the expanded prompt
body but not the addendum.

## Command catalog (v1)

| Command          | Args     | Available when  | Kind     |
|------------------|----------|-----------------|----------|
| `/plan <topic>`  | required | always          | macro    |
| `/fix <bug>`     | required | always          | macro    |
| `/redesign <area>` | required | always        | macro    |
| `/explain <thing>` | required | always        | macro    |
| `/review`        | none     | always          | macro    |
| `/seo`           | none     | always          | macro    |
| `/a11y`          | none     | always          | macro    |
| `/seed`          | none     | project has db  | macro    |
| `/deploy`        | none     | always          | action   |
| `/clear`         | none     | always          | action   |

**Removed from current catalog:** `/refactor`, `/ui`, `/test`. They overlap
with `/redesign`/`/review` and `/test` has no supporting infrastructure in
generated apps.

### Prompt templates (finalized copy)

Templates are Go constants in `internal/agent/slash_commands.go`. `{{.Args}}`
is the user's argument string (trimmed). Addendums are injected as a separate
`{Role: "system"}` message on the current turn only.

**`/plan <topic>`**
> Expanded prompt:
> `PLAN MODE — The user wants to plan: "{{.Args}}". Produce a concise plan,
> under 400 words, with these sections: (1) Key pages and routes, (2) Primary
> user flows, (3) Data model sketch, (4) Design direction (visual style, mood,
> typography), (5) Suggested first build step. Do NOT generate or modify any
> files. End with: "Reply 'continue' when you're ready to build."`
>
> Addendum (current turn): `CURRENT TURN IS PLANNING ONLY. You MUST NOT call
> write_file, edit_file, or delete_file on this turn. Respond with a plan
> only.`

**`/fix <bug>`**
> `FOCUSED BUGFIX — Fix only this specific issue: "{{.Args}}". Change the
> minimum set of files required. Do not redesign, do not refactor unrelated
> code, do not add features. If the root cause is unclear, ask one clarifying
> question instead of guessing.`

**`/redesign <area>`**
> `VISUAL REDESIGN — Redesign this area: "{{.Args}}". Improve layout,
> typography, spacing, color, and polish. Preserve all existing logic,
> routes, data flow, state, and component APIs. Do not rename props or
> change behavior. Visual changes only.`

**`/explain <thing>`**
> Expanded prompt:
> `EXPLAIN MODE — Walk me through how this works: "{{.Args}}". Reference
> specific files and line numbers. Be concrete. Do NOT modify any files.`
>
> Addendum (current turn): `CURRENT TURN IS READ-ONLY. You MUST NOT call
> write_file, edit_file, or delete_file on this turn. Respond with an
> explanation only.`

**`/review`**
> Expanded prompt:
> `REVIEW MODE — Audit the current app and report findings grouped by:
> Bugs, Accessibility, SEO, Performance, Design. Within each group, rank
> by impact. Be specific — cite file paths. Do NOT fix anything — the user
> will pick what to address.`
>
> Addendum (current turn): `CURRENT TURN IS READ-ONLY. You MUST NOT call
> write_file, edit_file, or delete_file on this turn. Respond with an
> audit report only.`

**`/seo`**
> `SEO OPTIMIZATION PASS — Improve the app's SEO: meta tags (title,
> description, canonical), Open Graph and Twitter Card tags, sitemap.xml,
> robots.txt, semantic headings, and JSON-LD structured data where sensible.
> Do not touch visual design or business logic.`

**`/a11y`**
> `ACCESSIBILITY PASS — Improve accessibility: ARIA labels where needed,
> alt text on images, semantic HTML, keyboard navigation, visible focus
> states, sufficient color contrast. Preserve the existing visual design —
> do not restyle components.`

**`/seed`**
> `SEED DATA REFRESH — Regenerate the database seed data with fresh,
> realistic content appropriate to the app's domain. Use the seed_database
> tool.`

## Backend implementation

### New file: `internal/agent/slash_commands.go`

```go
package agent

import (
    "strings"
    "text/template"

    "github.com/knid/wemob/internal/models"
)

type slashCommand struct {
    Name          string
    RequiresArgs  bool
    Template      *template.Template  // parsed from PromptTemplate
    SystemAddendum string              // "" if none
    AvailableFor  func(*models.AppProject) bool  // nil → always
}

var slashCommands map[string]*slashCommand  // populated in init()

// ExpandSlashCommand rewrites "/cmd args..." into the full expanded prompt.
// Returns the expanded text, the matched command name, and ok=true on hit.
// On miss (unknown command, or required-args command with empty args), it
// returns the input unchanged with ok=false.
func ExpandSlashCommand(text string) (expanded string, cmd string, ok bool) { … }

// SlashCommandSystemAddendum returns the optional extra system block for
// the current turn. Returns "" if the text is not a known command or the
// command has no addendum.
func SlashCommandSystemAddendum(text string) string { … }

// parseSlashCommand splits "/cmd rest of line" into (cmd, args). It is
// case-insensitive on the command name. Returns ("", "", false) if the
// text does not start with a "/" followed by a word.
func parseSlashCommand(text string) (cmd string, args string, ok bool) { … }
```

### Changes to `internal/agent/run.go`

Three touch points, all within `Run()`:

1. **Step 7+8 (unchanged):** Persist the literal message as-is. No changes
   to the `AppChatMessage` row.

2. **Step 9 (history conversion):** Update `convertHistory(history)` so that
   each user-role historical message has its text content passed through
   `ExpandSlashCommand` before being emitted as `utils.Message`. Assistant
   messages are unchanged.

3. **Step 9 (current turn):**
   ```go
   expandedMsg, _, _ := ExpandSlashCommand(message)
   userTurn := utils.Message{Role: "user", Content: expandedMsg}
   if len(images) > 0 {
       userTurn.Images = images
   }
   if addendum := SlashCommandSystemAddendum(message); addendum != "" {
       llmMessages = append(llmMessages, utils.Message{
           Role:    "system",
           Content: addendum,
       })
   }
   llmMessages = append(llmMessages, userTurn)
   ```

Note: `tracker.TraceInput = message` stays as the literal text — the
PostHog trace then shows what the user actually typed, which is more
useful for debugging than the expansion.

### Security: input sanitization ordering

The existing `SanitizePromptContent` in `internal/utils/prompt.go` wraps
untrusted content in `<user_data>` tags to defeat prompt injection. Args
to a slash command are user input and must go through the same
sanitization. Decision: **sanitize args before substituting them into the
template**, inside `ExpandSlashCommand`. The static template body is
trusted; only `{{.Args}}` needs wrapping.

## Frontend implementation

### `src/components/studio/slash-commands.tsx`

- Drop `kind: "insert"` and the `insert` field from `SlashCommand` entirely.
- Add new kind `"macro"`:
  ```ts
  interface MacroSlashCommand {
    command: string;        // "/plan"
    label: string;          // i18n'd short label
    kind: "macro";
    requiresArgs: boolean;
    available?: (ctx: { hasDb: boolean }) => boolean;
  }
  ```
- Keep `kind: "action"` unchanged for `/deploy` and `/clear`.
- `buildSlashCommands()` rebuilt to emit the new v1 catalog (see table above).
- `matchSlashCommands(text, ctx)` takes an optional context object and
  filters out macros whose `available()` returns false. Callers pass
  `{ hasDb: project.hasDb }`.
- `SlashCommandMenu`: add a subtle right-side hint — "needs input" for
  `requiresArgs: true`, "run now" for `requiresArgs: false` — so users can
  tell them apart at a glance.

### `src/components/studio/chat-panel.tsx`

- `pickSlashCommand` rewritten:
  ```ts
  if (cmd.kind === "macro") {
    const next = cmd.requiresArgs ? `${cmd.command} ` : cmd.command;
    setValue(next);
    // focus textarea at end, same as before
    return;
  }
  // "action" branch unchanged
  ```
- `handleSend` unchanged: it already sends `value.trim()` which is the
  literal text.
- `matchSlashCommands(value)` call updated to pass `{ hasDb }`. The
  `hasDb` flag needs to be threaded down from the studio workspace — it's
  already available on the project object that the chat panel receives.

### Message rendering

No changes to the chat timeline required — the message content is already
rendered verbatim, so `/plan a blog` displays as-is. Deferred to v1.1
(nice-to-have): a subtle visual "pill" for messages whose content starts
with a known slash command, using `matchSlashCommands` as the detector.

### i18n cleanup

The existing `fixInsert`, `explainInsert`, `refactorInsert`, `uiInsert`,
`testInsert` keys in `en/studio-views.json` and `tr/studio-views.json`
become dead code and are removed. The short labels (`fix`, `explain`, etc.)
stay for commands that remain; new commands add new label keys.

## Edge cases

- **Empty args on required-args command** (`/plan` with no args): falls
  through `ExpandSlashCommand` with `ok=false`, LLM sees literal `/plan`,
  and the agent can ask the user what they want to plan. No special error
  UI.
- **Unknown command** (`/wat hello`): falls through unchanged, LLM sees
  literal `/wat hello`. Harmless.
- **Slash not at start of line** (`hey /plan`): not a command. `parseSlashCommand`
  only matches when text starts with `/`. No expansion.
- **Case** (`/Plan a blog`): command name matching is case-insensitive.
  Args are preserved with original case.
- **Mid-stream reload:** history is re-loaded from DB, re-expanded. LLM
  context is stable across reloads.
- **History size limit (30):** unchanged. Expansion adds tokens on user
  turns only; the added tokens per command are small (~100 tokens each).
- **`/seed` typed manually on a no-db app:** frontend hides it from the
  menu but cannot prevent manual typing. Backend still expands it and the
  agent's `seed_database` tool will simply return "no database
  configured", which the agent relays to the user. Acceptable.
- **Stop token in args:** args go through `SanitizePromptContent` inside
  the expansion, so they can't break out of the template.

## Rollout

Single PR, both repos. No migration, no feature flag — the old
insert-commands were already user-facing and the new behavior is strictly
better (shorter chat log). The switch is a frontend-visible change, so the
change description in the PR should include a short "what changed in the
chat" note.

## Verification plan

No Go test suite exists. Verification is manual, via the Studio dev
environment:

1. **Literal persistence:** Type `/plan a blog`, confirm the chat bubble
   reads `/plan a blog`. Reload the page, confirm it still reads `/plan a
   blog`.
2. **Plan mode:** Confirm the agent responds with a plan and does not call
   any file-mutation tools (check the tool call timeline).
3. **Post-plan build:** After `/plan`, send "ok, build it" — confirm the
   agent now does write files (i.e. addendum was current-turn-only).
4. **Fix mode:** Introduce an obvious bug, type `/fix <bug>`, confirm a
   minimal diff.
5. **Redesign:** Type `/redesign hero section`, confirm only styling
   changes, no logic edits.
6. **Review:** Type `/review`, confirm a grouped audit with no file
   writes.
7. **SEO / a11y:** Type `/seo` then `/a11y`, confirm targeted changes.
8. **Seed:** On a db-enabled project, confirm `/seed` appears in the menu
   and triggers `seed_database`. On a non-db project, confirm it's hidden
   from the menu.
9. **Unknown command:** `/wat` — confirm agent sees it literally and
   responds reasonably.
10. **History round-trip:** In a multi-turn conversation with `/plan` in
    turn 1 and a regular edit in turn 5, confirm the agent behaves
    correctly (no lingering plan-mode block).
