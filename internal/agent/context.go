package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/observability"
	"github.com/knid/wemob/internal/utils"
)

func contextWithoutLangfuse() context.Context {
	return context.Background()
}

func depsWithoutLangfuse(deps *RunDependencies) *RunDependencies {
	if deps == nil {
		return nil
	}
	copyDeps := *deps
	copyDeps.ResolveLangfusePrompt = nil
	return &copyDeps
}

// planningDiscipline is injected as a final system message to reduce tool-round
// churn. The compact base prompt covers basic tool protocol; this block keeps
// batching-specific guidance close to the registry's batch tool surface.
const planningDiscipline = `## Planning Discipline
Read relevant code before editing. Prefer targeted patches over rewrites.
Plan briefly, batch obvious reads and edits, recover once from failed edits,
verify once, then stop calling tools. Keep narration brief and act with tools.`

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
	msgs, _ := BuildSystemMessagesForProfileWithLangfuse(contextWithoutLangfuse(), profile, project, depsWithoutLangfuse(deps), attachmentsPool, isFirstGen)
	return msgs
}

func BuildSystemMessagesForProfileWithLangfuse(ctx context.Context, profile *AgentProfile, project *models.AppProject, deps *RunDependencies, attachmentsPool []models.AppAsset, isFirstGen bool) ([]utils.Message, []observability.PromptLink) {
	if profile == nil {
		profile = ResolveAgentProfile(project)
	}
	base, baseLink := resolveLangfusePrompt(ctx, deps, langfuseBasePromptName(profile), profile.BasePrompt, nil)
	modeAddendum, modeLink := buildModePromptWithLangfuse(ctx, deps, profile, isFirstGen)
	addendum, addendumLinks := buildAddendumWithLangfuse(ctx, project, deps)
	boundary, boundaryLink := buildProjectTypeBoundaryWithLangfuse(ctx, deps, project)

	msgs := []utils.Message{{Role: "system", Content: base}}
	promptParts := []observability.PromptLink{baseLink}
	msgs = append(msgs, utils.Message{Role: "system", Content: modeAddendum})
	promptParts = append(promptParts, modeLink)
	if boundary != "" {
		msgs = append(msgs, utils.Message{Role: "system", Content: boundary})
		promptParts = append(promptParts, boundaryLink)
	}
	if addendum != "" {
		msgs = append(msgs, utils.Message{Role: "system", Content: addendum})
		promptParts = append(promptParts, addendumLinks...)
	}
	if manifest, link := AttachmentsAddendumWithLangfuse(ctx, deps, attachmentsPool); manifest != "" {
		msgs = append(msgs, utils.Message{Role: "system", Content: manifest})
		promptParts = append(promptParts, link)
	}
	if !leanWebEditContext(profile, isFirstGen) {
		planning, planningLink := resolveLangfusePrompt(ctx, deps, observability.PromptAgentPlanningDiscipline, planningDiscipline, nil)
		msgs = append(msgs, utils.Message{Role: "system", Content: planning})
		promptParts = append(promptParts, planningLink)
	}
	return msgs, promptParts
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

func buildProjectTypeBoundaryWithLangfuse(ctx context.Context, deps *RunDependencies, project *models.AppProject) (string, observability.PromptLink) {
	fallback := buildProjectTypeBoundary(project)
	if fallback == "" {
		return "", observability.PromptLink{}
	}
	projectType, label, article, targets := projectTypeBoundaryVars(project)
	return resolveLangfusePrompt(ctx, deps, observability.PromptAgentProjectTypeBoundary, fallback, map[string]string{
		"project_type":         projectType,
		"project_type_label":   label,
		"project_article":      article,
		"target_project_types": targets,
	})
}

func projectTypeBoundaryVars(project *models.AppProject) (string, string, string, string) {
	projectType := models.ProjectTypeApp
	if project != nil && project.ProjectType != "" {
		projectType = project.ProjectType
	}
	switch projectType {
	case models.ProjectTypeApp:
		return projectType, "web app", "a web app", "ecommerce or mobile_app_native"
	case models.ProjectTypeEcommerce:
		return projectType, "ecommerce web store", "an ecommerce app", "app or mobile_app_native"
	case models.ProjectTypeMobileAppNative:
		return projectType, "native mobile app", "a native mobile app", "app or ecommerce"
	default:
		return projectType, "unknown app type", "this app type", "supported"
	}
}

func langfuseBasePromptName(profile *AgentProfile) string {
	if profile != nil && profile.ID == AgentIDMobileBuilder {
		return observability.PromptAgentMobileBase
	}
	return observability.PromptAgentWebBase
}

func buildModePromptWithLangfuse(ctx context.Context, deps *RunDependencies, profile *AgentProfile, isFirstGen bool) (string, observability.PromptLink) {
	if isFirstGen {
		return resolveLangfusePrompt(ctx, deps, observability.PromptAgentModeFirstGen, profile.ModePrompt(true), firstGenPromptVars(profile))
	}
	return resolveLangfusePrompt(ctx, deps, observability.PromptAgentModeEdit, profile.ModePrompt(false), editPromptVars(profile))
}

func firstGenPromptVars(profile *AgentProfile) map[string]string {
	if profile != nil && profile.ID == AgentIDMobileBuilder {
		return map[string]string{
			"first_gen_intro":    "",
			"home_first_title":   "home screen first",
			"home_first_rule":    "On a new project, your VERY FIRST turn MUST customize mobile/app/(app)/index.tsx.\nIt must render a real home experience: header, hero/intro, primary CTA, and at\nleast one content block, even with stub data. Other screens come AFTER.\n\nWhy: runs can be cancelled mid-loop. If home lands first, the user sees\nsomething working. Treat this as a hard ordering constraint.\n\nThis rule does NOT apply when the FILE TREE already shows a customized home.",
			"home_label":         "home",
			"visible_surface":    "screen",
			"run_order":          "  1. mobile/app/(app)/index.tsx                  (Home First)\n  2. Every other screen + component with stubs   (UI First)\n  3. Image generation                            (Images Last)\n  4. Database / native modules / integrations    (only if requested)",
			"backend_exceptions": "This rule does NOT apply when the user explicitly asks for backend work first.",
			"image_surface":      "screen",
			"image_detail":       "Use __IMG_<SLOT>__ placeholder tokens, then swap them in a final edit_file pass.",
			"resume_detail":      "Treat \"continue\" or \"devam et\" as a resume signal when it follows a failed run.",
		}
	}
	return map[string]string{
		"first_gen_intro":    "## First Generation Role\nFor first generation only, build production-grade web applications: SaaS apps,\ndashboards, storefronts, sites, blogs, and workflow tools. Use React,\nTypeScript, and Tailwind in the project runtime. Designs must be specific to\nthe user's domain, responsive, accessible, and free of overlapping text or\nbroken states.\n",
		"home_first_title":   "design brief + layout & home first",
		"home_first_rule":    "If DESIGN.md does not exist, your VERY FIRST assistant turn MUST include\ncreate_design with concrete values for register, design_direction,\ncolor_system, typography, layout, components, imagery, and accessibility.\nDo not create DESIGN.md with write_files unless create_design is unavailable.\n\nIf app/layout.tsx or app/page.tsx does not exist, that same first assistant\nturn MUST also include write_files for app/layout.tsx and app/page.tsx. Keep\ntool arguments valid JSON by writing smaller files or splitting large work\nacross multiple structured tool calls.\nDESIGN.md, layout, and home go in the SAME first turn when all are missing.\nAll other pages, components, sections, route handlers, and assets come AFTER.\n\nThis rule does NOT require rewriting app/layout.tsx or app/page.tsx when both\nalready exist. But if DESIGN.md is missing during first generation, create it\nbefore broad visual work.",
		"home_label":         "layout + home",
		"visible_surface":    "page/component",
		"run_order":          "  1. DESIGN.md + app/layout.tsx + app/page.tsx (Design Brief + Layout & Home First)\n  2. Every other page + component with stubs   (UI First)\n  3. Image generation                          (Images Last)\n  4. Database / API routes / integrations      (when required)",
		"backend_exceptions": "This rule does NOT apply when:\n- The user explicitly asks for backend / DB / integration work first\n  (\"hook up Stripe now\", \"switch the stubs to real data\").\n- The project already has the UI built and the request is a wiring task.\n- A project architecture addendum says backend is required by default for the\n  requested product behavior, such as content management, admin workflows,\n  contact submissions, persistence, auth, or cross-device data.",
		"image_surface":      "page",
		"image_detail":       "When a file needs imagery, write it first using a distinctive placeholder\ntoken as the image src: \"__IMG_<SLOT>__\". Each placeholder must appear exactly\nonce in the project so a later edit_file can swap it unambiguously.\n\nOnce every file is written, enter the image phase:\n1. Call generate_image for each placeholder, passing a detailed prompt,\n   width, height, and style.\n2. Immediately follow with edit_file, old_string = the placeholder token\n   (e.g. \"__IMG_HERO__\"), new_string = the returned URL.\n3. Repeat for every placeholder.",
		"resume_detail":      "Treat \"continue\" or \"devam et\" as a resume signal when it follows a failed run.",
	}
}

func editPromptVars(profile *AgentProfile) map[string]string {
	if profile != nil && profile.ID == AgentIDMobileBuilder {
		return map[string]string{
			"project_kind":         "mobile project",
			"platform_edit_rules":  "Make minimal, targeted changes that address the user's request. Preserve existing functionality, design patterns, and code style. Do not rewrite entire files unless the user explicitly asks for a redesign.\nRespect the existing file structure; do not create new files unless needed.\nWhen the user asks to change an image, replace the existing source directly; only use __IMG_<SLOT>__ placeholders when adding a brand-new image.",
			"design_md_discipline": "",
		}
	}
	return map[string]string{
		"project_kind":         "app",
		"platform_edit_rules":  "Preserve behavior and structure. Fix only what the user asked for. Do not redesign, refactor, or globally clean up unless explicitly requested.\nUse existing semantic tokens, classes, and component patterns where available. Avoid hardcoded light backgrounds inside dark UI.",
		"design_md_discipline": "## DESIGN.md Discipline\nOnly update DESIGN.md for broad redesigns, new design systems, or explicit\nuser requests. Do not update DESIGN.md for targeted bug fixes such as dark\nmode, spacing, broken layout, or color contrast fixes.",
	}
}

func resolveLangfusePrompt(ctx context.Context, deps *RunDependencies, name, fallback string, vars map[string]string) (string, observability.PromptLink) {
	link := observability.PromptLink{Name: name}
	if deps == nil || deps.ResolveLangfusePrompt == nil || name == "" {
		return fallback, link
	}
	compiled, err := deps.ResolveLangfusePrompt(ctx, name, fallback, vars)
	if err != nil || strings.TrimSpace(compiled.Text) == "" {
		if err != nil {
			slog.Warn("failed to resolve Langfuse prompt, using local fallback", "error", err, "prompt", name)
		}
		return fallback, link
	}
	if compiled.Name == "" {
		compiled.Name = name
	}
	return compiled.Text, observability.PromptLink{Name: compiled.Name, Version: compiled.Version}
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
	text, _ := buildAddendumWithLangfuse(contextWithoutLangfuse(), project, depsWithoutLangfuse(deps))
	return text
}

func buildAddendumWithLangfuse(ctx context.Context, project *models.AppProject, deps *RunDependencies) (string, []observability.PromptLink) {
	if project == nil {
		return "", nil
	}
	if project.ProjectType == models.ProjectTypeMobileAppNative {
		var parts []string
		var links []observability.PromptLink
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
				if a, link := mobileNativeAddendumWithLangfuse(ctx, deps, cfg); a != "" {
					parts = append(parts, a)
					links = append(links, link)
				}
			}
		}
		if projectHasDatabase(project) {
			a, link := buildDatabaseCapabilityWithLangfuse(ctx, deps, DatabaseAddendumNative, "native_backend")
			parts = append(parts, a)
			links = append(links, link)
		}
		if projectHasLocalDatabase(project) {
			a, link := buildDatabaseCapabilityWithLangfuse(ctx, deps, LocalDatabaseAddendum, "mobile_local")
			parts = append(parts, a)
			links = append(links, link)
		}
		if len(parts) == 0 {
			return "", nil
		}
		return strings.Join(parts, "\n\n"), links
	}

	var parts []string
	var links []observability.PromptLink
	isEcommerce := project.ProjectType == "ecommerce"
	isBackendOnly := project.FrontendRuntime == models.FrontendRuntimeNone
	if isBackendOnly {
		a, link := buildArchitectureAddendumWithLangfuse(ctx, deps, BackendOnlyAddendum, "backend_only")
		parts = append(parts, a)
		links = append(links, link)
	} else if project.UsesSplitWebRuntime() {
		a, link := buildArchitectureAddendumWithLangfuse(ctx, deps, SplitWebAddendum, "split_web")
		parts = append(parts, a)
		links = append(links, link)
	}
	if isEcommerce {
		a, link := resolveLangfusePrompt(ctx, deps, observability.PromptAgentEcommerceAddendum, EcommerceAddendum, nil)
		parts = append(parts, a)
		links = append(links, link)
	}
	if projectHasAuth(project) {
		a, link := resolveLangfusePrompt(ctx, deps, observability.PromptAgentAuthAddendum, AuthAddendum, nil)
		parts = append(parts, a)
		links = append(links, link)
	}
	if (project.UsesSplitWebRuntime() || isBackendOnly) && projectHasDatabase(project) {
		a, link := buildDatabaseCapabilityWithLangfuse(ctx, deps, DatabaseAddendumSplitWeb, "split_web_backend")
		parts = append(parts, a)
		links = append(links, link)
	} else if !isEcommerce && projectHasDatabase(project) {
		a, link := buildDatabaseCapabilityWithLangfuse(ctx, deps, DatabaseAddendum, "nextjs_monolith")
		parts = append(parts, a)
		links = append(links, link)
	}
	if projectHasAI(project) {
		a, link := buildAIAddendumWithLangfuse(ctx, deps, project)
		parts = append(parts, a)
		links = append(links, link)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "\n\n"), links
}

