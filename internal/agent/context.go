package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// planningDiscipline is injected as a final system message to reduce tool-round
// churn. The compact base prompt covers basic tool protocol; this block keeps
// batching-specific guidance close to the registry's batch tool surface.
const planningDiscipline = `## Planning Discipline
Before calling tools, batch independent work into the fewest calls possible. Do
not spend one model round per file or per small edit when the next actions are
already known:
- read_files (plural) for every file you need to inspect — one call, all paths.
  If grep already identified all relevant files, read them together before
  deciding edits.
- create_design for DESIGN.md before broad first-generation UI work.
- write_files for one or more new files or small full-file writes in ONE call.
  Do not use it to rewrite large existing files; patch them instead.
- patch_files for exact-string edits across 2+ existing files in ONE call.
- multi_edit for 2+ changes to the SAME file. When the needed changes are clear,
  batch them instead of editing one occurrence per round. Prefer this for large
  existing files.
- execute_sql with multiple semicolon-separated SQL statements in ONE call for
  independent SELECTs, INSERTs, UPDATEs, or ALTERs. Only split into sequential
  execute_sql calls when a later statement depends on the result of an earlier
  one (e.g., an INSERT whose RETURNING id is needed by the next INSERT).

Once you have enough context, write or patch in the same round where possible.
Do not narrate your plan at length — act with tools immediately.`

const projectTypeBoundary = `## Project Type Boundary
This project was created with project_type=%q (%s).

If the user's request clearly asks for a different project type, do not build a
workaround in this project. Warn the user briefly that this project is %s and
that they should create a new %s project instead, then stop without writing
files or calling tools.

Treat these as mismatches:
- project_type=app, but the user asks for an online store, product catalog,
  cart, checkout, orders, inventory, shipping, or selling products/services:
  they need an ecommerce project.
- project_type=app or ecommerce, but the user asks for a native iOS/Android,
  React Native, Expo, App Store, Play Store, push-notification, biometric,
  deep-link, offline-native, or in-app-purchase app: they need a
  mobile_app_native project.
- project_type=mobile_app_native, but the user asks for a web-only marketing
  site, web dashboard, portfolio, or ecommerce storefront without native
  mobile requirements: they need an app or ecommerce project.

If the request fits this project type, continue normally.`

// mobileConfigGetter abstracts GetMobileAppConfig so context.go can be tested
// without a real *database.Database. *database.Database satisfies this interface.
type mobileConfigGetter interface {
	GetMobileAppConfig(projectID string) (*models.MobileAppConfig, error)
}

// BuildSystemMessages returns the cached system messages for a run:
//
//  1. Profile base prompt  (CompactBasePromptWeb or CompactBasePromptMobileNative)
//  2. Mode-specific block  (FirstGen or Chat addendum, depending on run mode)
//  3. Variant addendum     (stable across this project's runs, if applicable)
//  4. Project assets       (per-turn — asset uploads/deletes invalidate this block only)
//
// The file tree is intentionally NOT a system block. It now rides along with
// the user turn (see BuildFileTreeUserPrefix), because every full-file write used
// to mutate the system prefix and force a fresh prompt-cache write across all
// downstream blocks. Moving it into the user message keeps system content
// stable across rounds; the agent uses list_files / grep to refresh state
// on demand, and the in-history tool results already report each write.
//
// anthropic.go marks up to 4 system blocks with cache_control, so each block
// becomes its own cache breakpoint for prompt caching.
//
// deps is passed in so the AI addendum can render only the chat models the
// current user's subscription tier can actually reach; passing nil degrades
// to a conservative generic block.
func BuildSystemMessages(project *models.AppProject, deps *RunDependencies, attachmentsPool []models.AppAsset, isFirstGen bool) []utils.Message {
	return BuildSystemMessagesForProfile(ResolveAgentProfile(project), project, deps, attachmentsPool, isFirstGen)
}

func BuildSystemMessagesForProfile(profile *AgentProfile, project *models.AppProject, deps *RunDependencies, attachmentsPool []models.AppAsset, isFirstGen bool) []utils.Message {
	if profile == nil {
		profile = ResolveAgentProfile(project)
	}
	base := profile.BasePrompt
	modeAddendum := profile.ModePrompt(isFirstGen)
	addendum := buildAddendum(project, deps)
	boundary := buildProjectTypeBoundary(project)

	msgs := []utils.Message{{Role: "system", Content: base}}
	msgs = append(msgs, utils.Message{Role: "system", Content: modeAddendum})
	if boundary != "" {
		msgs = append(msgs, utils.Message{Role: "system", Content: boundary})
	}
	if addendum != "" {
		msgs = append(msgs, utils.Message{Role: "system", Content: addendum})
	}
	if manifest := AttachmentsAddendum(attachmentsPool); manifest != "" {
		msgs = append(msgs, utils.Message{Role: "system", Content: manifest})
	}
	msgs = append(msgs, utils.Message{Role: "system", Content: planningDiscipline})
	return msgs
}

