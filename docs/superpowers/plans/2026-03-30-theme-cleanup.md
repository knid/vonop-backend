# Theme Cleanup Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix recursive base64 encoding bug in AppTheme, remove dead fields, remove redundant theme from LLM context/API/git.

**Architecture:** Keep JSONB storage but change type from `[]byte` to `json.RawMessage` to prevent encoding accumulation. Remove 3 unused fields from `AppTheme`. Remove theme from API response, LLM context, and git repo. Write a one-time migration script to clean existing data.

**Tech Stack:** Go, Bun ORM, PostgreSQL

**Spec:** `docs/superpowers/specs/2026-03-30-theme-cleanup-design.md`

---

### Task 1: Slim down `AppTheme` and fix encoding type

**Files:**
- Modify: `internal/models/app_project.go:9-15` — remove fields from `AppTheme`, change `Theme` type
- Modify: `internal/models/app_snapshot.go:14` — change `Theme` type

- [ ] **Step 1: Update `AppTheme` struct and `Theme` field in `app_project.go`**

In `internal/models/app_project.go`, add `"encoding/json"` to imports, then replace the `AppTheme` struct and `Theme` field:

```go
type AppTheme struct {
	ColorScheme map[string]any `json:"color_scheme"`
	Typography  map[string]any `json:"typography,omitempty"`
}
```

Change line 28 from:
```go
Theme           []byte         `bun:",type:jsonb,default:'{}'"` // stores AppTheme as JSON
```
to:
```go
Theme           json.RawMessage `bun:",type:jsonb,default:'{}'"` // stores AppTheme as JSON
```

- [ ] **Step 2: Update `Theme` field in `app_snapshot.go`**

In `internal/models/app_snapshot.go`, add `"encoding/json"` to imports, then change line 14 from:
```go
Theme        []byte      `bun:",type:jsonb,default:'{}'"` // snapshot of theme
```
to:
```go
Theme        json.RawMessage `bun:",type:jsonb,default:'{}'"` // snapshot of theme
```

- [ ] **Step 3: Build to verify**

Run: `go build ./...`
Expected: Compiles with no errors (json.RawMessage is a drop-in replacement for []byte).

- [ ] **Step 4: Commit**

```bash
git add internal/models/app_project.go internal/models/app_snapshot.go
git commit -m "fix: slim down AppTheme struct, change Theme to json.RawMessage to prevent base64 re-encoding"
```

---

### Task 2: Remove `DarkColorScheme` from `appFileStructure`

**Files:**
- Modify: `internal/controllers/app_codegen_controller.go:145` — remove field from struct

- [ ] **Step 1: Remove `DarkColorScheme` from `appFileStructure`**

In `internal/controllers/app_codegen_controller.go`, remove line 145:
```go
DarkColorScheme map[string]any     `json:"darkColorScheme,omitempty"`
```

- [ ] **Step 2: Build to verify**