// mobileNativeAddendum formats the current MobileAppConfig as a system-message
// addendum so the agent sees the project's enabled native features on every
// turn without calling read_mobile_config first.
func mobileNativeAddendum(cfg *models.MobileAppConfig) string {
	text, _ := mobileNativeAddendumWithLangfuse(contextWithoutLangfuse(), nil, cfg)
	return text
}

func mobileNativeAddendumWithLangfuse(ctx context.Context, deps *RunDependencies, cfg *models.MobileAppConfig) (string, observability.PromptLink) {
	if cfg == nil {
		return "", observability.PromptLink{}
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
	fallback := sb.String()
	return resolveLangfusePrompt(ctx, deps, observability.PromptAgentMobileConfigAddendum, fallback, map[string]string{
		"bridge_methods":        strings.Join(methods, ", "),
		"url_scheme":            cfg.URLScheme,
		"universal_link_domain": cfg.UniversalLinkDomain,
		"biometric_enabled":     fmt.Sprintf("%v", cfg.BiometricEnabled),
		"push_enabled":          fmt.Sprintf("%v", cfg.PushEnabled),
		"dark_mode_enabled":     fmt.Sprintf("%v", cfg.DarkModeEnabled),
		"offline_enabled":       fmt.Sprintf("%v", cfg.OfflineEnabled),
		"permissions":           strings.Join(permissions, ", "),
	})
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