func buildProjectTypeBoundary(project *models.AppProject) string {
	if project == nil {
		return ""
	}
	projectType := project.ProjectType
	if projectType == "" {
		projectType = models.ProjectTypeApp
	}
	switch projectType {
	case models.ProjectTypeApp:
		return fmt.Sprintf(projectTypeBoundary, projectType, "web app", "a web app", "ecommerce or mobile_app_native")
	case models.ProjectTypeEcommerce:
		return fmt.Sprintf(projectTypeBoundary, projectType, "ecommerce web store", "an ecommerce app", "app or mobile_app_native")
	case models.ProjectTypeMobileAppNative:
		return fmt.Sprintf(projectTypeBoundary, projectType, "native mobile app", "a native mobile app", "app or ecommerce")
	default:
		return fmt.Sprintf(projectTypeBoundary, projectType, "unknown app type", "this app type", "supported")
	}
}

// BuildFileTreeUserPrefix returns the file-tree context block that should be
// prepended to the current turn's user message. Returning an empty string is
// a valid signal that no prefix is needed (e.g. brand-new project with no
// files yet — the agent already knows from the system prompt that it's
// starting from a templated scaffold).
//
// Why this lives outside system messages: see BuildSystemMessages doc.
func BuildFileTreeUserPrefix(fileMap *SafeFileMap) string {
	return buildFileTreeBlock(fileMap)
}

const (
	designBriefPath     = "DESIGN.md"
	maxDesignBriefChars = 8000
)

