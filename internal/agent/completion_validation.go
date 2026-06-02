package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/models"
)

func validateGeneratedApp(fileMap *SafeFileMap, state *BuildState, prompt string, isFirstGen bool, projectType string) error {
	if fileMap == nil {
		return fmt.Errorf("generated app validation failed: no files are available")
	}
	if !isFirstGen {
		return validateNoImagePlaceholders(fileMap)
	}
	if projectType == models.ProjectTypeMobileAppNative {
		if _, ok := fileMap.Get("mobile/package.json"); !ok {
			return fmt.Errorf("generated app validation failed: mobile/package.json is missing")
		}
		if _, ok := fileMap.Get("mobile/app/_layout.tsx"); !ok {
			return fmt.Errorf("generated app validation failed: mobile/app/_layout.tsx is missing")
		}
		if _, ok := fileMap.Get("mobile/app/(app)/index.tsx"); !ok {
			return fmt.Errorf("generated app validation failed: mobile/app/(app)/index.tsx is missing")
		}
		if err := validateNoImagePlaceholders(fileMap); err != nil {
			return err
		}
		if state != nil {
			state.SetPhase(BuildPhaseFinal)
		}
		return nil
	}
	if _, ok := fileMap.Get("app/layout.tsx"); !ok {
		return fmt.Errorf("generated app validation failed: app/layout.tsx is missing")
	}
	if _, ok := fileMap.Get("app/page.tsx"); !ok {
		return fmt.Errorf("generated app validation failed: app/page.tsx is missing")
	}
	if err := validateNoImagePlaceholders(fileMap); err != nil {
		return err
	}
	if isOperationalSaaSRequest(prompt) {
		if err := validateOperationalSaaSShape(fileMap, prompt); err != nil {
			return err
		}
	}
	if state != nil {
		state.SetPhase(BuildPhaseFinal)
	}
	return nil
}

func validateNoImagePlaceholders(fileMap *SafeFileMap) error {
	var paths []string
	for path, content := range fileMap.Snapshot() {
		if strings.Contains(content, "__IMG_") {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	return fmt.Errorf("generated app validation failed: unresolved image placeholder remains in %s", strings.Join(paths, ", "))
}

func isOperationalSaaSRequest(prompt string) bool {
	lower := strings.ToLower(prompt)
	if strings.Contains(lower, "landing page") || strings.Contains(lower, "marketing site") {
		return false
	}
	hits := 0
	for _, needle := range []string{
		"saas", "dashboard", "admin", "role", "captain", "logbook",
		"form", "history", "settings", "authentication", "auth",
	} {
		if strings.Contains(lower, needle) {
			hits++
		}
	}
	return hits >= 3
}

func validateOperationalSaaSShape(fileMap *SafeFileMap, prompt string) error {
	content := strings.ToLower(joinVisibleAppContent(fileMap))
	checks := []struct {
		promptNeedles  []string
		contentNeedles []string
		label          string
	}{
		{[]string{"admin", "owner"}, []string{"admin", "owner", "fleet"}, "admin/owner dashboard"},
		{[]string{"captain"}, []string{"captain"}, "captain interface"},
		{[]string{"role", "access"}, []string{"role", "assigned", "permission", "access"}, "role-specific access"},
		{[]string{"form", "entry", "input"}, []string{"form", "input", "textarea", "submit", "entry"}, "data entry form"},
		{[]string{"history", "past entries"}, []string{"history", "past", "chronological", "entry"}, "log history"},
		{[]string{"alert", "threshold", "battery", "service"}, []string{"alert", "threshold", "battery", "service", "warning"}, "smart alerts"},
	}

	var missing []string
	lowerPrompt := strings.ToLower(prompt)
	for _, check := range checks {
		if !containsAny(lowerPrompt, check.promptNeedles) {
			continue
		}
		if !containsAny(content, check.contentNeedles) {
			missing = append(missing, check.label)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("generated app validation failed: operational SaaS request is missing %s", strings.Join(missing, ", "))
}

func joinVisibleAppContent(fileMap *SafeFileMap) string {
	var paths []string
	for path := range fileMap.Snapshot() {
		if isProtectedRead(path) {
			continue
		}
		if strings.HasSuffix(path, ".tsx") ||
			strings.HasSuffix(path, ".ts") ||
			strings.HasSuffix(path, ".css") ||
			strings.HasSuffix(path, ".json") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	var sb strings.Builder
	for _, path := range paths {
		content, ok := fileMap.Get(path)
		if !ok {
			continue
		}
		sb.WriteString("\n")
		sb.WriteString(path)
		sb.WriteString("\n")
		sb.WriteString(content)
	}
	return sb.String()
}

func containsAny(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