Run: `go build ./...`
Expected: Compilation errors in files that reference `structure.DarkColorScheme` — these are fixed in the next tasks.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_codegen_controller.go
git commit -m "refactor: remove DarkColorScheme from appFileStructure"
```

---

### Task 3: Remove `DarkColorScheme` from theme construction in generation

**Files:**
- Modify: `internal/controllers/app_builder_generation.go:293-297,596-601` — remove DarkColorScheme from themeData construction
- Modify: `internal/views/app_views.go:70-75` — remove DarkColorScheme from themeData construction

- [ ] **Step 1: Update themeData in `streamSingleShotGeneration` (~line 293)**

In `internal/controllers/app_builder_generation.go`, change:
```go
	themeData := models.AppTheme{
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
```
to:
```go
	themeData := models.AppTheme{
		ColorScheme: structure.ColorScheme,
		Typography:  structure.Typography,
	}
```

- [ ] **Step 2: Update themeData in ecommerce generation (~line 596)**

In the same file, find the second `themeData` construction and apply the same change:
```go
	themeData := models.AppTheme{
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
```
to:
```go
	themeData := models.AppTheme{
		ColorScheme: structure.ColorScheme,
		Typography:  structure.Typography,
	}
```

- [ ] **Step 3: Update themeData in screenshot-to-structure flow in `app_views.go` (~line 70)**

In `internal/views/app_views.go`, change:
```go
	themeData := models.AppTheme{
		ColorScheme:     structure.ColorScheme,
		DarkColorScheme: structure.DarkColorScheme,
		Typography:      structure.Typography,
	}
```
to:
```go
	themeData := models.AppTheme{
		ColorScheme: structure.ColorScheme,
		Typography:  structure.Typography,
	}
```

- [ ] **Step 4: Build to verify**

Run: `go build ./...`
Expected: No more references to `DarkColorScheme` or `structure.DarkColorScheme`.

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/app_builder_generation.go internal/views/app_views.go
git commit -m "refactor: remove DarkColorScheme from theme construction"
```

---

### Task 4: Clean up `buildGlobalsCSSTheme` and downstream signatures

**Files:**
- Modify: `internal/controllers/app_codegen_controller.go:548,652,842,846` — remove darkColorScheme parameter
- Modify: `internal/controllers/app_builder_generation.go:327,334,726` — update call sites

- [ ] **Step 1: Update `buildGlobalsCSSTheme` signature**

In `internal/controllers/app_codegen_controller.go`, change:
```go
func buildGlobalsCSSTheme(colorScheme, darkColorScheme map[string]any, isMobile bool) string {
```
to:
```go
func buildGlobalsCSSTheme(colorScheme map[string]any, isMobile bool) string {
```

- [ ] **Step 2: Update `buildEcommerceGlobalsCSSTheme` signature and its call to `buildGlobalsCSSTheme`**

In the same file, change:
```go
func buildEcommerceGlobalsCSSTheme(colorScheme, darkColorScheme map[string]any) string {
	base := buildGlobalsCSSTheme(colorScheme, darkColorScheme, false)
```
to:
```go
func buildEcommerceGlobalsCSSTheme(colorScheme map[string]any) string {
	base := buildGlobalsCSSTheme(colorScheme, false)
```

- [ ] **Step 3: Update `getTemplateContentWithLang` signature and its call to `buildGlobalsCSSTheme`**

Change:
```go
func getTemplateContent(path string, colorScheme, darkColorScheme map[string]any, projectType string, dataModels []DataModel) string {
	return getTemplateContentWithLang(path, colorScheme, darkColorScheme, projectType, dataModels, "en")
}

func getTemplateContentWithLang(path string, colorScheme, darkColorScheme map[string]any, projectType string, dataModels []DataModel, lang string) string {
	// Dynamic templates that can't be static files
	switch path {
	case "app/globals.css":
		return buildGlobalsCSSTheme(colorScheme, darkColorScheme, projectType == "mobile_app")
```
to:
```go
func getTemplateContent(path string, colorScheme map[string]any, projectType string, dataModels []DataModel) string {
	return getTemplateContentWithLang(path, colorScheme, projectType, dataModels, "en")
}

func getTemplateContentWithLang(path string, colorScheme map[string]any, projectType string, dataModels []DataModel, lang string) string {
	// Dynamic templates that can't be static files
	switch path {
	case "app/globals.css":
		return buildGlobalsCSSTheme(colorScheme, projectType == "mobile_app")
```

- [ ] **Step 4: Update call sites in `app_builder_generation.go`**

Find and update these three calls:

```go
// ~line 327: change
if cssContent := buildEcommerceGlobalsCSSTheme(theme.ColorScheme, theme.DarkColorScheme); cssContent != "" {
// to:
if cssContent := buildEcommerceGlobalsCSSTheme(theme.ColorScheme); cssContent != "" {

// ~line 334: change
if cssContent := buildGlobalsCSSTheme(theme.ColorScheme, theme.DarkColorScheme, isMobile); cssContent != "" {
// to:
if cssContent := buildGlobalsCSSTheme(theme.ColorScheme, isMobile); cssContent != "" {

// ~line 726: change
if cssContent := buildEcommerceGlobalsCSSTheme(theme.ColorScheme, theme.DarkColorScheme); cssContent != "" {
// to:
if cssContent := buildEcommerceGlobalsCSSTheme(theme.ColorScheme); cssContent != "" {
```

- [ ] **Step 5: Update call site in `app_builder_controller.go`**

Find the `getTemplateContentWithLang` call (~line 340):
```go
content := getTemplateContentWithLang(tp, nil, nil, project.ProjectType, nil, lang)
```
Change to:
```go
content := getTemplateContentWithLang(tp, nil, project.ProjectType, nil, lang)
```

- [ ] **Step 6: Build to verify**

Run: `go build ./...`
Expected: Compiles cleanly. No remaining references to `darkColorScheme` in function signatures or call sites.

- [ ] **Step 7: Commit**

```bash
git add internal/controllers/app_codegen_controller.go internal/controllers/app_builder_generation.go internal/controllers/app_builder_controller.go
git commit -m "refactor: remove darkColorScheme parameter from CSS theme builder functions"
```

---

### Task 5: Remove theme from LLM context

**Files:**
- Modify: `internal/controllers/app_builder_context.go:732-748,841-858` — remove theme blocks from both context builders

- [ ] **Step 1: Remove theme block from `buildProjectContextForChat`**

In `internal/controllers/app_builder_context.go`, remove lines 732-748 (the entire theme block):
```go
	// Theme
	var theme map[string]any
	if len(project.Theme) > 0 && project.Theme[0] == '{' {
		if err := json.Unmarshal(project.Theme, &theme); err != nil {
			slog.Warn("failed to unmarshal project theme", "error", err, "project_id", project.ID)
		}
	} else if len(project.Theme) > 0 {
		var raw string
		if json.Unmarshal(project.Theme, &raw) == nil && len(raw) > 0 && raw[0] == '{' {
			if err := json.Unmarshal([]byte(raw), &theme); err != nil {
				slog.Warn("failed to unmarshal double-encoded project theme", "error", err, "project_id", project.ID)
			}
		}
	}
	if len(theme) > 0 {
		sb.WriteString(fmt.Sprintf("Theme: %v\n", theme))
	}
```

- [ ] **Step 2: Remove theme block from `buildProjectContext`**

In the same file, remove lines 841-858 (the identical theme block in the other function):
```go
	// Theme
	var theme map[string]any
	if len(project.Theme) > 0 && project.Theme[0] == '{' {
		if err := json.Unmarshal(project.Theme, &theme); err != nil {
			slog.Warn("failed to unmarshal project theme", "error", err, "project_id", project.ID)
		}
	} else if len(project.Theme) > 0 {
		// Theme is not a JSON object (possibly a string or corrupted) — try to unwrap
		var raw string
		if json.Unmarshal(project.Theme, &raw) == nil && len(raw) > 0 && raw[0] == '{' {
			// Double-encoded JSON string — unwrap and parse
			if err := json.Unmarshal([]byte(raw), &theme); err != nil {
				slog.Warn("failed to unmarshal double-encoded project theme", "error", err, "project_id", project.ID)
			}
		}
	}
	if len(theme) > 0 {
		sb.WriteString(fmt.Sprintf("Theme: %v\n", theme))
	}
```

- [ ] **Step 3: Build to verify**

Run: `go build ./...`
Expected: Compiles cleanly. Check that `json` and `slog` imports are still needed by other code in the file (they are).

- [ ] **Step 4: Commit**

```bash
git add internal/controllers/app_builder_context.go
git commit -m "refactor: remove redundant theme dump from LLM context (globals.css already carries theme tokens)"
```

---

### Task 6: Remove theme from API response

**Files:**
- Modify: `internal/responses/app_project.go:22` — remove Theme field
- Modify: `internal/views/app_views.go` — remove all theme unmarshalling and `Theme:` assignments

- [ ] **Step 1: Remove `Theme` from `AppProjectResponse`**

In `internal/responses/app_project.go`, remove line 22:
```go
	Theme       map[string]any `json:"theme"`
```

- [ ] **Step 2: Remove theme unmarshalling and assignments in `app_views.go`**

There are 6 locations in `internal/views/app_views.go` where `var theme map[string]any` is declared and `Theme: theme` is set. At each location:

1. **~line 81-82** (screenshot-to-structure): Remove `var theme map[string]any` and `json.Unmarshal(project.Theme, &theme)`. Remove `Theme: theme,` from the response struct (~line 91).

2. **~line 181-182** (list apps loop): Remove `var theme map[string]any` and `json.Unmarshal(p.Theme, &theme)`. Remove `Theme: theme,` from the response struct (~line 210).

3. **~line 238-239** (get app): Remove `var theme map[string]any` and `json.Unmarshal(project.Theme, &theme)`. Remove `Theme: theme,` from the response struct (~line 291).

4. **~line 324-325** (update app): Remove `var theme map[string]any` and `json.Unmarshal(project.Theme, &theme)`. Remove `Theme: theme,` from the response struct (~line 336).

5. **~line 856-857** (clone as mobile): Remove `var theme map[string]any` and `json.Unmarshal(clone.Theme, &theme)`. Remove `Theme: theme,` from the response struct (~line 865).

6. **~line 1111-1112** (app init): Remove `var theme map[string]any` and `json.Unmarshal(project.Theme, &theme)`. Remove `Theme: theme,` from the response struct (~line 1133).

- [ ] **Step 3: Build to verify**

Run: `go build ./...`
Expected: Compiles cleanly. No remaining references to `Theme` in response structs or view handlers.

- [ ] **Step 4: Commit**

```bash
git add internal/responses/app_project.go internal/views/app_views.go
git commit -m "refactor: remove theme from API response (frontend does not use it)"
```

---

### Task 7: Remove theme from git repo

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:1090-1093` — remove WriteTheme call
- Modify: `internal/controllers/app_project_controller.go:279-282,304-306` — remove WriteTheme call and ReadTheme restore
- Modify: `internal/services/gitrepo/repo.go:288-296` — remove WriteTheme and ReadTheme methods

- [ ] **Step 1: Remove `WriteTheme` call in commit flow**

In `internal/controllers/app_builder_controller.go`, remove lines 1090-1093:
```go
		// Write theme to repo before commit
		if err := repo.WriteTheme(project.Theme); err != nil {
			slog.Warn("failed to write theme to repo", "error", err, "project_id", project.ID)
		}
```

- [ ] **Step 2: Remove `WriteTheme` call in snapshot flow**

In `internal/controllers/app_project_controller.go`, remove lines 279-282:
```go
	// Write theme to repo so it's captured in the commit
	if err := repo.WriteTheme(project.Theme); err != nil {
		slog.Warn("CreateAppSnapshot: failed to write theme", "error", err, "project_id", project.ID)
	}
```

- [ ] **Step 3: Remove theme restore from `RestoreAppSnapshot`**

In the same file, remove the theme restoration block (~lines 304-314):
```go
	// Restore theme from repo
	if themeData, readErr := repo.ReadTheme(); readErr == nil && len(themeData) > 0 && validateThemeJSON(themeData) == nil {
		project.Theme = themeData
		project.UpdatedAt = time.Now()
		updated, updateErr := c.DB.UpdateAppProject(project)
		if updateErr != nil {
			slog.Error("RestoreAppSnapshot: failed to update project theme", "error", updateErr, "project_id", project.ID)
			return project, nil
		}
		return updated, nil
	}
```

And adjust the return at the end of `RestoreAppSnapshot` to just return the project directly.

- [ ] **Step 4: Remove `WriteTheme` and `ReadTheme` methods from gitrepo**

In `internal/services/gitrepo/repo.go`, remove lines 288-296:
```go
// WriteTheme writes the theme JSON to _wemob/theme.json.
func (r *Repo) WriteTheme(themeJSON []byte) error {
	return r.WriteFile(filepath.Join(metaDir, "theme.json"), themeJSON)
}

// ReadTheme reads the theme JSON from _wemob/theme.json.
func (r *Repo) ReadTheme() ([]byte, error) {
	return r.ReadFile(filepath.Join(metaDir, "theme.json"))
}
```

- [ ] **Step 5: Build to verify**

Run: `go build ./...`
Expected: Compiles cleanly. Verify `validateThemeJSON` is still used elsewhere (it is — in `app_codegen_controller.go`).

- [ ] **Step 6: Commit**

```bash
git add internal/controllers/app_builder_controller.go internal/controllers/app_project_controller.go internal/services/gitrepo/repo.go
git commit -m "refactor: remove theme from git repo (theme lives in DB, materialized into globals.css)"
```

---

### Task 8: Write data migration script

**Files:**
- Create: `cmd/fix_theme_encoding/main.go`

- [ ] **Step 1: Create the migration script**

Create `cmd/fix_theme_encoding/main.go`:

```go
package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/lib/pq"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	fixTable(db, "app_projects", "id", "theme")
	fixTable(db, "app_snapshots", "id", "theme")
}

func fixTable(db *sql.DB, table, idCol, themeCol string) {
	query := fmt.Sprintf("SELECT %s, %s::text FROM %s WHERE octet_length(%s::text) > 500", idCol, themeCol, table, themeCol)
	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		log.Fatalf("query %s failed: %v", table, err)
	}
	defer rows.Close()

	fixed := 0
	skipped := 0
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			log.Printf("scan error: %v", err)
			continue
		}

		decoded := decodeRecursive(raw)
		if decoded == raw {
			skipped++
			continue
		}

		// Strip removed fields
		cleaned := stripDeadFields(decoded)
		if cleaned == "" {
			log.Printf("  [%s] %s: failed to clean, skipping", table, id)
			skipped++
			continue
		}

		_, err := db.ExecContext(context.Background(),
			fmt.Sprintf("UPDATE %s SET %s = $1::jsonb WHERE %s = $2", table, themeCol, idCol),
			cleaned, id,
		)
		if err != nil {
			log.Printf("  [%s] %s: update failed: %v", table, id, err)
			skipped++
			continue
		}

		oldSize := len(raw)
		newSize := len(cleaned)
		log.Printf("  [%s] %s: %d -> %d bytes (%.0f%% reduction)", table, id, oldSize, newSize, float64(oldSize-newSize)/float64(oldSize)*100)
		fixed++
	}

	log.Printf("[%s] Done: %d fixed, %d skipped", table, fixed, skipped)
}

