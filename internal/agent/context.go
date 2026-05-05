package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// mobileConfigGetter abstracts GetMobileAppConfig so context.go can be tested
// without a real *database.Database. *database.Database satisfies this interface.
type mobileConfigGetter interface {
	GetMobileAppConfig(projectID string) (*models.MobileAppConfig, error)
}

// BuildSystemMessages returns the cached system messages for a run:
//
//  1. Base prompt          (BasePromptWeb or BasePromptMobileNative — by project type)
//  2. Variant addendum     (stable across this project's runs, if applicable)
//  3. File tree            (stable within this run, reused across turns)
//  4. Project assets       (per-turn — asset uploads/deletes invalidate this block only)
//
// anthropic.go marks up to 4 system blocks with cache_control, so each block
// becomes its own cache breakpoint for prompt caching.
//
// deps is passed in so the AI addendum can render only the chat models the
// current user's subscription tier can actually reach; passing nil degrades
// to a conservative generic block.
func BuildSystemMessages(project *models.AppProject, fileMap *SafeFileMap, deps *RunDependencies, attachmentsPool []models.AppAsset) []utils.Message {
	base := BasePromptWeb
	if project != nil {
		switch project.ProjectType {
		case models.ProjectTypeMobileAppNative:
			base = BasePromptMobileNative
		}
	}
	addendum := buildAddendum(project, deps)

	msgs := []utils.Message{{Role: "system", Content: base}}
	if addendum != "" {
		msgs = append(msgs, utils.Message{Role: "system", Content: addendum})
	}
	msgs = append(msgs, utils.Message{Role: "system", Content: buildFileTreeBlock(fileMap)})
	if manifest := AttachmentsAddendum(attachmentsPool); manifest != "" {
		msgs = append(msgs, utils.Message{Role: "system", Content: manifest})
	}
	return msgs
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
	if isEcommerce {
		parts = append(parts, EcommerceAddendum)
	}
	if projectHasAuth(project) {
		parts = append(parts, AuthAddendum)
	}
	if !isEcommerce && projectHasDatabase(project) {
		parts = append(parts, DatabaseAddendum)
	}
	if projectHasAI(project) {
		parts = append(parts, buildAIAddendum(deps))
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
	for _, p := range paths {
		fmt.Fprintf(&sb, "%s (%d bytes)\n", p, len(files[p]))
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
