# Remove Auth Template System — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove auth template injection and let the LLM generate auth files, fixing validation bugs and ecommerce+auth gaps.

**Architecture:** Delete auth template constants and injection code. Expand `NEXTJS_BUILDER_AUTH_RULES` with comprehensive implementation instructions so the LLM produces correct auth code. Make ecommerce Providers dynamic for auth support. Fix validation checks 2d/2e.

**Tech Stack:** Go (backend constants + controllers)

---

## Chunk 1: Remove Auth Templates & Expand Rules

### Task 1: Delete auth template constants from `ai_templates.go`

**Files:**
- Modify: `internal/constants/ai_templates.go:1549-2009`

- [ ] **Step 1: Delete the entire auth templates section**

Remove lines 1549–2009 (the `// Auth Templates` section) containing:
- `NEXTJS_AUTH_TEMPLATE_FILE_PATHS`
- `AUTH_DEFAULT_DATA_MODELS` — **KEEP THIS** (still used for DB provisioning)
- `NEXTJS_TEMPLATE_WEMOB_AUTH`
- `NEXTJS_TEMPLATE_WEMOB_AUTH_CLIENT`
- `NEXTJS_TEMPLATE_AUTH_ROUTE`

Keep `AUTH_DEFAULT_DATA_MODELS` (lines 1559-1575). Delete the rest.

- [ ] **Step 2: Verify build**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./...`
Expected: Compilation errors referencing deleted constants (will be fixed in later tasks)

---

### Task 2: Expand `NEXTJS_BUILDER_AUTH_RULES` in `ai_builder.go`

**Files:**
- Modify: `internal/constants/ai_builder.go:404-417`

- [ ] **Step 1: Replace `NEXTJS_BUILDER_AUTH_RULES` with comprehensive LLM instructions**

Replace the current short rules block with expanded instructions covering:
- File structure (lib/wemob-auth.ts, lib/wemob-auth-client.tsx, app/api/auth/[...action]/route.ts)
- Dependencies (jose for JWT, bcryptjs for passwords)
- Server-side API: register, login, logout, getSession, getCurrentUser, requireAuth, getApiSession
- Client-side API: AuthProvider context, useAuth hook
- Session management: JWT in httpOnly cookie, 7-day expiry
- Types: AuthUser (id, email, name, avatarUrl, role, emailVerified), Session, AuthResult
- Import boundary rules: server-only vs client-only
- Layout wrapping: AuthProvider in a client wrapper component or directly in layout

---

### Task 3: Update `structureCommonRules` in `ai_structure.go`

**Files:**
- Modify: `internal/constants/ai_structure.go:30`

- [ ] **Step 1: Update auth file planning instructions**

In the `structureCommonRules` constant, change rule 12 from "Auth infrastructure will be auto-injected as templates. Do NOT include auth route in file plan" to telling the LLM to plan all three auth files in its file list.

---

### Task 4: Remove `getAuthTemplateContent()` and auth paths from `isTemplateFile()` in `app_codegen_controller.go`

**Files:**
- Modify: `internal/controllers/app_codegen_controller.go:526-530, 948-959`

- [ ] **Step 1: Remove auth loop from `isTemplateFile()`**

Delete the loop at lines 526-530 that checks `NEXTJS_AUTH_TEMPLATE_FILE_PATHS`.

- [ ] **Step 2: Delete `getAuthTemplateContent()` function**

Delete lines 948-959 entirely.

---

### Task 5: Fix validation check 2d (false positive on layouts)

**Files:**
- Modify: `internal/controllers/app_codegen_controller.go:2142-2152`

- [ ] **Step 1: Skip layout files in check 2d**

Add a condition to skip files matching `app/layout.tsx` or `app/*/layout.tsx` pattern — server component layouts legitimately import client components like AuthProvider.

---

### Task 6: Fix validation check 2e (substring false negative)

**Files:**
- Modify: `internal/controllers/app_codegen_controller.go:2154-2164`

- [ ] **Step 1: Use exact import path matching**

Replace substring check with a regex or precise match that detects `@/lib/wemob-auth'` or `@/lib/wemob-auth"` (with quote terminator) to avoid matching `@/lib/wemob-auth-client`.

---

## Chunk 2: Remove Injection Code & Dynamic Providers

### Task 7: Remove auth template injection from `runPreGenerationSetup` in `app_builder_controller.go`

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:4881-4906`

- [ ] **Step 1: Remove template injection, keep AUTH_SECRET generation**

In the auth setup section of `runPreGenerationSetup` (lines 4881-4906):
- Keep the `AUTH_SECRET` env var generation (lines 4882-4892)
- Remove the auth template file injection loop (lines 4894-4899)
- Keep the `.env.local` update (lines 4902-4906)
- Change the guard from checking `fileMap["lib/wemob-auth.ts"]` to checking if AUTH_SECRET env var already exists.

---

### Task 8: Remove auth template injection from manage_database tool callback

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:4367-4386`

- [ ] **Step 1: Remove template injection loop, keep AUTH_SECRET**

In the manage_database tool callback:
- Keep AUTH_SECRET generation (lines 4369-4378)
- Remove the template injection loop (lines 4380-4385)

---

### Task 9: Update chat context strings

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:3126-3133`

- [ ] **Step 1: Remove "template-managed" references from chat context**

Update the auth context section to remove references to template-managed files. Keep the import paths and API surface documentation.

---

### Task 10: Make ecommerce Providers dynamic for auth

**Files:**
- Modify: `internal/controllers/app_codegen_controller.go:897-898`
- Modify: `internal/constants/ai_ecommerce_templates.go:1821-1828`

- [ ] **Step 1: Create `buildEcommerceProviders(needsAuth bool)` function**

Add a function in `app_codegen_controller.go` that returns the Providers component content, conditionally including `AuthProvider` wrapper when `needsAuth` is true.

- [ ] **Step 2: Update `getEcommerceTemplateContent` to call the new function**

Replace the static `constants.ECOMMERCE_TEMPLATE_PROVIDERS` return with a call to the dynamic builder. This requires passing project context to resolve `needsAuth`.

---

### Task 11: Final build verification & commit

- [ ] **Step 1: Run full build**

Run: `cd /Users/knid/Projects/wemob-backend && go build ./...`
Expected: Clean build with no errors

- [ ] **Step 2: Commit all changes**
