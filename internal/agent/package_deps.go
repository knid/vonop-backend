package agent

import (
	"encoding/json"
	"log/slog"
)

// addDependenciesToPackageJSON merges the given name→version pairs into the
// project's package.json dependencies map. Used by tools that inject
// template files with third-party imports (e.g. provision_database injects
// lib/db.ts which imports @neondatabase/serverless and drizzle-orm). The
// LLM didn't write those imports, so the tool must own the matching deps.
//
// Semantics: only adds entries that are missing. Never downgrades or
// upgrades an existing entry — the LLM may have set a specific version on
// purpose. Silently no-ops if package.json is missing or unparseable
// (logged as a warning); we never want a dep update to fail a tool call.
func addDependenciesToPackageJSON(rc *RunContext, deps map[string]string) {
	if rc == nil || rc.FileMap == nil || len(deps) == 0 {
		return
	}

	current, ok := rc.FileMap.Get("package.json")
	if !ok || current == "" {
		slog.Warn("addDependenciesToPackageJSON: package.json not found in file map")
		return
	}

	var pkg map[string]any
	if err := json.Unmarshal([]byte(current), &pkg); err != nil {
		slog.Warn("addDependenciesToPackageJSON: package.json is not valid JSON", "error", err)
		return
	}

	depsMap, _ := pkg["dependencies"].(map[string]any)
	if depsMap == nil {
		depsMap = make(map[string]any)
		pkg["dependencies"] = depsMap
	}

	added := false
	for name, version := range deps {
		if _, exists := depsMap[name]; exists {
			continue
		}
		depsMap[name] = version
		added = true
	}

	if !added {
		return
	}

	out, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		slog.Warn("addDependenciesToPackageJSON: re-marshal failed", "error", err)
		return
	}
	out = append(out, '\n')

	rc.FileMap.Set("package.json", string(out))
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID:     rc.RunID,
		Path:      "package.json",
		Operation: "edit",
		Bytes:     len(out),
		Content:   string(out),
	})
}
