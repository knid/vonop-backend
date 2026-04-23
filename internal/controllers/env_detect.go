package controllers

import (
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/models"
)

type RequiredEnvVar struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	IsPublic    bool   `json:"is_public"`
	Provider    string `json:"provider,omitempty"`
}

// --- Regex for env var detection ---
// Matches both dot notation (process.env.FOO) and bracket notation (process.env["FOO"] / process.env['FOO'])
var processEnvRegex = regexp.MustCompile(`process\.env\.([A-Z_][A-Z0-9_]*)|process\.env\[["']([A-Z_][A-Z0-9_]*)["']\]`)

// alwaysExcludedEnvVars are env vars that are always provided by the system
// and should never appear in "missing" detection.
var alwaysExcludedEnvVars = map[string]bool{
	"NODE_ENV": true,
}

// envVarNameRegex validates that an env var name contains only uppercase letters, digits, and underscores.
var envVarNameRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,100}$`)

// detectEnvVarsFromCode scans all generated file contents for process.env.* references
// and returns a sorted list of unique env var keys, excluding system-injected ones.
// Integration-provided vars (DATABASE_URL, AUTH_SECRET, etc.) are NOT excluded here
// so they show as missing when the integration is disconnected.
func detectEnvVarsFromCode(files map[string]string) []string {
	seen := map[string]bool{}
	for _, content := range files {
		for _, m := range processEnvRegex.FindAllStringSubmatch(content, -1) {
			// m[1] is dot notation capture, m[2] is bracket notation capture
			key := m[1]
			if key == "" {
				key = m[2]
			}
			if key != "" && !alwaysExcludedEnvVars[key] {
				seen[key] = true
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// computeMissingEnvVars merges LLM-declared env vars with code-scanned keys,
// then returns only those not yet configured in the project.
func (c *Controller) computeMissingEnvVars(project *models.AppProject, llmVars []RequiredEnvVar, codeKeys []string) []RequiredEnvVar {
	existing := c.CollectAllEnvVars(project)

	required := map[string]RequiredEnvVar{}
	for _, v := range llmVars {
		if !envVarNameRegex.MatchString(v.Key) {
			slog.Warn("skipping invalid env var name from LLM", "key", v.Key, "project_id", project.ID)
			continue
		}
		required[v.Key] = v
	}
	for _, key := range codeKeys {
		if !envVarNameRegex.MatchString(key) {
			continue
		}
		if _, ok := required[key]; !ok {
			required[key] = RequiredEnvVar{
				Key:         key,
				Description: "Used in generated code",
				IsPublic:    strings.HasPrefix(key, "NEXT_PUBLIC_"),
			}
		}
	}

	var missing []RequiredEnvVar
	for _, v := range required {
		// Check both the raw key and the NEXT_PUBLIC_ prefixed form,
		// since CollectAllEnvVars auto-prefixes public custom vars.
		if existing[v.Key] == "" && existing["NEXT_PUBLIC_"+v.Key] == "" {
			missing = append(missing, v)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Key < missing[j].Key })
	return missing
}
