# Template Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract ~5,400 lines of React/TSX template constants from Go string literals into real `.tsx` files under `internal/templates/nextjs/`, loaded via `go:embed`, eliminating switch-statement boilerplate while preserving identical runtime behavior.

**Architecture:** Create an `internal/templates/` package with a `nextjs/` subdirectory that mirrors the generated project structure 1:1. A single `embed.go` file exposes `GetTemplate(path)` and `GetTemplateFS()` for reading templates. The existing path arrays (`NEXTJS_TEMPLATE_FILE_PATHS`, etc.) move into this package. All consumers in `app_codegen_controller.go`, `app_builder_controller.go`, `app_builder_generation.go`, and `app_builder_tools.go` switch from `constants.ECOMMERCE_TEMPLATE_*` to `templates.GetTemplate(path)`.

**Tech Stack:** Go `embed` package, existing Bun/chi stack (no new dependencies)

---

## File Structure

### New files to create

```
internal/templates/
├── embed.go                                    # go:embed directive + GetTemplate/GetTemplateFS/path arrays
├── nextjs/
│   ├── package.json
│   ├── tsconfig.json
│   ├── postcss.config.mjs
│   ├── next.config.mjs
│   ├── .gitignore.tmpl                         # renamed to avoid git treating it as real gitignore
│   ├── app/
│   │   ├── not-found.tsx                       # English base, has {{title}}/{{description}}/{{cta}} placeholders
│   │   ├── error.tsx                           # English base, has {{message}}/{{retry}} placeholders
│   │   ├── cart/page.tsx
│   │   ├── checkout/page.tsx
│   │   ├── checkout/success/page.tsx
│   │   ├── checkout/cancel/page.tsx
│   │   ├── products/page.tsx
│   │   ├── products/[slug]/page.tsx
│   │   ├── categories/[slug]/page.tsx
│   │   ├── policies/shipping/page.tsx
│   │   ├── policies/returns/page.tsx
│   │   ├── policies/privacy/page.tsx
│   │   └── policies/terms/page.tsx
│   ├── components/
│   │   ├── ecommerce/
│   │   │   ├── product-card.tsx
│   │   │   ├── cart-drawer.tsx
│   │   │   ├── price-display.tsx
│   │   │   ├── quantity-selector.tsx
│   │   │   ├── product-gallery.tsx
│   │   │   ├── checkout-form.tsx
│   │   │   ├── category-nav.tsx
│   │   │   ├── cart-item-row.tsx
│   │   │   ├── order-summary.tsx
│   │   │   ├── empty-state.tsx
│   │   │   ├── review-list.tsx
│   │   │   ├── wishlist-button.tsx
│   │   │   ├── site-header.tsx
│   │   │   ├── site-footer.tsx
│   │   │   ├── providers.tsx
│   │   │   └── newsletter-section.tsx
│   │   └── ui/
│   │       ├── button.tsx
│   │       ├── card.tsx
│   │       ├── input.tsx
│   │       ├── badge.tsx
│   │       └── skeleton.tsx
│   └── lib/
│       ├── utils.ts
│       ├── db.ts
│       └── wemob-store.ts
```

### Files to modify

- `internal/templates/embed.go` — new file, the embed package
- `internal/constants/ai_templates.go` — remove all `NEXTJS_TEMPLATE_*` string constants and `NEXTJS_*_FILE_PATHS` arrays (keep `GetNotFoundTemplate`, `GetErrorTemplate`, `DetectLanguage`, `notFoundStrings`, `errorStrings`, `AUTH_DEFAULT_DATA_MODELS`, and `buildGlobalsCSSTheme`-related code which stays in codegen controller)
- `internal/constants/ai_ecommerce_templates.go` — delete entirely (all constants move to files)
- `internal/controllers/app_codegen_controller.go` — replace `constants.ECOMMERCE_TEMPLATE_*` / `constants.NEXTJS_TEMPLATE_*` references with `templates.GetTemplate()` calls; simplify `getTemplateContentWithLang`, `getEcommerceTemplateContent`, `getUITemplateContent`, `isTemplateFile`
- `internal/controllers/app_builder_controller.go` — replace `constants.NEXTJS_TEMPLATE_FILE_PATHS` / `constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS` with `templates.*`
- `internal/controllers/app_builder_generation.go` — replace `constants.NEXTJS_TEMPLATE_DB_TS` with `templates.GetTemplate("lib/db.ts")`
- `internal/controllers/app_builder_tools.go` — replace `constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS` / `constants.NEXTJS_TEMPLATE_DB_TS` with `templates.*`

