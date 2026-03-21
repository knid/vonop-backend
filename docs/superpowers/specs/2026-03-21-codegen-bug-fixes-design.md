# Codegen Bug Fixes — Design Spec

**Date:** 2026-03-21
**Context:** Generated app "Görsel Üretici" (5e02987c) had multiple bugs traced to backend codegen issues. These fixes prevent the same bugs in all future generated apps.

---

## Fix 1: Missing `import './globals.css'` in layout.tsx

**Problem:** The LLM generates `app/layout.tsx` but sometimes omits `import './globals.css'`. Since globals.css is template-injected (not LLM-generated), the LLM doesn't always know to import it. Without this import, all Tailwind utilities and theme tokens fail — the entire app appears unstyled.

**Root cause:** The prompt says "Do NOT write app/globals.css" but never explicitly says "import it in layout.tsx".

**Changes:**

### A. Prompt rule (ai_builder.go)

Add to `NEXTJS_BUILDER_SINGLESHOT_INSTRUCTIONS` (the file-block output rules):
> `app/layout.tsx MUST include import './globals.css' as the FIRST import. This file is pre-injected with the project's color scheme and Tailwind v4 config — you must import it or all styles will break.`

Add same rule to `NEXTJS_BUILDER_CHAT_INSTRUCTIONS` for the chat path.

Add to the `builderFrameworkRulesBase` (shared rules block, near the layout.tsx CRITICAL rule):
> `CRITICAL: app/layout.tsx MUST import './globals.css' (pre-injected with Tailwind v4 theme and semantic tokens). Without this import, ALL styles break.`

### B. Deterministic check (app_codegen_controller.go)

In `runDeterministicChecks`, add a new check:
- If `app/layout.tsx` exists in files map
- AND its content does NOT contain `import './globals.css'` or `import "../globals.css"`
- → Add validation error: type `missing_globals_css_import`, auto-fixable

---

## Fix 2: Duplicate auth file detection

**Problem:** The LLM sometimes generates both `lib/wemob-auth.ts` (correct, per prompt spec) AND `lib/wemob-auth.tsx` (incorrect duplicate). TypeScript resolves `.ts` first, making `.tsx` dead code with a conflicting implementation.

**Changes:**

### Deterministic check (app_codegen_controller.go)

In `runDeterministicChecks`, add:
- If BOTH `lib/wemob-auth.ts` AND `lib/wemob-auth.tsx` exist in the files map
- → Add validation error for `lib/wemob-auth.tsx`: type `duplicate_auth_file`
- Message: "Duplicate auth file. Only lib/wemob-auth.ts should exist (server-only). Remove lib/wemob-auth.tsx."
- Auto-fix: delete `lib/wemob-auth.tsx` from the file map before passing to fix pass

---

## Fix 3: Permanent logo URL

**Problem:** `getProjectLogoURL()` returns an S3 pre-signed URL with 7-day expiry. The LLM hardcodes this URL into generated component files (Navbar, LoginClient, layout.tsx metadata). After 7 days, all logos and favicons break.

**Changes:**

### A. New public endpoint

**Route:** `GET /v2/apps/{appId}/logo`
**Handler:** `GetAppLogo` in `internal/views/app_views.go`
**Logic:**
1. Load project by ID (no auth required — logos are public assets)
2. If `logo_s3_key` is empty → return 404
3. Generate fresh pre-signed URL (1 hour expiry is fine for redirects)
4. Return 302 redirect to the pre-signed URL
5. Set `Cache-Control: public, max-age=86400` so browsers/CDNs cache for 24h

**Route registration:** In `cmd/main.go`, add to public V2 routes:
```go
r.Get("/v2/apps/{appId}/logo", views.GetAppLogo)
```

### B. Update getProjectLogoURL()

In `internal/controllers/app_builder_controller.go`:
- Change `getProjectLogoURL()` to return the permanent endpoint URL:
  `https://api.wemob.io/v2/apps/{project.ID}/logo`
- Use `WEMOB_API_BASE_URL` constant instead of hardcoding the domain
- Remove the pre-signed URL generation from this function

### C. Update generateAppLogo()

- After uploading to S3, return the permanent URL instead of pre-signed URL
- The returned URL format: `{WEMOB_API_BASE_URL}/v2/apps/{project.ID}/logo`

---

## Fix 4: Localize error.tsx and not-found.tsx templates

**Problem:** `NEXTJS_TEMPLATE_NOT_FOUND` and `NEXTJS_TEMPLATE_ERROR` are Go string constants with hardcoded English text ("This page could not be found.", "Something went wrong."). Generated apps are often in Turkish or other languages, creating an inconsistent UX.

**Changes:**

### A. Template functions (ai_templates.go)

Convert constants to functions:
```go
func GetNotFoundTemplate(lang string) string { ... }
func GetErrorTemplate(lang string) string { ... }
```

Translation map for the few strings needed:
- not-found: "This page could not be found." / "Back to home"
- error: "Something went wrong." / "Try again"

Supported languages: `en` (default), `tr`. Fallback to English for unknown languages. Can add more languages later.

### B. Language detection

Add a helper `detectLanguage(prompt string) string` that:
- Checks for Turkish characters (ğ, ü, ş, ı, ö, ç) → `"tr"`
- Could use a simple heuristic or the project's structure metadata
- Default: `"en"`

### C. Update injection sites

In `app_builder_controller.go`, wherever templates are injected, use the function:
```go
lang := detectLanguage(project.Prompt)
c.injectTemplateFile(repo, "app/not-found.tsx", constants.GetNotFoundTemplate(lang), fileMap, sendEvent)
c.injectTemplateFile(repo, "app/error.tsx", constants.GetErrorTemplate(lang), fileMap, sendEvent)
```

Currently the template content is resolved via `getTemplateContent(tp)` which reads from the constants. Update this lookup to pass the language.

---

## Files Changed

| File | Changes |
|------|---------|
| `internal/constants/ai_builder.go` | Add globals.css import rules to prompts |
| `internal/constants/ai_templates.go` | Convert NOT_FOUND/ERROR templates to functions with i18n |
| `internal/controllers/app_codegen_controller.go` | Add 2 deterministic checks (globals.css import, duplicate auth) |
| `internal/controllers/app_builder_controller.go` | Update `getProjectLogoURL()`, `generateAppLogo()`, template injection to pass language |
| `internal/views/app_views.go` | Add `GetAppLogo` handler |
| `cmd/main.go` | Register `/v2/apps/{appId}/logo` route |

## Not in scope

- Fixing the specific downloaded app (user confirmed)
- PostHog `$ai_total_cost_usd` null (PostHog-side pricing table issue)
- Context selection quality (fallback already handles it)
- Not-found page wemob-create-page postMessage (intentional, only shows in iframe)
- Footer year (LLM output randomness, not systematically fixable)
- Missing FAL_KEY env var (app-specific — user needs to add it via Settings → Environment)
