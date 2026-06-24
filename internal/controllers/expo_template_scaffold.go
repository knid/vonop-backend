package controllers

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/knid/wemob/internal/agent/templates/expo_native"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
)

// scaffoldExpoTemplate copies every file in the embedded wemob-expo-template
// FS into the project's git repo under mobile/<path>, substituting the
// __FOO__ placeholders the template carries with real per-project values.
//
// Values are substituted at scaffold time so the on-disk app.json is always
// a valid Expo config that the preview sandbox and user export can consume.
//
// Existing files in mobile/ are NOT overwritten — this is a one-shot
// scaffold meant to run on a fresh project.
//
// Binary assets (PNG icons under assets/) are written verbatim. Text files
// get the substitutions applied; values are JSON-string-escaped because
// every placeholder in the template lives inside a JSON string or a
// TS double-quoted literal.
func scaffoldExpoTemplate(c *Controller, repo *gitrepo.Repo, project *models.AppProject, authEnabled bool) error {
	if repo == nil {
		return fmt.Errorf("scaffoldExpoTemplate: nil repo")
	}
	if project == nil {
		return fmt.Errorf("scaffoldExpoTemplate: nil project")
	}
	subs := expoTemplateSubstitutions(c, project, authEnabled)

	return fs.WalkDir(expo_native.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Skip the embed.go directive file itself — it's pulled in by `all:`
		// otherwise but doesn't belong in the user's repo.
		if p == "embed.go" || strings.HasSuffix(p, "/embed.go") {
			return nil
		}
		// When auth is disabled, omit the entire (auth) route group and its
		// sign-in / sign-up screens so the app boots straight into (app).
		if !authEnabled && (strings.HasPrefix(p, "app/(auth)/") || p == "app/(auth)/_layout.tsx" || p == "app/(auth)/sign-in.tsx" || p == "app/(auth)/sign-up.tsx") {
			return nil
		}
		data, err := expo_native.FS.ReadFile(p)
		if err != nil {
			return fmt.Errorf("scaffoldExpoTemplate: read %s: %w", p, err)
		}
		if isTextTemplateFile(p) {
			data = applyTemplateSubstitutions(data, subs)
		}
		dest := path.Join("mobile", p)
		if err := repo.WriteFile(dest, data); err != nil {
			return fmt.Errorf("scaffoldExpoTemplate: write %s: %w", dest, err)
		}
		return nil
	})
}

// expoTemplateSubstitutions returns the placeholder→value map used at
// scaffold time. Project-stable values are derived from the project.
func expoTemplateSubstitutions(c *Controller, project *models.AppProject, authEnabled bool) map[string]string {
	bundleID := bundleIDForProject(project, "")
	return map[string]string{
		"__APP_TITLE__":         project.Name,
		"__APP_SLUG__":          project.Slug,
		"__BUNDLE_ID__":         bundleID,
		"__PACKAGE_NAME__":      bundleID,
		"__MARKETING_VERSION__": "1.0.0",
		"__BUILD_NUMBER__":      "1",
		"__SPLASH_COLOR__":      "#FFFFFF",
		"__APP_SCHEME__":        strings.ToLower(strings.TrimSpace(project.Slug)),
	}
}

// applyTemplateSubstitutions replaces each placeholder in content with the
// corresponding value, JSON-string-escaped so it's safe to drop inside
// JSON or TS/JS double-quoted string literals.
func applyTemplateSubstitutions(content []byte, subs map[string]string) []byte {
	s := string(content)
	for placeholder, value := range subs {
		s = strings.ReplaceAll(s, placeholder, jsonStringEscape(value))
	}
	return []byte(s)
}

// jsonStringEscape returns the JSON encoding of s with the surrounding
// quotes stripped, suitable for inserting into an existing "..." literal.
func jsonStringEscape(s string) string {
	b, err := json.Marshal(s)
	if err != nil || len(b) < 2 {
		return s
	}
	return string(b[1 : len(b)-1])
}

// isTextTemplateFile reports whether the file at path should pass through
// placeholder substitution. Binary assets must not be touched — even a
// no-op string replacement could corrupt them if encoding ever drifts.
func isTextTemplateFile(p string) bool {
	switch filepath.Ext(p) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico",
		".ttf", ".otf", ".woff", ".woff2":
		return false
	}
	return true
}