### What stays in `internal/constants/`

These are NOT templates (they're either prompts, dynamic builders, i18n logic, or data):

- `ai_templates.go` — keeps: `GetNotFoundTemplate()`, `GetErrorTemplate()`, `DetectLanguage()`, `notFoundStrings`, `errorStrings`, `AUTH_DEFAULT_DATA_MODELS`. Loses: all `NEXTJS_TEMPLATE_*` constants and `NEXTJS_*_FILE_PATHS` arrays.
- `ai_ecommerce_i18n.go` — stays entirely (i18n string maps + `LocalizeEcommerceTemplate`)
- `ai_ecommerce.go` — stays entirely (ecommerce prompts + data models)
- `ai_builder.go` — stays entirely (LLM prompts)
- `ai_structure.go` — stays entirely (LLM prompts)
- `ai_validation.go` — stays entirely
- `ai_conversation.go` — stays entirely
- `ai_dashboard.go` — stays entirely
- `ai_mobile_injection.go` — stays entirely

### What stays in `internal/controllers/app_codegen_controller.go`

Dynamic template builders that cannot be static files:

- `buildGlobalsCSSTheme()` — generates `app/globals.css` from color scheme
- `buildDrizzleSchema()` — generates `lib/schema.ts` from data models
- `buildEcommerceProviders()` — conditionally wraps with AuthProvider
- `getTemplateContentWithLang()` — simplified to use `templates.GetTemplate()` for static files, keeps dynamic cases inline

---

## Tasks

### Task 1: Create `internal/templates/embed.go` package

**Files:**
- Create: `internal/templates/embed.go`

- [ ] **Step 1: Create the embed package**

```go
package templates

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed nextjs
var nextjsFS embed.FS

// GetTemplate reads a template file by its generated-project-relative path.
// For example: "package.json", "components/ecommerce/product-card.tsx", "lib/utils.ts".
// The ".gitignore" path is mapped to ".gitignore.tmpl" on disk to avoid git interference.
// Returns the file content as a string, or empty string if not found.
func GetTemplate(path string) string {
	fsPath := "nextjs/" + path
	if path == ".gitignore" {
		fsPath = "nextjs/.gitignore.tmpl"
	}
	data, err := nextjsFS.ReadFile(fsPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// FS returns the embedded filesystem rooted at nextjs/.
func FS() fs.FS {
	sub, _ := fs.Sub(nextjsFS, "nextjs")
	return sub
}

// HasTemplate reports whether a template file exists at the given path.
func HasTemplate(path string) bool {
	fsPath := "nextjs/" + path
	if path == ".gitignore" {
		fsPath = "nextjs/.gitignore.tmpl"
	}
	_, err := nextjsFS.ReadFile(fsPath)
	return err == nil
}

// Template file paths that are managed by hardcoded templates, not LLM-generated.
var BaseFilePaths = []string{
	"package.json",
	"tsconfig.json",
	"postcss.config.mjs",
	"next.config.mjs",
	"app/globals.css",
	"app/not-found.tsx",
	"app/error.tsx",
	".gitignore",
}

// DBFilePaths are template-managed files injected only after successful Neon provisioning.
var DBFilePaths = []string{
	"lib/db.ts",
	"lib/schema.ts",
}

// EcommerceFilePaths are template-managed files injected only for e-commerce projects.
var EcommerceFilePaths = []string{
	"lib/wemob-store.ts",
	"components/ecommerce/product-card.tsx",
	"components/ecommerce/cart-drawer.tsx",
	"components/ecommerce/price-display.tsx",
	"components/ecommerce/quantity-selector.tsx",
	"components/ecommerce/product-gallery.tsx",
	"components/ecommerce/checkout-form.tsx",
	"components/ecommerce/category-nav.tsx",
	"components/ecommerce/cart-item-row.tsx",
	"components/ecommerce/order-summary.tsx",
	"components/ecommerce/empty-state.tsx",
	"components/ecommerce/review-list.tsx",
	"components/ecommerce/wishlist-button.tsx",
	"components/ecommerce/site-header.tsx",
	"components/ecommerce/site-footer.tsx",
	"components/ecommerce/providers.tsx",
	"components/ecommerce/newsletter-section.tsx",
	"app/cart/page.tsx",
	"app/checkout/page.tsx",
	"app/checkout/success/page.tsx",
	"app/checkout/cancel/page.tsx",
	"app/products/page.tsx",
	"app/products/[slug]/page.tsx",
	"app/categories/[slug]/page.tsx",
	"app/policies/shipping/page.tsx",
	"app/policies/returns/page.tsx",
	"app/policies/privacy/page.tsx",
	"app/policies/terms/page.tsx",
}

// UIFilePaths are pre-built UI component templates injected during initial generation.
var UIFilePaths = []string{
	"lib/utils.ts",
	"components/ui/button.tsx",
	"components/ui/card.tsx",
	"components/ui/input.tsx",
	"components/ui/badge.tsx",
	"components/ui/skeleton.tsx",
}

// IsTemplateFile checks if a file path is managed by any template category.
func IsTemplateFile(path string) bool {
	for _, paths := range [][]string{BaseFilePaths, DBFilePaths, EcommerceFilePaths, UIFilePaths} {
		for _, tp := range paths {
			if path == tp {
				return true
			}
		}
	}
	return false
}

// AllTemplatePaths returns a deduplicated slice of all template file paths.
func AllTemplatePaths() []string {
	seen := make(map[string]bool)
	var all []string
	for _, paths := range [][]string{BaseFilePaths, DBFilePaths, EcommerceFilePaths, UIFilePaths} {
		for _, p := range paths {
			if !seen[p] {
				seen[p] = true
				all = append(all, p)
			}
		}
	}
	return all
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./internal/templates/`
Expected: Compilation error about missing `nextjs` directory (embed directive can't find files). This is expected — files are created in Task 2.

- [ ] **Step 3: Commit**

```bash
git add internal/templates/embed.go
git commit -m "feat(templates): add embed package with GetTemplate and path arrays"
```

---

### Task 2: Extract base templates to files

**Files:**
- Create: `internal/templates/nextjs/package.json`
- Create: `internal/templates/nextjs/tsconfig.json`
- Create: `internal/templates/nextjs/postcss.config.mjs`
- Create: `internal/templates/nextjs/next.config.mjs`
- Create: `internal/templates/nextjs/.gitignore.tmpl`
- Create: `internal/templates/nextjs/lib/db.ts`
- Create: `internal/templates/nextjs/lib/utils.ts`
- Create: `internal/templates/nextjs/lib/wemob-store.ts`
- Create: `internal/templates/nextjs/app/not-found.tsx`
- Create: `internal/templates/nextjs/app/error.tsx`

Strategy: For each constant in `ai_templates.go`, extract the raw string content (everything between the backticks) into the corresponding file. These are copy-paste operations — no transformation needed.

**Special cases:**
- `.gitignore` → save as `.gitignore.tmpl` (to avoid git treating it as a real gitignore for the backend repo)
- `not-found.tsx` and `error.tsx` — these are currently generated dynamically by `GetNotFoundTemplate(lang)` / `GetErrorTemplate(lang)` with Go string concatenation. These stay as Go functions in `constants/ai_templates.go` for now — do NOT extract them. They use `s[0]`, `s[1]`, `s[2]` concatenation which can't be a static file.

Revised list — only extract **static** templates:

- [ ] **Step 1: Create directory structure**

```bash
mkdir -p internal/templates/nextjs/{app,lib,components/ui,components/ecommerce}
mkdir -p internal/templates/nextjs/app/{cart,checkout/success,checkout/cancel,products/\\[slug\\],categories/\\[slug\\],policies/{shipping,returns,privacy,terms}}
```

- [ ] **Step 2: Extract base config templates**

Extract from `internal/constants/ai_templates.go` — copy the exact string content (between backticks) of each constant into its file:

| Go constant | Target file |
|---|---|
| `NEXTJS_TEMPLATE_PACKAGE_JSON` (line 69) | `internal/templates/nextjs/package.json` |
| `NEXTJS_TEMPLATE_TSCONFIG` (line 112) | `internal/templates/nextjs/tsconfig.json` |
| `NEXTJS_TEMPLATE_POSTCSS_CONFIG` (line 139) | `internal/templates/nextjs/postcss.config.mjs` |
| `NEXTJS_TEMPLATE_NEXT_CONFIG` (line 149) | `internal/templates/nextjs/next.config.mjs` |
| `NEXTJS_TEMPLATE_GITIGNORE` (line 166) | `internal/templates/nextjs/.gitignore.tmpl` |
| `NEXTJS_TEMPLATE_DB_TS` (line 368) | `internal/templates/nextjs/lib/db.ts` |
| `NEXTJS_TEMPLATE_WEMOB_STORE` (line 394) | `internal/templates/nextjs/lib/wemob-store.ts` |
| `NEXTJS_TEMPLATE_UTILS_TS` (line 1444) | `internal/templates/nextjs/lib/utils.ts` |

For each: read the Go constant content, write it to the target file exactly as-is (no leading/trailing newlines unless the original has them).

- [ ] **Step 3: Extract UI component templates**

| Go constant | Target file |
|---|---|
| `NEXTJS_TEMPLATE_BUTTON_TSX` (line 1452) | `internal/templates/nextjs/components/ui/button.tsx` |
| `NEXTJS_TEMPLATE_CARD_TSX` (line 1506) | `internal/templates/nextjs/components/ui/card.tsx` |
| `NEXTJS_TEMPLATE_INPUT_TSX` (line 1554) | `internal/templates/nextjs/components/ui/input.tsx` |
| `NEXTJS_TEMPLATE_BADGE_TSX` (line 1577) | `internal/templates/nextjs/components/ui/badge.tsx` |
| `NEXTJS_TEMPLATE_SKELETON_TSX` (line 1609) | `internal/templates/nextjs/components/ui/skeleton.tsx` |

- [ ] **Step 4: Verify embed compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./internal/templates/`
Expected: Compiles successfully (all embedded files now exist). May warn about missing ecommerce files — that's fine, they're added in Task 3.

- [ ] **Step 5: Commit**

```bash
git add internal/templates/nextjs/
git commit -m "feat(templates): extract base and UI templates to .tsx files"
```

---

### Task 3: Extract ecommerce templates to files

**Files:**
- Create: 16 ecommerce component files + 11 ecommerce page files
- Source: `internal/constants/ai_ecommerce_templates.go`

- [ ] **Step 1: Extract ecommerce component templates**

Extract from `internal/constants/ai_ecommerce_templates.go` — copy each constant's raw string content into its file:

| Go constant | Target file |
|---|---|
| `ECOMMERCE_TEMPLATE_PRODUCT_CARD` (line 6) | `internal/templates/nextjs/components/ecommerce/product-card.tsx` |
| `ECOMMERCE_TEMPLATE_CART_DRAWER` (line 142) | `internal/templates/nextjs/components/ecommerce/cart-drawer.tsx` |
| `ECOMMERCE_TEMPLATE_PRICE_DISPLAY` (line 298) | `internal/templates/nextjs/components/ecommerce/price-display.tsx` |
| `ECOMMERCE_TEMPLATE_QUANTITY_SELECTOR` (line 346) | `internal/templates/nextjs/components/ecommerce/quantity-selector.tsx` |
| `ECOMMERCE_TEMPLATE_PRODUCT_GALLERY` (line 418) | `internal/templates/nextjs/components/ecommerce/product-gallery.tsx` |
| `ECOMMERCE_TEMPLATE_CHECKOUT_FORM` (line 522) | `internal/templates/nextjs/components/ecommerce/checkout-form.tsx` |
| `ECOMMERCE_TEMPLATE_CATEGORY_NAV` (line 757) | `internal/templates/nextjs/components/ecommerce/category-nav.tsx` |
| `ECOMMERCE_TEMPLATE_CART_ITEM_ROW` (line 815) | `internal/templates/nextjs/components/ecommerce/cart-item-row.tsx` |
| `ECOMMERCE_TEMPLATE_ORDER_SUMMARY` (line 885) | `internal/templates/nextjs/components/ecommerce/order-summary.tsx` |
| `ECOMMERCE_TEMPLATE_EMPTY_STATE` (line 932) | `internal/templates/nextjs/components/ecommerce/empty-state.tsx` |
| `ECOMMERCE_TEMPLATE_REVIEW_LIST` (line 969) | `internal/templates/nextjs/components/ecommerce/review-list.tsx` |
| `ECOMMERCE_TEMPLATE_WISHLIST_BUTTON` (line 1169) | `internal/templates/nextjs/components/ecommerce/wishlist-button.tsx` |
| `ECOMMERCE_TEMPLATE_SITE_HEADER` (line 1222) | `internal/templates/nextjs/components/ecommerce/site-header.tsx` |
| `ECOMMERCE_TEMPLATE_SITE_FOOTER` (line 1593) | `internal/templates/nextjs/components/ecommerce/site-footer.tsx` |
| `ECOMMERCE_TEMPLATE_PROVIDERS` (line 1821) | `internal/templates/nextjs/components/ecommerce/providers.tsx` |
| `ECOMMERCE_TEMPLATE_NEWSLETTER` (line 1831) | `internal/templates/nextjs/components/ecommerce/newsletter-section.tsx` |

- [ ] **Step 2: Extract ecommerce page templates**

| Go constant | Target file |
|---|---|
| `ECOMMERCE_TEMPLATE_CART_PAGE` (line 1938) | `internal/templates/nextjs/app/cart/page.tsx` |
| `ECOMMERCE_TEMPLATE_CHECKOUT_PAGE` (line 2147) | `internal/templates/nextjs/app/checkout/page.tsx` |
| `ECOMMERCE_TEMPLATE_CHECKOUT_SUCCESS_PAGE` (line 2226) | `internal/templates/nextjs/app/checkout/success/page.tsx` |
| `ECOMMERCE_TEMPLATE_CHECKOUT_CANCEL_PAGE` (line 2404) | `internal/templates/nextjs/app/checkout/cancel/page.tsx` |
| `ECOMMERCE_TEMPLATE_PRODUCTS_PAGE` (line 2508) | `internal/templates/nextjs/app/products/page.tsx` |
| `ECOMMERCE_TEMPLATE_PRODUCT_DETAIL_PAGE` (line 3026) | `internal/templates/nextjs/app/products/[slug]/page.tsx` |
| `ECOMMERCE_TEMPLATE_CATEGORY_PAGE` (line 3305) | `internal/templates/nextjs/app/categories/[slug]/page.tsx` |
| `ECOMMERCE_TEMPLATE_SHIPPING_POLICY` (line 3484) | `internal/templates/nextjs/app/policies/shipping/page.tsx` |
| `ECOMMERCE_TEMPLATE_RETURNS_POLICY` (line 3581) | `internal/templates/nextjs/app/policies/returns/page.tsx` |
| `ECOMMERCE_TEMPLATE_PRIVACY_POLICY` (line 3659) | `internal/templates/nextjs/app/policies/privacy/page.tsx` |
| `ECOMMERCE_TEMPLATE_TERMS_POLICY` (line 3737) | `internal/templates/nextjs/app/policies/terms/page.tsx` |

- [ ] **Step 3: Verify embed compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./internal/templates/`
Expected: Compiles successfully.

- [ ] **Step 4: Commit**

```bash
git add internal/templates/nextjs/
git commit -m "feat(templates): extract ecommerce templates to .tsx files"
```

---

### Task 4: Update `app_codegen_controller.go` to use templates package

**Files:**
- Modify: `internal/controllers/app_codegen_controller.go`

- [ ] **Step 1: Add import**

Add `"github.com/knid/wemob/internal/templates"` to the imports in `app_codegen_controller.go`.

- [ ] **Step 2: Replace `isTemplateFile` function**

Replace the existing `isTemplateFile` function (around line 526-544) with:

```go
// isTemplateFile checks if a file path is managed by hardcoded templates
func isTemplateFile(path string) bool {
	return templates.IsTemplateFile(path)
}
```

- [ ] **Step 3: Simplify `getTemplateContentWithLang`**

Replace the function (around line 865-889) with:

```go
func getTemplateContentWithLang(path string, colorScheme, darkColorScheme map[string]any, projectType string, dataModels []DataModel, lang string) string {
	// Dynamic templates that can't be static files
	switch path {
	case "app/globals.css":
		return buildGlobalsCSSTheme(colorScheme, darkColorScheme, projectType == "mobile_app")
	case "app/not-found.tsx":
		return constants.GetNotFoundTemplate(lang)
	case "app/error.tsx":
		return constants.GetErrorTemplate(lang)
	case "lib/schema.ts":
		return buildDrizzleSchema(dataModels)
	}
	// All other templates are static embedded files
	return templates.GetTemplate(path)
}
```

- [ ] **Step 4: Simplify `getEcommerceTemplateContent`**

Replace the function (around line 916-977) with:

```go
func getEcommerceTemplateContent(path string, lang string) string {
	// No i18n needed for these
	if path == "lib/wemob-store.ts" || path == "components/ecommerce/empty-state.tsx" || path == "components/ecommerce/providers.tsx" {
		return templates.GetTemplate(path)
	}
	raw := templates.GetTemplate(path)
	if raw == "" {
		slog.Warn("getEcommerceTemplateContent: no content for template path", "path", path)
		return ""
	}
	return constants.LocalizeEcommerceTemplate(raw, lang)
}
```

- [ ] **Step 5: Simplify `getUITemplateContent`**

Replace the function (around line 983-999) with:

```go
func getUITemplateContent(path string) string {
	return templates.GetTemplate(path)
}
```

- [ ] **Step 6: Verify build**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: Compiles successfully.

- [ ] **Step 7: Commit**

```bash
git add internal/controllers/app_codegen_controller.go
git commit -m "refactor(codegen): use templates.GetTemplate instead of constant switch"
```

---

### Task 5: Update `app_builder_controller.go` to use templates package

**Files:**
- Modify: `internal/controllers/app_builder_controller.go`

- [ ] **Step 1: Add import and replace path array references**

Add `"github.com/knid/wemob/internal/templates"` to imports.

Replace all occurrences of:
- `constants.NEXTJS_TEMPLATE_FILE_PATHS` → `templates.BaseFilePaths` (line ~331)
- `constants.NEXTJS_UI_TEMPLATE_FILE_PATHS` → `templates.UIFilePaths` (line ~344)
- `constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS` → `templates.EcommerceFilePaths` (line ~357)

Search for these patterns in the file and replace each one. Note: `buildEcommerceProviders` stays — it's a dynamic builder, not a static template.

- [ ] **Step 2: Verify build**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_controller.go
git commit -m "refactor(builder): use templates package path arrays"
```

---

### Task 6: Update `app_builder_generation.go` to use templates package

**Files:**
- Modify: `internal/controllers/app_builder_generation.go`

- [ ] **Step 1: Add import and replace references**

Add `"github.com/knid/wemob/internal/templates"` to imports.

Replace:
- `constants.NEXTJS_TEMPLATE_DB_TS` → `templates.GetTemplate("lib/db.ts")`

There are 2 occurrences (around lines 121 and 196 based on earlier grep).

- [ ] **Step 2: Verify build**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_generation.go
git commit -m "refactor(generation): use templates.GetTemplate for db.ts"
```

---

### Task 7: Update `app_builder_tools.go` to use templates package

**Files:**
- Modify: `internal/controllers/app_builder_tools.go`

- [ ] **Step 1: Add import and replace references**

Add `"github.com/knid/wemob/internal/templates"` to imports.

Replace:
- `constants.NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS` → `templates.EcommerceFilePaths`
- `constants.NEXTJS_TEMPLATE_DB_TS` → `templates.GetTemplate("lib/db.ts")`

- [ ] **Step 2: Verify build**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add internal/controllers/app_builder_tools.go
git commit -m "refactor(tools): use templates package for ecommerce paths and db.ts"
```

---

### Task 8: Clean up old constants

**Files:**
- Modify: `internal/constants/ai_templates.go`
- Delete: `internal/constants/ai_ecommerce_templates.go`

- [ ] **Step 1: Delete `ai_ecommerce_templates.go`**

```bash
rm internal/constants/ai_ecommerce_templates.go
```

This file is now fully replaced by `internal/templates/nextjs/components/ecommerce/*.tsx` and `internal/templates/nextjs/app/**/*.tsx`.

- [ ] **Step 2: Clean up `ai_templates.go`**

Remove the following from `ai_templates.go`:
- `NEXTJS_TEMPLATE_FILE_PATHS` variable (line 9-18)
- `NEXTJS_DB_TEMPLATE_FILE_PATHS` variable (line 21-24)
- `NEXTJS_ECOMMERCE_TEMPLATE_FILE_PATHS` variable (line 27-56)
- `NEXTJS_UI_TEMPLATE_FILE_PATHS` variable (line 60-67)
- `NEXTJS_TEMPLATE_PACKAGE_JSON` constant (line 69-110)
- `NEXTJS_TEMPLATE_TSCONFIG` constant (line 112-137)
- `NEXTJS_TEMPLATE_POSTCSS_CONFIG` constant (line 139-147)
- `NEXTJS_TEMPLATE_NEXT_CONFIG` constant (line 149-164)
- `NEXTJS_TEMPLATE_GITIGNORE` constant (line 166-200)
- `NEXTJS_TEMPLATE_DB_TS` constant (line 368-390)
- `NEXTJS_TEMPLATE_WEMOB_STORE` constant (line 394-~1440)
- `NEXTJS_TEMPLATE_UTILS_TS` constant (line 1444-1450)
- `NEXTJS_TEMPLATE_BUTTON_TSX` constant (line 1452-1504)
- `NEXTJS_TEMPLATE_CARD_TSX` constant (line 1506-1552)
- `NEXTJS_TEMPLATE_INPUT_TSX` constant (line 1554-1575)
- `NEXTJS_TEMPLATE_BADGE_TSX` constant (line 1577-1607)
- `NEXTJS_TEMPLATE_SKELETON_TSX` constant (line 1609-1616)

Keep:
- `import "strings"` (used by `DetectLanguage`)
- `notFoundStrings` map (line 203-212)
- `errorStrings` map (line 215-224)
- `GetNotFoundTemplate()` function (line 227-287)
- `GetErrorTemplate()` function (line 290-328)
- `DetectLanguage()` function (line 332-363)
- `AUTH_DEFAULT_DATA_MODELS` constant (line 1623-1639)

- [ ] **Step 3: Remove unused `constants` import if needed**

Check all modified controller files — if any file no longer references any `constants.*` symbol, remove the import. (Unlikely since they still use `constants.GetNotFoundTemplate`, `constants.LocalizeEcommerceTemplate`, etc.)

- [ ] **Step 4: Full build verification**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./cmd/main.go`
Expected: Compiles successfully with no errors.

- [ ] **Step 5: Verify binary runs**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build -o /tmp/wemob-test ./cmd/main.go && echo "Build OK"`
Expected: "Build OK" — confirms the embedded templates are bundled correctly.

- [ ] **Step 6: Commit**

```bash
git add -A internal/constants/ai_templates.go internal/constants/ai_ecommerce_templates.go
git commit -m "refactor(constants): remove template string constants replaced by embedded files"
```

---

### Task 9: Smoke test — verify template content matches

**Files:**
- None (verification only)

- [ ] **Step 1: Write a quick verification script**

Create a temporary Go test file to verify that the embedded templates return the same content that the old constants had. Since we can't compare against the deleted constants, we verify key properties:

```bash
cd /home/knid/Projects/wemob/wemob-backend
go run -v -exec "echo" ./cmd/main.go 2>&1 | head -5
```

This just verifies the binary starts without panicking on missing templates.

- [ ] **Step 2: Spot-check a few templates via Go**

Run a quick Go snippet to verify templates load correctly:

```bash
cd /home/knid/Projects/wemob/wemob-backend
cat > /tmp/verify_templates.go << 'GOEOF'
//go:build ignore

package main

import (
	"fmt"
	"github.com/knid/wemob/internal/templates"
)

func main() {
	checks := []struct {
		path     string
		contains string
	}{
		{"package.json", `"next": "^16.0.0"`},
		{"components/ui/button.tsx", "buttonVariants"},
		{"components/ecommerce/product-card.tsx", "ProductCard"},
		{"lib/db.ts", "drizzle"},
		{"lib/wemob-store.ts", "CartProvider"},
		{".gitignore", "node_modules"},
		{"lib/utils.ts", "twMerge"},
		{"app/cart/page.tsx", "useCart"},
	}
	allOK := true
	for _, c := range checks {
		content := templates.GetTemplate(c.path)
		if content == "" {
			fmt.Printf("FAIL: %s — empty\n", c.path)
			allOK = false
		} else if !containsStr(content, c.contains) {
			fmt.Printf("FAIL: %s — missing %q\n", c.path, c.contains)
			allOK = false
		} else {
			fmt.Printf("OK:   %s\n", c.path)
		}
	}
	if !allOK {
		fmt.Println("\nSOME CHECKS FAILED")
	} else {
		fmt.Println("\nALL CHECKS PASSED")
	}
}

func containsStr(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
GOEOF
go run /tmp/verify_templates.go
```

Expected output:
```
OK:   package.json
OK:   components/ui/button.tsx
OK:   components/ecommerce/product-card.tsx
OK:   lib/db.ts
OK:   lib/wemob-store.ts
OK:   .gitignore
OK:   lib/utils.ts
OK:   app/cart/page.tsx

ALL CHECKS PASSED
```

- [ ] **Step 3: Verify IsTemplateFile works**

```bash
cat > /tmp/verify_istemplate.go << 'GOEOF'
//go:build ignore

package main

import (
	"fmt"
	"github.com/knid/wemob/internal/templates"
)

func main() {
	yes := []string{"package.json", "components/ecommerce/product-card.tsx", "lib/utils.ts"}
	no := []string{"app/globals.css.bak", "random.tsx", ""}
	for _, p := range yes {
		if !templates.IsTemplateFile(p) {
			fmt.Printf("FAIL: %s should be template\n", p)
		} else {
			fmt.Printf("OK:   IsTemplateFile(%q) = true\n", p)
		}
	}
	for _, p := range no {
		if templates.IsTemplateFile(p) {
			fmt.Printf("FAIL: %s should NOT be template\n", p)
		} else {
			fmt.Printf("OK:   IsTemplateFile(%q) = false\n", p)
		}
	}
}
GOEOF
go run /tmp/verify_istemplate.go
```

Expected: All OK.

- [ ] **Step 4: Clean up temp files**

```bash
rm -f /tmp/verify_templates.go /tmp/verify_istemplate.go /tmp/wemob-test
```

- [ ] **Step 5: Final commit (no changes expected, just verify clean state)**

```bash
cd /home/knid/Projects/wemob/wemob-backend && git status
```

Expected: Clean working tree (all changes committed in prior tasks).