// BuildDesignBriefUserPrefix returns the persisted design contract for the
// current app. It rides in the user turn rather than a system block so edits to
// DESIGN.md do not invalidate the cached base prompt.
func BuildDesignBriefUserPrefix(fileMap *SafeFileMap) string {
	if fileMap == nil {
		return ""
	}
	content, ok := fileMap.Get(designBriefPath)
	if !ok || strings.TrimSpace(content) == "" {
		return ""
	}
	content = strings.TrimSpace(content)
	truncated := false
	if len(content) > maxDesignBriefChars {
		content = content[:maxDesignBriefChars]
		truncated = true
	}
	var sb strings.Builder
	sb.WriteString("DESIGN BRIEF\n")
	sb.WriteString("============\n\n")
	sb.WriteString("Persisted from DESIGN.md. Treat this as the visual source of truth for this project; preserve it unless the user asks for a redesign.\n\n")
	sb.WriteString(content)
	if truncated {
		sb.WriteString("\n\n[DESIGN.md truncated for context]\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// buildAddendum composes project-specific prompt blocks.
//
// Ecommerce projects receive EcommerceAddendum only — it already covers the
// database story for them. DatabaseAddendum is for generic web projects whose
// user asked for a DB; layering it on top of EcommerceAddendum contradicted
// the canonical ecommerce schema paths and doubled the token count.
//
// mobile_app_native projects receive a MobileAppConfig addendum listing the
// currently-enabled native features so the agent doesn't need to call
// read_mobile_config every turn.
func buildAddendum(project *models.AppProject, deps *RunDependencies) string {
	if project.ProjectType == models.ProjectTypeMobileAppNative {
		var parts []string
		var fetcher mobileConfigGetter
		if deps != nil {
			if deps.mobileConfigFetcher != nil {
				fetcher = deps.mobileConfigFetcher
			} else if deps.DB != nil {
				fetcher = deps.DB
			}
		}
		if fetcher != nil {
			cfg, err := fetcher.GetMobileAppConfig(project.ID.String())
			if err == nil && cfg != nil {
				if a := mobileNativeAddendum(cfg); a != "" {
					parts = append(parts, a)
				}
			}
		}
		if projectHasDatabase(project) {
			parts = append(parts, DatabaseAddendumNative)
		}
		if projectHasLocalDatabase(project) {
			parts = append(parts, LocalDatabaseAddendum)
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, "\n\n")
	}

	var parts []string
	isEcommerce := project.ProjectType == "ecommerce"
	isBackendOnly := project.FrontendRuntime == models.FrontendRuntimeNone
	if isBackendOnly {
		parts = append(parts, BackendOnlyAddendum)
	} else if project.UsesSplitWebRuntime() {
		parts = append(parts, SplitWebAddendum)
	}
	if isEcommerce {
		parts = append(parts, EcommerceAddendum)
	}
	if projectHasAuth(project) {
		parts = append(parts, AuthAddendum)
	}
	if (project.UsesSplitWebRuntime() || isBackendOnly) && projectHasDatabase(project) {
		parts = append(parts, DatabaseAddendumSplitWeb)
	} else if !isEcommerce && projectHasDatabase(project) {
		parts = append(parts, DatabaseAddendum)
	}
	if projectHasAI(project) {
		parts = append(parts, buildAIAddendum(deps, project))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// mobileNativeAddendum formats the current MobileAppConfig as a system-message
// addendum so the agent sees the project's enabled native features on every
// turn without calling read_mobile_config first.
func mobileNativeAddendum(cfg *models.MobileAppConfig) string {
	if cfg == nil {
		return ""
	}
	methods := cfg.BridgeMethods
	if methods == nil {
		methods = []string{}
	}
	permissions := cfg.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	var sb strings.Builder
	sb.WriteString("## Current MobileAppConfig (read by the agent — call read_mobile_config to refetch)\n\n")
	sb.WriteString(fmt.Sprintf("Enabled native features: [%s]\n", strings.Join(methods, ", ")))
	sb.WriteString(fmt.Sprintf("URL scheme: %q\n", cfg.URLScheme))
	sb.WriteString(fmt.Sprintf("Universal link domain: %q\n", cfg.UniversalLinkDomain))
	sb.WriteString(fmt.Sprintf("Biometric enabled: %v\n", cfg.BiometricEnabled))
	sb.WriteString(fmt.Sprintf("Push enabled: %v\n", cfg.PushEnabled))
	sb.WriteString(fmt.Sprintf("Dark mode enabled: %v\n", cfg.DarkModeEnabled))
	sb.WriteString(fmt.Sprintf("Offline enabled: %v\n", cfg.OfflineEnabled))
	sb.WriteString(fmt.Sprintf("Permissions: [%s]\n", strings.Join(permissions, ", ")))
	sb.WriteString("\nIf you need a feature not in the list, call configure_mobile_bridge first.")
	return sb.String()
}

// projectHasAI returns true if the project has the Wemob AI Gateway wired up
// (signaled by Settings["has_ai"]). The flag is flipped by the
// SetupAIGatewayTool via the EnableAI callback.
func projectHasAI(project *models.AppProject) bool {
	if project == nil || project.Settings == nil {
		return false
	}
	if v, ok := project.Settings["has_ai"].(bool); ok {
		return v
	}
	return false
}

// buildFileTreeBlock produces a deterministic "FILE TREE" listing sorted
// by path so cache keys remain stable across runs with identical file sets.
// Large projects are truncated to keep the context window lean.
func buildFileTreeBlock(fileMap *SafeFileMap) string {
	if fileMap == nil {
		return "FILE TREE\n=========\n\n(empty — this is a new project, start by creating files)\n"
	}
	files := fileMap.Snapshot()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var sb strings.Builder
	sb.WriteString("FILE TREE\n")
	sb.WriteString("=========\n\n")
	if len(paths) == 0 {
		sb.WriteString("(empty — this is a new project, start by creating files)\n")
		return sb.String()
	}

	const maxTreeLines = 100
	if len(paths) > maxTreeLines {
		// Prioritize config/entry files, then truncate
		prioritySuffixes := []string{"package.json", "app.json", "tsconfig.json", "tailwind.config.js", ".env", "/index.tsx", "/_layout.tsx", "/page.tsx"}
		priority := make([]string, 0, len(paths))
		rest := make([]string, 0, len(paths))
		for _, p := range paths {
			isPriority := false
			for _, suffix := range prioritySuffixes {
				if strings.HasSuffix(p, suffix) {
					isPriority = true
					break
				}
			}
			if isPriority {
				priority = append(priority, p)
			} else {
				rest = append(rest, p)
			}
		}
		for _, p := range priority {
			fmt.Fprintf(&sb, "%s (%d bytes)\n", p, len(files[p]))
		}
		remaining := maxTreeLines - len(priority)
		if remaining > 0 && len(rest) > remaining {
			for _, p := range rest[:remaining] {
				fmt.Fprintf(&sb, "%s (%d bytes)\n", p, len(files[p]))
			}
		}
		fmt.Fprintf(&sb, "... (%d more files)\n", len(paths)-len(priority)-remaining)
	} else {
		for _, p := range paths {
			fmt.Fprintf(&sb, "%s (%d bytes)\n", p, len(files[p]))
		}
	}
	return sb.String()
}

// projectHasAuth reads the auth flag from project.Settings.
func projectHasAuth(project *models.AppProject) bool {
	if project == nil || project.Settings == nil {
		return false
	}
	if v, ok := project.Settings["needs_auth"].(bool); ok {
		return v
	}
	return false
}
