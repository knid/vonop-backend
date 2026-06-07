package agent

import (
	"fmt"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/templates"
)

// InjectTemplates writes the project-type-appropriate template files into an
// empty repo. Called exactly once, during setup of a draft project's first run.
// Returns the number of files written.
func InjectTemplates(repo *gitrepo.Repo, project *models.AppProject) (int, error) {
	lang := constants.DetectLanguage(project.Prompt)
	written := 0

	if project.FrontendRuntime == models.FrontendRuntimeNone {
		for path, content := range minimalBackendFiles() {
			if content == "" {
				continue
			}
			if err := repo.WriteFile(path, []byte(content)); err != nil {
				return written, fmt.Errorf("write %s: %w", path, err)
			}
			written++
		}
		return written, nil
	}

	if project.UsesSplitWebRuntime() {
		for _, p := range templates.ViteFilePaths {
			content := templates.GetViteTemplate(p)
			if content == "" {
				continue
			}
			dest := "frontend/" + p
			if err := repo.WriteFile(dest, []byte(content)); err != nil {
				return written, fmt.Errorf("write %s: %w", dest, err)
			}
			written++
		}
		return written, nil
	}

	// Pull color scheme / typography off Settings for theme generation.
	// Non-ecommerce projects still get these tokens via BaseFilePaths'
	// globals.css below — the LLM reads @theme to inform its design.
	var colorScheme, darkColorScheme map[string]any
	var typography map[string]any
	if project.Settings != nil {
		if v, ok := project.Settings["color_scheme"].(map[string]any); ok {
			colorScheme = v
		}
		if v, ok := project.Settings["dark_color_scheme"].(map[string]any); ok {
			darkColorScheme = v
		}
		if v, ok := project.Settings["typography"].(map[string]any); ok {
			typography = v
		}
	}

	// Base templates (package.json, tsconfig, next.config, etc.).
	// app/globals.css is skipped here for ecommerce because the ecommerce
	// branch below writes a richer type-specific version via
	// BuildEcommerceGlobalsCSSTheme. Non-ecommerce web projects pick up
	// the base globals.css here.
	for _, p := range templates.BaseFilePaths {
		if p == "app/globals.css" && project.ProjectType == "ecommerce" {
			continue
		}
		content := templates.GetContentWithLang(p, colorScheme, darkColorScheme, project.ProjectType, nil, lang)
		if content == "" {
			continue
		}
		if err := repo.WriteFile(p, []byte(content)); err != nil {
			return written, fmt.Errorf("write %s: %w", p, err)
		}
		written++
	}

	// UI component templates (lib/utils, components/ui/*)
	for _, p := range templates.UIFilePaths {
		content := templates.GetUIContent(p)
		if content == "" {
			continue
		}
		if err := repo.WriteFile(p, []byte(content)); err != nil {
			return written, fmt.Errorf("write %s: %w", p, err)
		}
		written++
	}

	// Ecommerce templates (when applicable)
	if project.ProjectType == "ecommerce" {
		needsAuth := false
		if project.Settings != nil {
			if v, ok := project.Settings["needs_auth"].(bool); ok {
				needsAuth = v
			}
		}
		for _, p := range templates.EcommerceFilePaths {
			var content string
			switch p {
			case "components/ecommerce/providers.tsx":
				content = templates.BuildEcommerceProviders(needsAuth)
			case "app/layout.tsx":
				// Ship the root layout pre-wired with fonts, Providers,
				// SiteHeader/SiteFooter and Toaster. `lang` comes from
				// DetectLanguage so screen readers and browser hyphenation
				// behave correctly from the first render — and the LLM
				// knows (from the system prompt) not to overwrite this file.
				content = templates.BuildEcommerceLayout(project.Name, typography, lang)
			case "app/globals.css":
				content = templates.BuildEcommerceGlobalsCSSTheme(colorScheme, darkColorScheme)
			default:
				content = templates.GetEcommerceContent(p, lang)
			}
			if content == "" {
				continue
			}
			if err := repo.WriteFile(p, []byte(content)); err != nil {
				return written, fmt.Errorf("write %s: %w", p, err)
			}
			written++
		}
	}

	return written, nil
}
