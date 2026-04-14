# Slash Command Macros Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn `/`-prefixed chat commands into server-side macros — the UI shows the literal text the user typed (e.g. `/plan a blog`), while the LLM receives a fully expanded prompt.

**Architecture:** Backend-side expansion at LLM-context-build time in `agent.Run()`. `AppChatMessage.Content` stores the literal user text unchanged. A pure function `ExpandSlashCommand()` rewrites `/cmd args` into the full prompt whenever a user message is about to be fed to the LLM — both the current turn and historical turns in `convertHistory()`. Modal commands (`/plan`, `/explain`, `/review`) also inject a current-turn-only system addendum that forbids file mutations.

**Tech Stack:** Go 1.22+ (backend), text/template, React 18 + TypeScript + i18next (frontend).

**Repos:** `wemob-backend` (primary), `wemob-dashboard-v2` (frontend).

**Reference spec:** `docs/design/2026-04-14-slash-command-macros-design.md`.

**Note on tests:** `wemob-backend` has no Go test suite (per `CLAUDE.md`) and this plan does not introduce one. Verification is by a small one-off harness script (Task 1.5) and end-to-end manual testing in Studio (Phase 3). Each backend task ends with a compile check (`go build ./...`) as its fast feedback loop.

---

## Phase 1 — Backend: expansion engine

### Task 1.1: Create the slash command registry + expansion function

**Files:**
- Create: `wemob-backend/internal/agent/slash_commands.go`

- [ ] **Step 1: Create the new file with the registry skeleton**

Create `internal/agent/slash_commands.go` with exactly this content:

```go
package agent

import (
	"bytes"
	"strings"
	"text/template"

	"github.com/knid/wemob/internal/utils"
)

// slashCommand describes one server-side macro. The template body is the
// prompt the LLM actually sees after expansion. {{.Args}} is the user's
// argument string after SanitizePromptContent wrapping. SystemAddendum is
// injected as a separate system message on the *current* turn only — see
// Run() for why historical invocations do not replay the addendum.
type slashCommand struct {
	Name           string
	RequiresArgs   bool
	Template       *template.Template
	SystemAddendum string
}

// slashCommands is populated in init(). Keys are lowercase command names
// including the leading slash (e.g. "/plan").
var slashCommands = map[string]*slashCommand{}

// registerSlashCommand parses the template body and stores the command.
// Panics on a malformed template — templates are compile-time constants,
// so a parse failure is a programmer error and should crash at startup.
func registerSlashCommand(name string, requiresArgs bool, body, addendum string) {
	tmpl, err := template.New(name).Parse(body)
	if err != nil {
		panic("slash_commands: bad template for " + name + ": " + err.Error())
	}
	slashCommands[strings.ToLower(name)] = &slashCommand{
		Name:           name,
		RequiresArgs:   requiresArgs,
		Template:       tmpl,
		SystemAddendum: addendum,
	}
}
```

- [ ] **Step 2: Add the parse helper**

Append to the same file:

```go
// parseSlashCommand splits a leading "/cmd rest of line" into (cmd, args).
// Matching is case-insensitive on the command name. Returns ok=false if
// the text does not start with "/" followed by a word character or if the
// first token is just "/".
func parseSlashCommand(text string) (cmd string, args string, ok bool) {
	text = strings.TrimLeft(text, " \t")
	if len(text) < 2 || text[0] != '/' {
		return "", "", false
	}
	// Find the end of the command token — first whitespace or end of string.
	end := len(text)
	for i := 1; i < len(text); i++ {
		if text[i] == ' ' || text[i] == '\t' || text[i] == '\n' {
			end = i
			break
		}
	}
	cmd = strings.ToLower(text[:end])
	if end == len(text) {
		return cmd, "", true
	}
	args = strings.TrimSpace(text[end:])
	return cmd, args, true
}
```

- [ ] **Step 3: Add ExpandSlashCommand and SlashCommandSystemAddendum**

Append to the same file:

```go
// ExpandSlashCommand rewrites "/cmd args..." into the full expanded prompt.
// Returns (expanded, matchedName, true) on a successful expansion, or
// (text, "", false) otherwise. It is a pure function — callers may run it
// on any user message (current or historical) without side effects.
//
// Args are wrapped in SanitizePromptContent before substitution so a
// hostile user cannot inject prompt instructions via the argument.
// Unknown commands fall through unchanged. Commands that require args but
// were called with empty args also fall through unchanged — the LLM then
// sees the raw "/cmd" and can ask the user what they want.
func ExpandSlashCommand(text string) (expanded string, matched string, ok bool) {
	cmd, args, parsed := parseSlashCommand(text)
	if !parsed {
		return text, "", false
	}
	sc, found := slashCommands[cmd]
	if !found {
		return text, "", false
	}
	if sc.RequiresArgs && strings.TrimSpace(args) == "" {
		return text, "", false
	}

	data := struct{ Args string }{Args: utils.SanitizePromptContent(args)}
	var buf bytes.Buffer
	if err := sc.Template.Execute(&buf, data); err != nil {
		// A template execution failure on a compile-time-parsed template
		// is exceptionally unlikely; degrade gracefully by leaving the
		// original text in place rather than crashing the user's turn.
		return text, "", false
	}
	return buf.String(), sc.Name, true
}

// SlashCommandSystemAddendum returns the extra system block to append to
// llmMessages when this text is the user's *current* turn. Returns "" if
// the text is not a known command, required-args check fails, or the
// command has no addendum. Historical turns must not call this function —
// addendums apply to the active turn only.
func SlashCommandSystemAddendum(text string) string {
	cmd, args, parsed := parseSlashCommand(text)
	if !parsed {
		return ""
	}
	sc, found := slashCommands[cmd]
	if !found {
		return ""
	}
	if sc.RequiresArgs && strings.TrimSpace(args) == "" {
		return ""
	}
	return sc.SystemAddendum
}
```

- [ ] **Step 4: Register the v1 catalog in init()**

Append to the same file:

```go
// Addendums shared by multiple read-only / planning commands. Kept as
// constants so the exact wording stays in one place.
const (
	addendumReadOnly = "CURRENT TURN IS READ-ONLY. You MUST NOT call write_file, edit_file, or delete_file on this turn. Respond with an explanation only."
	addendumPlanOnly = "CURRENT TURN IS PLANNING ONLY. You MUST NOT call write_file, edit_file, or delete_file on this turn. Respond with a plan only."
	addendumAuditOnly = "CURRENT TURN IS READ-ONLY. You MUST NOT call write_file, edit_file, or delete_file on this turn. Respond with an audit report only."
)

func init() {
	registerSlashCommand("/plan", true,
		`PLAN MODE — The user wants to plan: {{.Args}}. Produce a concise plan, under 400 words, with these sections: (1) Key pages and routes, (2) Primary user flows, (3) Data model sketch, (4) Design direction (visual style, mood, typography), (5) Suggested first build step. Do NOT generate or modify any files. End with: "Reply 'continue' when you're ready to build."`,
		addendumPlanOnly,
	)

	registerSlashCommand("/fix", true,
		`FOCUSED BUGFIX — Fix only this specific issue: {{.Args}}. Change the minimum set of files required. Do not redesign, do not refactor unrelated code, do not add features. If the root cause is unclear, ask one clarifying question instead of guessing.`,
		"",
	)

	registerSlashCommand("/redesign", true,
		`VISUAL REDESIGN — Redesign this area: {{.Args}}. Improve layout, typography, spacing, color, and polish. Preserve all existing logic, routes, data flow, state, and component APIs. Do not rename props or change behavior. Visual changes only.`,
		"",
	)

	registerSlashCommand("/explain", true,
		`EXPLAIN MODE — Walk me through how this works: {{.Args}}. Reference specific files and line numbers. Be concrete. Do NOT modify any files.`,
		addendumReadOnly,
	)

	registerSlashCommand("/review", false,
		`REVIEW MODE — Audit the current app and report findings grouped by: Bugs, Accessibility, SEO, Performance, Design. Within each group, rank by impact. Be specific — cite file paths. Do NOT fix anything — the user will pick what to address.`,
		addendumAuditOnly,
	)

	registerSlashCommand("/seo", false,
		`SEO OPTIMIZATION PASS — Improve the app's SEO: meta tags (title, description, canonical), Open Graph and Twitter Card tags, sitemap.xml, robots.txt, semantic headings, and JSON-LD structured data where sensible. Do not touch visual design or business logic.`,
		"",
	)

	registerSlashCommand("/a11y", false,
		`ACCESSIBILITY PASS — Improve accessibility: ARIA labels where needed, alt text on images, semantic HTML, keyboard navigation, visible focus states, sufficient color contrast. Preserve the existing visual design — do not restyle components.`,
		"",
	)

	registerSlashCommand("/seed", false,
		`SEED DATA REFRESH — Regenerate the database seed data with fresh, realistic content appropriate to the app's domain. Use the seed_database tool. If no database is configured on this project, tell the user to set one up first.`,
		"",
	)
}
```

- [ ] **Step 5: Compile check**

Run from `wemob-backend/`:

```bash
go build ./...
```

Expected: exit code 0, no output. If you see an unused-import error on `utils`, confirm the `SanitizePromptContent` call is in place. If you see a template parse panic at runtime (later), it means one of the backtick strings above has an unclosed `{{`.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/slash_commands.go
git commit -m "feat(agent): slash command expansion registry"
```

---

### Task 1.2: Expand historical user messages in convertHistory

**Files:**
- Modify: `wemob-backend/internal/agent/run.go:528-534` (the `convertHistory` function)

- [ ] **Step 1: Read the current function**

Re-read `internal/agent/run.go` lines 528-534 so you have the exact existing shape in mind. It currently looks like:

```go
func convertHistory(history []models.AppChatMessage) []utils.Message {
	var out []utils.Message
	for _, m := range history {
		out = append(out, utils.Message{Role: m.Role, Content: m.Content})
	}
	return out
}
```

- [ ] **Step 2: Replace the function**

Replace those 7 lines with:

```go
func convertHistory(history []models.AppChatMessage) []utils.Message {
	var out []utils.Message
	for _, m := range history {
		content := m.Content
		// Historical user messages are stored literally (e.g. "/plan a blog").
		// Re-expand on every load so the LLM sees the same prompt body it saw
		// on the original turn. We deliberately do NOT replay the SystemAddendum
		// for historical turns — modal constraints like "do not write files"
		// only apply to the turn they were typed on.
		if m.Role == "user" {
			content, _, _ = ExpandSlashCommand(content)
		}
		out = append(out, utils.Message{Role: m.Role, Content: content})
	}
	return out
}
```

- [ ] **Step 3: Compile check**

```bash
go build ./...
```

Expected: exit code 0.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/run.go
git commit -m "feat(agent): expand slash commands in conversation history"
```

---

### Task 1.3: Expand current turn + inject current-turn system addendum

**Files:**
- Modify: `wemob-backend/internal/agent/run.go:143-155` (the block that builds `llmMessages`)

- [ ] **Step 1: Re-read the current block**

Re-read `internal/agent/run.go` around lines 143-155. It currently looks like:

```go
// --- Step 9: build system messages + registry + tracker ---
llmMessages := BuildSystemMessages(project, fileMap)
llmMessages = append(llmMessages, convertHistory(history)...)

// Append the current turn explicitly — we don't rely on history reload
// to round-trip images, and we want the turn to land even if history
// load failed.
userTurn := utils.Message{Role: "user", Content: message}
if len(images) > 0 {
	userTurn.Images = images
}
llmMessages = append(llmMessages, userTurn)
```

- [ ] **Step 2: Replace the block**

Replace the `userTurn := ...` through `llmMessages = append(llmMessages, userTurn)` lines with:

```go
// Append the current turn explicitly — we don't rely on history reload
// to round-trip images, and we want the turn to land even if history
// load failed. Slash command expansion happens here so the LLM sees the
// full prompt body while the persisted DB row (above) keeps the literal
// text the user typed.
expandedMessage, _, _ := ExpandSlashCommand(message)
userTurn := utils.Message{Role: "user", Content: expandedMessage}
if len(images) > 0 {
	userTurn.Images = images
}
// Modal commands (/plan, /explain, /review) inject an extra system block
// that forbids file mutations on the current turn only. History replay
// does NOT re-inject this — see convertHistory for the rationale.
if addendum := SlashCommandSystemAddendum(message); addendum != "" {
	llmMessages = append(llmMessages, utils.Message{
		Role:    "system",
		Content: addendum,
	})
}
llmMessages = append(llmMessages, userTurn)
```

Keep the `tracker.TraceInput = message` assignment further down untouched — PostHog traces should show the literal text the user typed, not the expansion, so bug reports are easier to trace back to chat UI bubbles.

- [ ] **Step 3: Compile check**

```bash
go build ./...
```

Expected: exit code 0.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/run.go
git commit -m "feat(agent): expand slash commands on current turn + inject addendum"
```

---

### Task 1.4: Smoke-test expansion locally via a one-off Go program

**Files:**
- Create: `wemob-backend/cmd/slashcheck/main.go` (temporary, removed at the end of the phase)

This gives you a fast feedback loop without spinning up the full server, Postgres, or Infisical. It exercises `ExpandSlashCommand` and `SlashCommandSystemAddendum` against the real registry.

- [ ] **Step 1: Create the harness**

```go
package main

import (
	"fmt"

	"github.com/knid/wemob/internal/agent"
)

func main() {
	cases := []string{
		"/plan a blog",
		"/plan", // required args missing
		"/fix header not clickable",
		"/redesign the hero section",
		"/explain how auth works",
		"/review",
		"/seo",
		"/a11y",
		"/seed",
		"/Plan A BLOG", // case insensitivity
		"/wat hello",   // unknown
		"hello /plan",  // not at start
		"",             // empty
		"not a command at all",
	}
	for _, in := range cases {
		expanded, name, ok := agent.ExpandSlashCommand(in)
		addendum := agent.SlashCommandSystemAddendum(in)
		fmt.Printf("---\nINPUT:    %q\n", in)
		fmt.Printf("OK:       %v\n", ok)
		fmt.Printf("MATCHED:  %q\n", name)
		fmt.Printf("ADDENDUM: %q\n", addendum)
		fmt.Printf("EXPANDED: %q\n", expanded)
	}
}
```

- [ ] **Step 2: Run it**

```bash
go run ./cmd/slashcheck
```

Expected output (key checks — full output is longer):

- `INPUT: "/plan a blog"` → `OK: true`, `MATCHED: "/plan"`, `ADDENDUM` non-empty, `EXPANDED` begins with `PLAN MODE — The user wants to plan: <user_data>a blog</user_data>...`.
- `INPUT: "/plan"` → `OK: false`, `EXPANDED` equals input `"/plan"` (required args missing, falls through).
- `INPUT: "/review"` → `OK: true`, `ADDENDUM` non-empty (audit-only text), `EXPANDED` starts with `REVIEW MODE —`.
- `INPUT: "/seo"` → `OK: true`, `ADDENDUM` empty, `EXPANDED` starts with `SEO OPTIMIZATION PASS —`.
- `INPUT: "/Plan A BLOG"` → `OK: true`, `MATCHED: "/plan"` (case-insensitive match). Args preserved with original case: expansion contains `A BLOG`.
- `INPUT: "/wat hello"` → `OK: false`, `EXPANDED` equals input.
- `INPUT: "hello /plan"` → `OK: false`, `EXPANDED` equals input.
- `INPUT: ""` → `OK: false`, `EXPANDED` equals `""`.

If any row is wrong, fix `slash_commands.go` and re-run. Do not proceed to the frontend phase until every row above matches.

- [ ] **Step 3: Remove the harness**

```bash
rm -rf cmd/slashcheck
```

- [ ] **Step 4: Compile check after removal**

```bash
go build ./...
```

Expected: exit code 0.

- [ ] **Step 5: Commit the removal**

```bash
git add -A cmd/slashcheck
git commit -m "chore(agent): remove slashcheck scratch harness"
```

(The `git add -A` captures the deletion. If the working tree is already clean, there's nothing to commit — that's fine, skip.)

---

## Phase 2 — Frontend: new catalog + literal display

### Task 2.1: Rebuild the slash commands module

**Files:**
- Modify: `wemob-dashboard-v2/src/components/studio/slash-commands.tsx:129-197` (the slash command section)

- [ ] **Step 1: Replace the `SlashCommand` interface, builder, matcher, and menu**

Open `src/components/studio/slash-commands.tsx` and replace the region from `// ---- Slash commands ----` (line 129) through the end of `SlashCommandMenu` (line 244) with the block below. The `ReasoningPicker` above (lines 1-128) and the `FileIconKind` / `FileKindIcon` helpers below (lines 246-end) stay unchanged.

```tsx
// ---- Slash commands ----

export type SlashCommand =
  | {
      command: string;
      label: string;
      kind: "macro";
      requiresArgs: boolean;
    }
  | {
      command: string;
      label: string;
      kind: "action";
      action: "deploy" | "clear";
    };

function buildSlashCommands(
  translate: (key: string) => string
): SlashCommand[] {
  return [
    {
      command: "/plan",
      label: translate("studioViews.slashCommands.commands.plan"),
      kind: "macro",
      requiresArgs: true,
    },
    {
      command: "/fix",
      label: translate("studioViews.slashCommands.commands.fix"),
      kind: "macro",
      requiresArgs: true,
    },
    {
      command: "/redesign",
      label: translate("studioViews.slashCommands.commands.redesign"),
      kind: "macro",
      requiresArgs: true,
    },
    {
      command: "/explain",
      label: translate("studioViews.slashCommands.commands.explain"),
      kind: "macro",
      requiresArgs: true,
    },
    {
      command: "/review",
      label: translate("studioViews.slashCommands.commands.review"),
      kind: "macro",
      requiresArgs: false,
    },
    {
      command: "/seo",
      label: translate("studioViews.slashCommands.commands.seo"),
      kind: "macro",
      requiresArgs: false,
    },
    {
      command: "/a11y",
      label: translate("studioViews.slashCommands.commands.a11y"),
      kind: "macro",
      requiresArgs: false,
    },
    {
      command: "/seed",
      label: translate("studioViews.slashCommands.commands.seed"),
      kind: "macro",
      requiresArgs: false,
    },
    {
      command: "/deploy",
      label: translate("studioViews.slashCommands.commands.deploy"),
      kind: "action",
      action: "deploy",
    },
    {
      command: "/clear",
      label: translate("studioViews.slashCommands.commands.clear"),
      kind: "action",
      action: "clear",
    },
  ];
}

export const SLASH_COMMANDS: SlashCommand[] = buildSlashCommands((key) =>
  i18n.t(key)
);

export function matchSlashCommands(text: string): SlashCommand[] {
  if (!text.startsWith("/") || text.includes("\n")) return [];
  // Match on the first token only so users can keep typing arguments
  // without the menu disappearing. "/plan" matches, "/plan a blog"
  // also matches (first token "/plan").
  const firstToken = text.split(/\s+/)[0].toLowerCase();
  const commands = buildSlashCommands((key) => i18n.t(key));
  return commands.filter((c) => c.command.startsWith(firstToken));
}

export function SlashCommandMenu({
  items,
  activeIndex,
  onPick,
}: {
  items: SlashCommand[];
  activeIndex: number;
  onPick: (cmd: SlashCommand) => void;
}) {
  const { t } = useTranslation();
  if (items.length === 0) return null;
  return (
    <div className="absolute bottom-full left-0 right-0 mb-2 bg-[var(--bg-surface)] border border-[var(--border)] rounded-[12px] shadow-lg overflow-hidden z-20">
      <div className="px-3 py-1.5 text-[10px] uppercase tracking-wider text-[var(--ink-faint)] border-b border-[var(--border)] font-semibold">
        {t("studioViews.slashCommands.menu.heading")}
      </div>
      <ul className="max-h-[220px] overflow-y-auto">
        {items.map((item, i) => {
          const hint =
            item.kind === "macro"
              ? item.requiresArgs
                ? t("studioViews.slashCommands.menu.needsInput")
                : t("studioViews.slashCommands.menu.runNow")
              : null;
          return (
            <li key={item.command}>
              <button
                type="button"
                onMouseDown={(e) => {
                  // Prevent textarea blur before click fires.
                  e.preventDefault();
                  onPick(item);
                }}
                className={cn(
                  "w-full text-left px-3 py-2 flex items-center gap-2 text-[13px] transition-colors",
                  i === activeIndex
                    ? "bg-[var(--accent-soft)] text-[var(--accent)]"
                    : "text-[var(--ink)] hover:bg-[var(--bg-hover)]"
                )}
              >
                <span className="mono text-[12px] font-semibold">
                  {item.command}
                </span>
                <span className="text-[var(--ink-faint)] text-[12px] truncate flex-1">
                  {item.label}
                </span>
                {hint && (
                  <span className="text-[10px] text-[var(--ink-faint)] uppercase tracking-wider ml-auto pl-2 flex-shrink-0">
                    {hint}
                  </span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
```

Notes for the engineer:

1. The `SlashCommand` type is now a discriminated union. TypeScript will force every consumer to handle both variants — that's intentional and helps Task 2.2 below.
2. `matchSlashCommands` used to match on the full text with `.startsWith(prefix)`. The new version matches on the first whitespace-delimited token so that typing `"/plan a blog"` still leaves the menu matching `/plan`. This also means the matcher stays stable while the user types arguments (before, typing a space would clear the match).
3. The `kind: "insert"` variant and its `insert` field are completely gone. If you see any dangling reference, fix it.

- [ ] **Step 2: TypeScript check**

Run from `wemob-dashboard-v2/`:

```bash
npx tsc --noEmit
```

Expected: expect type errors pointing at `chat-panel.tsx` — specifically, `pickSlashCommand` still references `cmd.kind === "insert"` and `cmd.insert`. These are fixed in Task 2.2. Do not commit yet if there are any errors outside of `chat-panel.tsx`.

---

### Task 2.2: Update chat-panel to handle the new macro kind

**Files:**
- Modify: `wemob-dashboard-v2/src/components/studio/chat-panel.tsx:915-950` (the `pickSlashCommand` callback)

- [ ] **Step 1: Re-read the current `pickSlashCommand`**

Re-read lines 915-950 of `src/components/studio/chat-panel.tsx`. It currently has an `if (cmd.kind === "insert" && cmd.insert)` branch and an `if (cmd.kind === "action")` branch.

- [ ] **Step 2: Replace the callback**

Replace the entire `pickSlashCommand` `useCallback(...)` block (from `const pickSlashCommand = useCallback(` down to and including the closing `);` with its dependency array) with:

```tsx
  const pickSlashCommand = useCallback(
    (cmd: SlashCommand) => {
      if (cmd.kind === "macro") {
        // Macro commands are sent literally to the backend. We just help
        // the user along: if the command needs arguments, seed the
        // textarea with "/cmd " so they can type their input; otherwise
        // seed it with "/cmd" so a quick Enter sends immediately.
        const next = cmd.requiresArgs ? `${cmd.command} ` : cmd.command;
        setValue(next);
        requestAnimationFrame(() => {
          const el = textareaRef.current;
          if (el) {
            el.focus();
            el.setSelectionRange(el.value.length, el.value.length);
          }
        });
        return;
      }
      if (cmd.kind === "action") {
        setValue("");
        if (cmd.action === "deploy") {
          if (onRequestDeploy) onRequestDeploy();
          else toast(t("chat.panel.addDeployHandler"));
          return;
        }
        if (cmd.action === "clear") {
          if (onClearChat) {
            onClearChat().catch((err) => {
              toast.error(t("chat.panel.clearFailedTitle"), {
                description: err instanceof Error ? err.message : String(err),
              });
            });
          } else {
            toast(t("chat.panel.addClearHandler"));
          }
          return;
        }
      }
    },
    [onRequestDeploy, onClearChat, t],
  );
```

The only structural change vs. the original is that the `"insert"` branch is replaced with the `"macro"` branch. Everything else is identical — same ref, same requestAnimationFrame pattern, same action handling, same dependency array.

- [ ] **Step 3: TypeScript check**

```bash
npx tsc --noEmit
```

Expected: exit code 0. If errors remain, re-read the file to make sure no stray `cmd.insert` references survived.

- [ ] **Step 4: Commit the slash commands refactor and chat panel update together**

```bash
git add src/components/studio/slash-commands.tsx src/components/studio/chat-panel.tsx
git commit -m "feat(studio): slash commands become server-side macros"
```

---

### Task 2.3: Update i18n locales (en + tr)

**Files:**
- Modify: `wemob-dashboard-v2/src/lib/i18n/locales/en/studio-views.json:251-275`
- Modify: `wemob-dashboard-v2/src/lib/i18n/locales/tr/studio-views.json:251-275`

- [ ] **Step 1: Replace the `slashCommands.commands` object in en/studio-views.json**

Open `src/lib/i18n/locales/en/studio-views.json`. The current `slashCommands` object (lines 251-275) has keys like `fix`, `fixInsert`, `explain`, `explainInsert`, etc. Replace the entire `slashCommands` object with:

```json
    "slashCommands": {
      "reasoning": {
        "tooltip": "Reasoning effort",
        "fast": "Fast",
        "balanced": "Balanced",
        "deep": "Deep"
      },
      "menu": {
        "heading": "Commands",
        "needsInput": "Needs input",
        "runNow": "Run now"
      },
      "commands": {
        "plan": "Plan an app without building it",
        "fix": "Fix a specific bug",
        "redesign": "Redesign an area visually",
        "explain": "Explain how part of the app works",
        "review": "Audit the app for issues",
        "seo": "Run an SEO optimization pass",
        "a11y": "Run an accessibility pass",
        "seed": "Regenerate database seed data",
        "deploy": "Deploy now",
        "clear": "Clear conversation"
      }
    }
```

The removed keys are: `fixInsert`, `explainInsert`, `refactor`, `refactorInsert`, `ui`, `uiInsert`, `test`, `testInsert`. The added keys are: `menu.needsInput`, `menu.runNow`, `commands.plan`, `commands.redesign`, `commands.review`, `commands.seo`, `commands.a11y`, `commands.seed`.

- [ ] **Step 2: Replace the same object in tr/studio-views.json**

Open `src/lib/i18n/locales/tr/studio-views.json` and replace its `slashCommands` object (lines 251-275) with:

```json
    "slashCommands": {
      "reasoning": {
        "tooltip": "Akıl yürütme çabası",
        "fast": "Hızlı",
        "balanced": "Dengeli",
        "deep": "Derin"
      },
      "menu": {
        "heading": "Komutlar",
        "needsInput": "Giriş gerekli",
        "runNow": "Hemen çalıştır"
      },
      "commands": {
        "plan": "Uygulamayı oluşturmadan önce planla",
        "fix": "Belirli bir hatayı düzelt",
        "redesign": "Bir alanı görsel olarak yeniden tasarla",
        "explain": "Uygulamanın bir kısmını açıkla",
        "review": "Uygulamayı sorunlar için denetle",
        "seo": "SEO iyileştirme geçişi yap",
        "a11y": "Erişilebilirlik geçişi yap",
        "seed": "Veritabanı tohum verisini yenile",
        "deploy": "Şimdi dağıt",
        "clear": "Konuşmayı temizle"
      }
    }
```

- [ ] **Step 3: Grep for dead references**

Make sure no code still references the removed keys.

```bash
grep -rn "fixInsert\|explainInsert\|refactorInsert\|uiInsert\|testInsert\|slashCommands.commands.refactor\|slashCommands.commands.ui\b\|slashCommands.commands.test\b" src
```

Expected: no output. If anything matches, delete those references — they are the tail of the old insert-style commands.

- [ ] **Step 4: TypeScript check**

```bash
npx tsc --noEmit
```

Expected: exit code 0.

- [ ] **Step 5: Commit**

```bash
git add src/lib/i18n/locales/en/studio-views.json src/lib/i18n/locales/tr/studio-views.json
git commit -m "i18n(studio): new slash command labels (en + tr)"
```

---

## Phase 3 — End-to-end verification in Studio

No code changes in this phase — only manual testing. The goal is to validate each command behaves as the spec describes.

### Task 3.1: Start the dev environment

**Files:** (none)

- [ ] **Step 1: Start the backend**

From `wemob-backend/`:

```bash
docker-compose -f docker-compose-dev.yaml up
```

Wait for the Air watcher to print a "running" line. Confirm no panic in the logs — a template parse error in `slash_commands.go` would surface here.

- [ ] **Step 2: Start the dashboard**

From `wemob-dashboard-v2/`, in a second terminal:

```bash
npm run dev
```

Open the printed URL and log in. Navigate to an existing Studio app (not a draft — this ensures history-load exercises `convertHistory`).

---

### Task 3.2: Literal persistence check

- [ ] **Step 1: Open the chat panel and type `/plan a blog`**

Type `"/plan"` to bring up the menu, press Enter to seed `"/plan "` into the textarea, then type `"a blog"` so the full textarea reads `/plan a blog`. Press Enter to send.

Expected chat bubble contents: exactly `/plan a blog`. Not the expanded prompt. If the bubble contains the long `PLAN MODE — ...` text, the frontend is still expanding client-side — grep `slash-commands.tsx` and `chat-panel.tsx` for any `insert` leftovers and the bug is there.

- [ ] **Step 2: Reload the page and confirm the same bubble**

Hit browser refresh. Wait for the chat to re-hydrate from the backend. The same bubble should still read `/plan a blog`. If it now reads the expanded prompt, `AppChatMessage.Content` was persisted expanded — grep `run.go` for `InsertChatMessage` and confirm the `Content` field passed in is the raw `message` variable, not `expandedMessage`.

---

### Task 3.3: Plan-mode addendum check

- [ ] **Step 1: Watch the agent response to `/plan a blog`**

The agent should respond with a structured 5-section plan under ~400 words. It must NOT write, edit, or delete any files. Check the file-operations panel in the Studio — it should remain empty for this turn.

- [ ] **Step 2: Send "continue"**

Send `continue` as a plain user message. The agent should now begin building — writing and editing files. This confirms the plan-mode addendum was current-turn-only. If the agent refuses to write files on the second turn as well, you regressed the "current-turn-only" rule — re-read Task 1.3.

---

### Task 3.4: Fix, redesign, explain, review checks

- [ ] **Step 1: `/fix <bug>`**

Introduce an obvious bug (e.g. open a file and change a valid import path to a broken one), then send `"/fix the broken import in app/page.tsx"`. Expect a minimal diff — only the import line touched.

- [ ] **Step 2: `/redesign hero section`**

Send `"/redesign the hero section"`. Expect visual-only changes — classNames, Tailwind utilities, typography. No new routes, no new state, no prop renames.

- [ ] **Step 3: `/explain how auth works`**

Send `"/explain how the login flow works"`. Expect a prose walkthrough referencing file paths. No file mutations in the timeline.

- [ ] **Step 4: `/review`**

Send `"/review"` with no args. Expect a grouped audit (Bugs / Accessibility / SEO / Performance / Design) with no file writes.

- [ ] **Step 5: `/seo` and `/a11y`**

Send `"/seo"` and `"/a11y"` in sequence. Expect targeted changes — meta tags / sitemap / robots.txt for the first, ARIA / alt / keyboard nav for the second. Don't expect either to touch visual design.

---

### Task 3.5: Seed, unknown, and history checks

- [ ] **Step 1: `/seed` on a db-less project**

On a project without a Neon database integration, send `"/seed"`. The agent should respond with a "no database configured" message (via the `seed_database` tool's error path) or tell the user to set one up. It must not crash.

- [ ] **Step 2: Unknown command**

Send `"/wat hello"`. The agent should respond normally — the literal `"/wat hello"` is just treated as a plain user message. It may ask what the user meant.

- [ ] **Step 3: History round-trip**

In a multi-turn conversation where an earlier turn was `/plan a topic`, send a new plain-text edit request. Confirm the agent builds files now — the historical `/plan` must not still be blocking the agent. Check the Studio's file operations panel for writes on this new turn.

---

### Task 3.6: Cleanup

- [ ] **Step 1: Shut down both dev servers**

Ctrl+C the `docker-compose` and `npm run dev` processes.

- [ ] **Step 2: Confirm no uncommitted changes**

From both `wemob-backend/` and `wemob-dashboard-v2/`:

```bash
git status
```

Expected: both working trees clean, with the commits from Phases 1 and 2 on the branch.

---

## Summary of commits

Phase 1 (`wemob-backend`):
1. `feat(agent): slash command expansion registry`
2. `feat(agent): expand slash commands in conversation history`
3. `feat(agent): expand slash commands on current turn + inject addendum`
4. (optional) `chore(agent): remove slashcheck scratch harness`

Phase 2 (`wemob-dashboard-v2`):
1. `feat(studio): slash commands become server-side macros`
2. `i18n(studio): new slash command labels (en + tr)`

Phase 3: no commits (verification only).
