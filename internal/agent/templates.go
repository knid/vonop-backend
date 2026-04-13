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

	// Base templates (package.json, tsconfig, next.config, etc.) — skip
	// app/globals.css so the LLM writes its own theme.
	for _, p := range templates.BaseFilePaths {
		if p == "app/globals.css" {
			continue
		}
		content := templates.GetContentWithLang(p, nil, nil, project.ProjectType, nil, lang)
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
			if p == "components/ecommerce/providers.tsx" {
				content = templates.BuildEcommerceProviders(needsAuth)
			} else {
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