// decodeRecursive peels off base64 and JSON string layers until clean JSON object is reached.
func decodeRecursive(s string) string {
	s = strings.TrimSpace(s)

	// If it's already a JSON object, we're done
	if len(s) > 0 && s[0] == '{' {
		var obj map[string]any
		if json.Unmarshal([]byte(s), &obj) == nil {
			return s
		}
	}

	// Try unwrapping a JSON string (quoted value)
	if len(s) > 1 && s[0] == '"' {
		var unquoted string
		if json.Unmarshal([]byte(s), &unquoted) == nil {
			return decodeRecursive(unquoted)
		}
	}

	// Try base64 decoding
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		// Try URL-safe base64
		decoded, err = base64.URLEncoding.DecodeString(s)
		if err != nil {
			return s // can't decode further
		}
	}
	return decodeRecursive(string(decoded))
}

// stripDeadFields removes dark_color_scheme, spacing, border_radius from the theme JSON.
func stripDeadFields(s string) string {
	var theme map[string]any
	if err := json.Unmarshal([]byte(s), &theme); err != nil {
		return ""
	}
	delete(theme, "dark_color_scheme")
	delete(theme, "spacing")
	delete(theme, "border_radius")
	out, err := json.Marshal(theme)
	if err != nil {
		return ""
	}
	return string(out)
}
```

- [ ] **Step 2: Build to verify**

Run: `go build ./cmd/fix_theme_encoding/`
Expected: Compiles cleanly.

- [ ] **Step 3: Commit**

```bash
git add cmd/fix_theme_encoding/main.go
git commit -m "feat: add one-time migration script to fix recursive base64 encoding in theme data"
```
