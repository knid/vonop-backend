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
	SkillIDs       []string
}

// slashCommands is populated in init(). Keys are lowercase command names
// including the leading slash (e.g. "/plan").
var slashCommands = map[string]*slashCommand{}

// registerSlashCommand parses the template body and stores the command.
// Panics on a malformed template — templates are compile-time constants,
// so a parse failure is a programmer error and should crash at startup.
func registerSlashCommand(name string, requiresArgs bool, body, addendum string) {
	registerSlashCommandWithSkills(name, requiresArgs, body, addendum, nil)
}

func registerSlashCommandWithSkills(name string, requiresArgs bool, body, addendum string, skillIDs []string) {
	tmpl, err := template.New(name).Parse(body)
	if err != nil {
		panic("slash_commands: bad template for " + name + ": " + err.Error())
	}
	slashCommands[strings.ToLower(name)] = &slashCommand{
		Name:           name,
		RequiresArgs:   requiresArgs,
		Template:       tmpl,
		SystemAddendum: addendum,
		SkillIDs:       skillIDs,
	}
}

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

// SlashCommandIsReadOnly returns true when the current slash command is a
// modal planning/audit/explanation turn that must not generate or mutate files.
func SlashCommandIsReadOnly(text string) bool {
	cmd, args, parsed := parseSlashCommand(text)
	if !parsed {
		return false
	}
	sc, found := slashCommands[cmd]
	if !found {
		return false
	}
	if sc.RequiresArgs && strings.TrimSpace(args) == "" {
		return false
	}
	return sc.SystemAddendum == addendumReadOnly ||
		sc.SystemAddendum == addendumPlanOnly ||
		sc.SystemAddendum == addendumAuditOnly
}

// SlashCommandSkillIDs returns skill ids requested by a recognized leading
// slash command. Unknown commands and commands missing required args fall
// through with no skills, matching ExpandSlashCommand behavior.
func SlashCommandSkillIDs(text string) []string {
	cmd, args, parsed := parseSlashCommand(text)
	if !parsed {
		return nil
	}
	sc, found := slashCommands[cmd]
	if !found {
		return nil
	}
	if sc.RequiresArgs && strings.TrimSpace(args) == "" {
		return nil
	}
	out := make([]string, len(sc.SkillIDs))
	copy(out, sc.SkillIDs)
	return out
}

// Addendums shared by multiple read-only / planning commands. Kept as
// constants so the exact wording stays in one place.
const (
	addendumReadOnly  = "CURRENT TURN IS READ-ONLY. You MUST NOT call create_design, write_files, edit_file, multi_edit, or delete_file on this turn. Respond with an explanation only."
	addendumPlanOnly  = "CURRENT TURN IS PLANNING ONLY. You MUST NOT call create_design, write_files, edit_file, multi_edit, or delete_file on this turn. Respond with a plan only."
	addendumAuditOnly = "CURRENT TURN IS READ-ONLY. You MUST NOT call create_design, write_files, edit_file, multi_edit, or delete_file on this turn. Respond with an audit report only."
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

	registerSlashCommandWithSkills("/seo", false,
		`SEO OPTIMIZATION PASS — Improve the app's SEO: meta tags (title, description, canonical), Open Graph and Twitter Card tags, sitemap.xml, robots.txt, semantic headings, and JSON-LD structured data where sensible. Do not touch visual design or business logic.`,
		"",
		[]string{"web-seo"},
	)

	registerSlashCommandWithSkills("/a11y", false,
		`ACCESSIBILITY PASS — Improve accessibility: ARIA labels where needed, alt text on images, semantic HTML, keyboard navigation, visible focus states, sufficient color contrast. Preserve the existing visual design — do not restyle components.`,
		"",
		[]string{"accessibility"},
	)

	registerSlashCommand("/seed", false,
		`SEED DATA REFRESH — Regenerate the database seed data with fresh, realistic content appropriate to the app's domain. Use the seed_database tool. If no database is configured on this project, tell the user to set one up first.`,
		"",
	)
}
