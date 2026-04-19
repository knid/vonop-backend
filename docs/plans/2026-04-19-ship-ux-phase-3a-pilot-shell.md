# Ship UX — Phase 3a · Pilot Shell + Connection Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the Publish Pilot slide-over that users open from a Launchpad store tile (Start setup / Submit update). 5-step flow — Connection, Identity, Describe, Show, Review — with live Apple/Google connection validation and submission via the existing `/v1/deployments` endpoint. AI drafts, listing preview phone frame, and fastlane produce/download-metadata are deferred to Phase 3b/3c.

**Architecture:** Pilot is a client-side slide-over component that lives next to `/apps/:appId/ship` and is opened by store-tile actions. Step state is a single React object; each step reads + writes a slice. New backend endpoints `POST /v1/connections/apple/validate` and `POST /v1/connections/google-play/validate` hit Apple App Store Connect / Google Play Publishing APIs and return a small result so the UX can show "✓ We saw 3 apps in your account" before the user wastes a 15-minute build on bad creds.

**Tech stack:** Go 1.22+, chi/v5, Bun, existing Apple App Store Connect API client (or a new fetch). Frontend: Next.js 16, React 19, Tailwind 4, sonner, existing upload helpers.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` (Publish Pilot, Connections).

---

## Test strategy

Same discipline as Phase 1/2: `go build` + `go vet` clean, `npm run build` + lint clean on new files, Postgres MCP for data checks, runtime smoke documented for user (sandbox can't boot backend).

## File structure overview

### Backend (wemob-backend) — new files

```
internal/controllers/connection_validation.go   — ValidateAppleConnection + ValidateGooglePlayConnection
internal/views/connection_validation_views.go   — HTTP handlers wrapping the two controller methods
internal/requests/connection_validation.go      — request bodies
internal/responses/connection_validation.go     — response shapes
```

### Backend — modified

```
cmd/main.go  — register POST /v1/connections/apple/validate + /v1/connections/google-play/validate routes
```

### Frontend (wemob-app) — new files

```
src/components/ship/pilot/pilot.tsx                 — slide-over container + step routing
src/components/ship/pilot/step-indicator.tsx        — 5 dots + progress
src/components/ship/pilot/step-connection.tsx       — step 1: managed vs DIY + connection forms
src/components/ship/pilot/step-identity.tsx         — step 2: bundle ID, app name preview (read-mostly)
src/components/ship/pilot/step-describe.tsx         — step 3: title + description textareas
src/components/ship/pilot/step-show.tsx             — step 4: screenshots list (reuse existing)
src/components/ship/pilot/step-review.tsx           — step 5: checklist + submit
src/components/ship/pilot/pilot-state.ts            — shared PilotState type + defaults
src/hooks/use-pilot.ts                              — open / close / step management
```

### Frontend — modified

```
src/lib/api.ts                               — add validateAppleConnection + validateGooglePlayConnection
src/components/ship/store-tile.tsx           — wire "Start setup" / "Submit update" buttons to open Pilot
src/components/ship/launchpad.tsx            — host the Pilot overlay when open
src/lib/ship/launchpad-types.ts              — ValidationResult type
```

## Why these boundaries

- **Validation endpoints separate from connection CRUD.** CRUD endpoints live in `internal/controllers/credentials.go` (renamed Phase 1). Validation hits external APIs, has different error paths, and will grow an allowlist cache — own file, own handler.
- **Pilot lives under `components/ship/pilot/`.** One folder per Phase 3 subsystem keeps the `components/ship/` namespace flat enough to reason about. Each step is its own file for focused editing.
- **Shared PilotState in a separate type file.** Step components import the type, not each other. Keeps coupling low as the shape evolves in 3b (AI drafts, preview).
- **`use-pilot.ts` encapsulates open/close/navigate.** The Launchpad hosts the overlay and manages visibility; individual store tiles don't own the Pilot lifecycle.

---

## Task 1: Define connection validation request + response types

**Files:**
- Create: `internal/requests/connection_validation.go`
- Create: `internal/responses/connection_validation.go`

- [ ] **Step 1: Requests**

Write `internal/requests/connection_validation.go`:

```go
package requests

// ValidateAppleConnectionRequest validates an App Store Connect API
// key before it's saved, so the user gets "✓ We saw 3 apps" feedback
// instantly instead of discovering a bad key mid-deploy.
type ValidateAppleConnectionRequest struct {
	KeyID     string `json:"key_id"`
	IssuerID  string `json:"issuer_id"`
	TeamID    string `json:"team_id"`
	P8KeyID   string `json:"p8_key_id"`   // S3 storage id of the uploaded .p8
}

// ValidateGooglePlayConnectionRequest validates a Play Console service
// account JSON via the Play Publishing API.
type ValidateGooglePlayConnectionRequest struct {
	ServiceAccountID string `json:"service_account_id"` // S3 storage id of the uploaded .json
}
```

- [ ] **Step 2: Responses**

Write `internal/responses/connection_validation.go`:

```go
package responses

// ValidationResult is returned by the connection validation endpoints.
// Valid=true means the credentials successfully authenticated against
// the external API. When Apps is populated, the UI can render
// "We saw 3 apps in your account, including TaskTracker Pro" for trust.
type ValidationResult struct {
	Valid   bool     `json:"valid"`
	Message string   `json:"message"`
	Apps    []string `json:"apps,omitempty"` // app display names the key can see
}
```

- [ ] **Step 3: Verify compile + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/requests/connection_validation.go internal/responses/connection_validation.go
git commit -m "feat(ship): define connection validation request + response types

Inputs mirror what the connection-create endpoints already accept.
ValidationResult carries a 'we saw N apps' list so the Pilot can show
immediate evidence the credentials are real before the user saves them."
```

---

## Task 2: Implement controller validators

**Files:**
- Create: `internal/controllers/connection_validation.go`

- [ ] **Step 1: Write the controller methods**

Create `internal/controllers/connection_validation.go`:

```go
package controllers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// ValidateAppleConnection confirms the provided App Store Connect API
// credentials can authenticate and list apps. Returns Valid=false with
// a human message on failure rather than an error — the caller surfaces
// this as UI feedback, not an HTTP 500.
func (c *Controller) ValidateAppleConnection(ctx context.Context, req requests.ValidateAppleConnectionRequest) responses.ValidationResult {
	if req.KeyID == "" || req.IssuerID == "" || req.TeamID == "" || req.P8KeyID == "" {
		return responses.ValidationResult{Valid: false, Message: "All four fields are required."}
	}

	// Download the .p8 from S3 using the existing storage service.
	p8Bytes, err := c.Storage.DownloadUserAPIKey(ctx, req.P8KeyID)
	if err != nil {
		return responses.ValidationResult{Valid: false, Message: "Couldn't read your .p8 file — re-upload and try again."}
	}

	// Build a JWT per App Store Connect API spec.
	token, err := buildAppStoreConnectJWT(req.KeyID, req.IssuerID, p8Bytes)
	if err != nil {
		return responses.ValidationResult{Valid: false, Message: fmt.Sprintf("Couldn't sign the JWT: %v", err)}
	}

	// Call GET https://api.appstoreconnect.apple.com/v1/apps (limit 20).
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", "https://api.appstoreconnect.apple.com/v1/apps?limit=20", nil)
	httpReq.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return responses.ValidationResult{Valid: false, Message: fmt.Sprintf("Apple API unreachable: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return responses.ValidationResult{Valid: false, Message: "Apple rejected the key. Check Key ID, Issuer ID, and that the key has Admin role."}
	}
	if resp.StatusCode != 200 {
		return responses.ValidationResult{Valid: false, Message: fmt.Sprintf("Apple returned HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))}
	}

	apps := parseAppStoreConnectAppNames(body)
	msg := fmt.Sprintf("We saw %d app%s in your App Store Connect account.", len(apps), plural(len(apps)))
	if len(apps) == 0 {
		msg = "Key is valid but no apps are registered yet. That's fine — new submissions create app records automatically."
	}
	return responses.ValidationResult{Valid: true, Message: msg, Apps: apps}
}

// ValidateGooglePlayConnection confirms the provided service account
// JSON can authenticate against the Play Publishing API.
func (c *Controller) ValidateGooglePlayConnection(ctx context.Context, req requests.ValidateGooglePlayConnectionRequest) responses.ValidationResult {
	if req.ServiceAccountID == "" {
		return responses.ValidationResult{Valid: false, Message: "Service account JSON is required."}
	}
	jsonBytes, err := c.Storage.DownloadUserAPIKey(ctx, req.ServiceAccountID)
	if err != nil {
		return responses.ValidationResult{Valid: false, Message: "Couldn't read your service account file — re-upload and try again."}
	}

	// Exchange the service account for an OAuth token using the
	// existing google auth helper if one exists, else inline here.
	token, err := googleServiceAccountAccessToken(ctx, jsonBytes, "https://www.googleapis.com/auth/androidpublisher")
	if err != nil {
		return responses.ValidationResult{Valid: false, Message: fmt.Sprintf("Couldn't exchange the service account for an access token: %v", err)}
	}

	// Play Publishing API doesn't have a simple "list apps" call — the
	// canonical way to validate is to call /androidpublisher/v3/applications/{pkg}/edits,
	// which requires a known package name. Since we don't have one at
	// validation time, a lighter test is /androidpublisher/v3/applications.
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", "https://androidpublisher.googleapis.com/androidpublisher/v3/applications", nil)
	httpReq.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return responses.ValidationResult{Valid: false, Message: fmt.Sprintf("Google API unreachable: %v", err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return responses.ValidationResult{Valid: false, Message: "Google rejected the service account. Check that the Play Publishing API is enabled and the account is linked in Play Console."}
	}
	if resp.StatusCode == 404 {
		// Google returns 404 when the service account isn't linked yet,
		// which is still a "credentials work" outcome — surface as warn.
		return responses.ValidationResult{Valid: true, Message: "Credentials authenticated, but no apps are linked yet. Add this service account to your Play Console app once first."}
	}
	if resp.StatusCode != 200 {
		return responses.ValidationResult{Valid: false, Message: fmt.Sprintf("Google returned HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))}
	}

	apps := parsePlayPublishingAppNames(body)
	msg := fmt.Sprintf("We saw %d app%s in your Play Console.", len(apps), plural(len(apps)))
	if len(apps) == 0 {
		msg = "Credentials authenticated. No apps visible yet — linking completes on first publish."
	}
	return responses.ValidationResult{Valid: true, Message: msg, Apps: apps}
}

// Helpers —

func buildAppStoreConnectJWT(keyID, issuerID string, p8PEM []byte) (string, error) {
	privateKey, err := jwt.ParseECPrivateKeyFromPEM(p8PEM)
	if err != nil {
		return "", err
	}
	t := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": issuerID,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"aud": "appstoreconnect-v1",
	})
	t.Header["kid"] = keyID
	return t.SignedString(privateKey)
}

func googleServiceAccountAccessToken(ctx context.Context, serviceAccountJSON []byte, scope string) (string, error) {
	// Use the existing google-oauth helper if the services layer
	// has one. Otherwise implement inline here:
	// 1. Parse SA JSON -> client_email, private_key
	// 2. Build JWT with aud=https://oauth2.googleapis.com/token, scope
	// 3. POST to token endpoint, parse access_token
	// If an existing helper exists in internal/services/, use it and
	// delete the placeholder below.
	return "", fmt.Errorf("google service-account token exchange not yet implemented — see task note")
}

func parseAppStoreConnectAppNames(body []byte) []string {
	// Minimal: pull `.data[].attributes.name` strings out of the JSON.
	// Use encoding/json with a narrow struct rather than a full SDK.
	type item struct{ Attributes struct{ Name string } }
	var payload struct{ Data []item }
	if err := jsonDecode(body, &payload); err != nil {
		return nil
	}
	names := make([]string, 0, len(payload.Data))
	for _, it := range payload.Data {
		if it.Attributes.Name != "" {
			names = append(names, it.Attributes.Name)
		}
	}
	return names
}

func parsePlayPublishingAppNames(body []byte) []string {
	type item struct{ Title, PackageName string }
	var payload struct{ Applications []item }
	if err := jsonDecode(body, &payload); err != nil {
		return nil
	}
	names := make([]string, 0, len(payload.Applications))
	for _, it := range payload.Applications {
		if it.Title != "" {
			names = append(names, it.Title)
		} else if it.PackageName != "" {
			names = append(names, it.PackageName)
		}
	}
	return names
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func jsonDecode(body []byte, out interface{}) error {
	return decodeJSONFromBytes(body, out)
}
```

- [ ] **Step 2: Inspect existing helpers and adapt**

Before committing, **investigate**:

1. Is there already an `app-store-connect` helper that signs JWTs and/or lists apps? Check `internal/services/` for anything Apple-related:
   ```bash
   grep -rn "appstoreconnect\|app_store_connect\|ParseECPrivateKeyFromPEM" /Users/knid/Projects/wemob/wemob-backend/internal/services/ /Users/knid/Projects/wemob/wemob-backend/internal/controllers/ | head -10
   ```
   If yes — use it instead of reimplementing JWT signing.

2. Is there a Google service-account token helper?
   ```bash
   grep -rn "serviceAccount\|google.*jwt\|androidpublisher\|golang.org/x/oauth2" /Users/knid/Projects/wemob/wemob-backend/internal/services/ | head -10
   ```
   If yes — use it and drop the placeholder.

3. Confirm `c.Storage.DownloadUserAPIKey` exists and signature:
   ```bash
   grep -n "DownloadUserAPIKey\|DownloadP8\|GetUserAPIKey" /Users/knid/Projects/wemob/wemob-backend/internal/services/storage/*.go
   ```
   Adapt the call to the real method name.

4. Confirm `jsonDecode` exists as a helper, or replace with `json.Unmarshal` directly (+ `"encoding/json"` import).

Adapt the placeholder code above to match real helpers. If `googleServiceAccountAccessToken` would have to be written from scratch, note that in your report — it's non-trivial (needs `golang.org/x/oauth2/google`), and it's acceptable to punt that validation path in 3a by returning `Valid: false, Message: "Google Play validation coming in Phase 3b"` until the real implementation lands. **Apple validation MUST work** for the Pilot's Apple flow to be demonstrable.

- [ ] **Step 3: Verify compile**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
```

If `jwt-go` isn't already a dependency, add it:

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go get github.com/golang-jwt/jwt/v5
```

Then `go mod tidy` and `go build` again.

- [ ] **Step 4: Commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
git add internal/controllers/connection_validation.go go.mod go.sum
git commit -m "feat(ship): add ValidateApple + ValidateGooglePlay connection controllers

Both hit the real external APIs (App Store Connect + Play Publishing)
and return a ValidationResult with Valid / Message / Apps. The Pilot's
Connection step calls these when the user pastes credentials so bad
keys are caught before a 15-minute build wastes credits.

Google service-account token exchange delegated to existing helper if
present; otherwise temporarily returns Valid=false with a 'coming in
3b' message so the build stays green."
```

---

## Task 3: Add HTTP handlers + routes

**Files:**
- Create: `internal/views/connection_validation_views.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Write the handlers**

`internal/views/connection_validation_views.go`:

```go
package views

import (
	"net/http"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// ValidateAppleConnection handles POST /v1/connections/apple/validate.
func (v *HttpViews) ValidateAppleConnection(w http.ResponseWriter, r *http.Request) {
	user := v.Controller.GetUserFromReq(r)
	if user == nil {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req requests.ValidateAppleConnectionRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	result := v.Controller.ValidateAppleConnection(r.Context(), req)
	utils.JSONResponse(w, result, http.StatusOK)
}

// ValidateGooglePlayConnection handles POST /v1/connections/google-play/validate.
func (v *HttpViews) ValidateGooglePlayConnection(w http.ResponseWriter, r *http.Request) {
	user := v.Controller.GetUserFromReq(r)
	if user == nil {
		utils.JSONErrorResponse(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req requests.ValidateGooglePlayConnectionRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	result := v.Controller.ValidateGooglePlayConnection(r.Context(), req)
	utils.JSONResponse(w, result, http.StatusOK)
}
```

Match the existing view helper conventions — `utils.ParseJSONBody`, `utils.JSONResponse`, `utils.JSONErrorResponse`. Adapt if names differ.

- [ ] **Step 2: Register routes in `cmd/main.go`**

Find the existing `/v1/connections` block (added in Phase 1). Add two routes inside:

```go
r.Route("/connections", func(r chi.Router) {
    r.Route("/google-play", func(r chi.Router) {
        // ... existing CRUD routes
        r.Post("/validate", httpViews.ValidateGooglePlayConnection)
    })
    r.Route("/apple", func(r chi.Router) {
        // ... existing CRUD routes
        r.Post("/validate", httpViews.ValidateAppleConnection)
    })
})
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/views/connection_validation_views.go cmd/main.go
git commit -m "feat(ship): wire POST /v1/connections/{apple,google-play}/validate routes

Auth-gated (GetUserFromReq), parse JSON, delegate to the validator
methods on the controller, return the ValidationResult as 200 JSON
regardless of whether the creds were valid — Valid=false is a normal
result, not an HTTP error."
```

---

## Task 4: Frontend — API client methods + ValidationResult type

**Files:**
- Modify: `src/lib/ship/launchpad-types.ts`
- Modify: `src/lib/api.ts`

- [ ] **Step 1: Add types**

Append to `src/lib/ship/launchpad-types.ts`:

```ts
export interface ValidationResult {
  valid: boolean;
  message: string;
  apps?: string[];
}

export interface ValidateAppleConnectionRequest {
  key_id: string;
  issuer_id: string;
  team_id: string;
  p8_key_id: string;
}

export interface ValidateGooglePlayConnectionRequest {
  service_account_id: string;
}
```

- [ ] **Step 2: Add API client methods**

Add after `getLaunchpad` in `src/lib/api.ts`:

```ts
async validateAppleConnection(
  req: import("./ship/launchpad-types").ValidateAppleConnectionRequest,
): Promise<import("./ship/launchpad-types").ValidationResult> {
  return this.request<import("./ship/launchpad-types").ValidationResult>(
    "/v1/connections/apple/validate",
    { method: "POST", body: JSON.stringify(req) },
  );
}

async validateGooglePlayConnection(
  req: import("./ship/launchpad-types").ValidateGooglePlayConnectionRequest,
): Promise<import("./ship/launchpad-types").ValidationResult> {
  return this.request<import("./ship/launchpad-types").ValidationResult>(
    "/v1/connections/google-play/validate",
    { method: "POST", body: JSON.stringify(req) },
  );
}
```

Adapt to the `this.request` signature used by other methods.

- [ ] **Step 3: Verify lint + build + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/lib/ship/launchpad-types.ts src/lib/api.ts
git commit -m "feat(ship): add validate{Apple,GooglePlay}Connection API methods + types

Mirrors the new backend POST /v1/connections/{apple,google-play}/validate
endpoints. Used by the Pilot's Connection step to prove credentials
work before saving them."
```

---

## Task 5: Frontend — `PilotState` + `usePilot` hook

**Files:**
- Create: `src/components/ship/pilot/pilot-state.ts`
- Create: `src/hooks/use-pilot.ts`

- [ ] **Step 1: Shared state type**

```ts
// src/components/ship/pilot/pilot-state.ts
import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";

export type PilotTarget = "play_store" | "app_store";
export type UploadMethod = "wemob" | "user";

export interface PilotState {
  // Context of this submission.
  target: PilotTarget;
  method: UploadMethod;

  // Connection — selected from existing, or newly validated.
  connectionId: string;   // "" until selected/created
  connectionValidated: boolean;

  // Listing snapshot (pre-filled from LaunchpadResponse.listing + AppStoreListing).
  title: string;
  description: string;
  iconS3Key: string;
  screenshotIds: string[];
  privacyURL: string;
  locale: string;

  // Identity (read-only from Launchpad — shown for confirmation).
  bundleID: string;
  version: string;

  // Flow state.
  currentStep: number; // 1..5
}

export function initialPilotState(target: PilotTarget, data: LaunchpadResponse): PilotState {
  return {
    target,
    method: "wemob",
    connectionId: "",
    connectionValidated: false,
    title: "",
    description: "",
    iconS3Key: data.app.icon_s3_key ?? "",
    screenshotIds: [],
    privacyURL: "",
    locale: "en-US",
    bundleID: data.app.bundle_id ?? "",
    version: data.app.version_name
      ? `${data.app.version_name} (Build ${data.app.build_number})`
      : "",
    currentStep: 1,
  };
}
```

- [ ] **Step 2: Hook**

```ts
// src/hooks/use-pilot.ts
"use client";

import { useCallback, useState } from "react";
import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import type { PilotState, PilotTarget } from "@/components/ship/pilot/pilot-state";
import { initialPilotState } from "@/components/ship/pilot/pilot-state";

export function usePilot() {
  const [state, setState] = useState<PilotState | null>(null);

  const open = useCallback((target: PilotTarget, data: LaunchpadResponse) => {
    setState(initialPilotState(target, data));
  }, []);

  const close = useCallback(() => setState(null), []);

  const patch = useCallback((p: Partial<PilotState>) => {
    setState((s) => (s ? { ...s, ...p } : s));
  }, []);

  const goto = useCallback((step: number) => {
    setState((s) => (s ? { ...s, currentStep: step } : s));
  }, []);

  return { state, isOpen: !!state, open, close, patch, goto };
}
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/components/ship/pilot/pilot-state.ts src/hooks/use-pilot.ts
git commit -m "feat(ship): add PilotState + usePilot hook

Single state object carries the Pilot's full form: target (play/apple),
method (managed/user), connection + validation status, listing
snapshot, and current step. usePilot owns open / close / patch / goto
so components stay stateless."
```

---

## Task 6: Frontend — `StepIndicator` + `Pilot` shell

**Files:**
- Create: `src/components/ship/pilot/step-indicator.tsx`
- Create: `src/components/ship/pilot/pilot.tsx`

- [ ] **Step 1: StepIndicator**

```tsx
// src/components/ship/pilot/step-indicator.tsx
"use client";

import { cn } from "@/lib/utils";

const STEPS = ["Connection", "Identity", "Describe", "Show", "Review"] as const;

export function StepIndicator({ current }: { current: number }) {
  return (
    <div className="flex items-center gap-2 px-5 py-3 border-b border-[var(--border)]">
      {STEPS.map((label, idx) => {
        const n = idx + 1;
        const active = n === current;
        const done = n < current;
        return (
          <div key={label} className="flex items-center gap-2">
            <div
              className={cn(
                "w-5 h-5 rounded-full inline-flex items-center justify-center text-[9px] font-medium",
                active && "bg-[var(--ink)] text-white",
                done && "bg-[var(--success)] text-white",
                !active && !done && "bg-[var(--bg-raised)] border border-[var(--border)] text-[var(--ink-faint)]",
              )}
            >
              {done ? "✓" : n}
            </div>
            <span
              className={cn(
                "text-[10px]",
                active ? "font-medium text-[var(--ink)]" : "text-[var(--ink-faint)]",
              )}
            >
              {label}
            </span>
            {idx < STEPS.length - 1 && (
              <div className={cn("w-6 h-px", done ? "bg-[var(--success)]" : "bg-[var(--border)]")} />
            )}
          </div>
        );
      })}
    </div>
  );
}
```

- [ ] **Step 2: Pilot shell container**

```tsx
// src/components/ship/pilot/pilot.tsx
"use client";

import { X } from "lucide-react";
import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import type { PilotState } from "./pilot-state";
import { StepIndicator } from "./step-indicator";
import { StepConnection } from "./step-connection";
import { StepIdentity } from "./step-identity";
import { StepDescribe } from "./step-describe";
import { StepShow } from "./step-show";
import { StepReview } from "./step-review";

interface PilotProps {
  state: PilotState;
  data: LaunchpadResponse;
  onPatch: (p: Partial<PilotState>) => void;
  onGoto: (step: number) => void;
  onClose: () => void;
  onSubmitted: () => void; // called after successful deployment create
}

export function Pilot({ state, data, onPatch, onGoto, onClose, onSubmitted }: PilotProps) {
  return (
    <div
      onClick={onClose}
      className="fixed inset-0 bg-[rgba(26,26,26,0.35)] backdrop-blur-[2px] flex justify-end z-[200]"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-[560px] max-w-[92vw] h-full bg-[var(--bg-surface)] border-l border-[var(--border)] flex flex-col shadow-[var(--shadow-xl)]"
      >
        <header className="flex items-center px-5 py-4 border-b border-[var(--border)]">
          <div className="font-serif text-[18px]">
            Ship to {state.target === "play_store" ? "Play Store" : "App Store"}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="ml-auto w-7 h-7 rounded-md inline-flex items-center justify-center text-[var(--ink-muted)] hover:bg-[var(--bg-hover)]"
          >
            <X size={14} />
          </button>
        </header>

        <StepIndicator current={state.currentStep} />

        <div className="flex-1 overflow-y-auto">
          {state.currentStep === 1 && (
            <StepConnection state={state} data={data} onPatch={onPatch} />
          )}
          {state.currentStep === 2 && (
            <StepIdentity state={state} onPatch={onPatch} />
          )}
          {state.currentStep === 3 && (
            <StepDescribe state={state} onPatch={onPatch} />
          )}
          {state.currentStep === 4 && (
            <StepShow state={state} data={data} onPatch={onPatch} />
          )}
          {state.currentStep === 5 && (
            <StepReview state={state} data={data} onSubmitted={onSubmitted} onClose={onClose} />
          )}
        </div>

        <footer className="flex items-center gap-2 px-5 py-3 border-t border-[var(--border)]">
          <button
            type="button"
            onClick={() => onGoto(Math.max(1, state.currentStep - 1))}
            disabled={state.currentStep === 1}
            className="px-3 py-1.5 rounded-md text-[12px] border border-[var(--border-strong)] disabled:opacity-40"
          >
            ← Back
          </button>
          <div className="flex-1" />
          {state.currentStep < 5 ? (
            <button
              type="button"
              onClick={() => onGoto(state.currentStep + 1)}
              disabled={!canContinue(state)}
              className="px-3.5 py-2 rounded-md text-[12px] font-medium bg-[var(--ink)] text-white disabled:opacity-40"
            >
              Continue →
            </button>
          ) : null}
        </footer>
      </div>
    </div>
  );
}

function canContinue(s: PilotState): boolean {
  switch (s.currentStep) {
    case 1:
      return s.method === "wemob" || (s.method === "user" && s.connectionValidated);
    case 2:
      return !!s.bundleID; // identity is pre-filled; advance freely unless bundle missing
    case 3:
      return s.title.trim().length >= 2 && s.description.trim().length >= 10;
    case 4:
      return true; // screenshots optional in 3a
    case 5:
      return false; // step 5 has its own submit button
  }
  return false;
}
```

This file imports the five step components that don't exist yet (Tasks 7–11). Commits for those fold together. Keep lint disabled for now — proceed to Task 7.

---

## Task 7: Frontend — `StepConnection`

**Files:**
- Create: `src/components/ship/pilot/step-connection.tsx`

- [ ] **Step 1: Write the component**

```tsx
"use client";

import { useEffect, useState } from "react";
import { apiClient } from "@/lib/api";
import type { LaunchpadResponse, ValidationResult } from "@/lib/ship/launchpad-types";
import type { PilotState, UploadMethod } from "./pilot-state";

interface StepConnectionProps {
  state: PilotState;
  data: LaunchpadResponse;
  onPatch: (p: Partial<PilotState>) => void;
}

export function StepConnection({ state, data, onPatch }: StepConnectionProps) {
  return (
    <div className="px-5 py-5">
      <h2 className="font-serif text-[20px] mb-2">Who ships this?</h2>
      <p className="text-[12px] text-[var(--ink-muted)] mb-4">
        Pick once. You can change for future submissions.
      </p>

      <div className="flex flex-col gap-2.5">
        <MethodRadio
          value="wemob"
          current={state.method}
          title="Wemob ships for you"
          body="We use our developer account. No Apple or Google fees. Live in ~2 business days. App appears under Wemob Ltd."
          recommended
          onSelect={(v) => onPatch({ method: v, connectionValidated: v === "wemob" })}
        />
        <MethodRadio
          value="user"
          current={state.method}
          title={`Ship from your ${state.target === "app_store" ? "Apple Developer" : "Google Play"} account`}
          body="You own the listing. We use your API key. Requires an active developer account."
          onSelect={(v) => onPatch({ method: v, connectionValidated: false })}
        />
      </div>

      {state.method === "user" && (
        <div className="mt-5 border-t border-[var(--border)] pt-4">
          {state.target === "app_store" ? (
            <AppleConnectionValidator state={state} onPatch={onPatch} />
          ) : (
            <GoogleConnectionValidator state={state} onPatch={onPatch} />
          )}
        </div>
      )}
    </div>
  );
}

function MethodRadio({
  value,
  current,
  title,
  body,
  recommended,
  onSelect,
}: {
  value: UploadMethod;
  current: UploadMethod;
  title: string;
  body: string;
  recommended?: boolean;
  onSelect: (v: UploadMethod) => void;
}) {
  const active = current === value;
  return (
    <button
      type="button"
      onClick={() => onSelect(value)}
      className={
        "text-left rounded-xl px-4 py-3 border transition-colors " +
        (active
          ? "border-[var(--accent)] bg-[var(--accent-soft)]"
          : "border-[var(--border)] hover:border-[var(--border-strong)]")
      }
    >
      <div className="flex items-center gap-2">
        <div
          className={
            "w-5 h-5 rounded-full border-2 flex items-center justify-center " +
            (active ? "border-[var(--accent)]" : "border-[var(--border-strong)]")
          }
        >
          {active && <div className="w-2.5 h-2.5 rounded-full bg-[var(--accent)]" />}
        </div>
        <div className="font-medium text-[13px]">{title}</div>
        {recommended && (
          <span className="ml-auto text-[9px] px-2 py-0.5 bg-[var(--success)] text-white rounded-full uppercase tracking-wide">
            Recommended
          </span>
        )}
      </div>
      <div className="pl-7 text-[12px] text-[var(--ink-muted)] mt-1 leading-[1.55]">
        {body}
      </div>
    </button>
  );
}

function AppleConnectionValidator({
  state,
  onPatch,
}: {
  state: PilotState;
  onPatch: (p: Partial<PilotState>) => void;
}) {
  const [keyID, setKeyID] = useState("");
  const [issuerID, setIssuerID] = useState("");
  const [teamID, setTeamID] = useState("");
  const [p8KeyID, setP8KeyID] = useState(""); // TODO Phase 3b: upload UI
  const [result, setResult] = useState<ValidationResult | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    onPatch({ connectionValidated: !!result?.valid });
  }, [result?.valid]); // eslint-disable-line react-hooks/exhaustive-deps

  async function validate() {
    setBusy(true);
    setResult(null);
    try {
      const r = await apiClient.validateAppleConnection({
        key_id: keyID.trim(),
        issuer_id: issuerID.trim(),
        team_id: teamID.trim(),
        p8_key_id: p8KeyID.trim(),
      });
      setResult(r);
    } catch (err) {
      setResult({
        valid: false,
        message: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <div className="text-[13px] font-medium mb-2">Paste your App Store Connect API Key</div>
      <p className="text-[11px] text-[var(--ink-muted)] mb-3 leading-[1.55]">
        Find this in appstoreconnect.apple.com → Users and Access → Keys. Key must have Admin role.
      </p>
      <div className="flex flex-col gap-2">
        <Input label="Team ID" value={teamID} onChange={setTeamID} placeholder="ABCDE12345" />
        <Input label="Key ID" value={keyID} onChange={setKeyID} placeholder="ABCDE12345" />
        <Input label="Issuer ID" value={issuerID} onChange={setIssuerID} placeholder="69a6de70-..." />
        <Input label="P8 upload ID (Phase 3b UI)" value={p8KeyID} onChange={setP8KeyID} placeholder="storage-id" />
      </div>
      <button
        type="button"
        onClick={validate}
        disabled={busy || !keyID || !issuerID || !teamID || !p8KeyID}
        className="mt-3 px-3 py-1.5 rounded-md text-[12px] bg-[var(--ink)] text-white disabled:opacity-40"
      >
        {busy ? "Validating…" : "Validate"}
      </button>
      {result && (
        <div
          className={
            "mt-3 px-3 py-2 rounded-md text-[12px] border " +
            (result.valid
              ? "bg-[rgba(122,155,118,0.12)] border-[var(--success)]/40 text-[var(--ink)]"
              : "bg-[var(--accent-soft)] border-[var(--accent)]/40 text-[var(--accent)]")
          }
        >
          {result.valid ? "✓" : "✗"} {result.message}
          {result.apps && result.apps.length > 0 && (
            <ul className="mt-1 text-[11px] text-[var(--ink-muted)] pl-4 list-disc">
              {result.apps.slice(0, 5).map((a) => <li key={a}>{a}</li>)}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}

function GoogleConnectionValidator({
  state,
  onPatch,
}: {
  state: PilotState;
  onPatch: (p: Partial<PilotState>) => void;
}) {
  const [saID, setSaID] = useState("");
  const [result, setResult] = useState<ValidationResult | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    onPatch({ connectionValidated: !!result?.valid });
  }, [result?.valid]); // eslint-disable-line react-hooks/exhaustive-deps

  async function validate() {
    setBusy(true);
    setResult(null);
    try {
      const r = await apiClient.validateGooglePlayConnection({
        service_account_id: saID.trim(),
      });
      setResult(r);
    } catch (err) {
      setResult({
        valid: false,
        message: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <div className="text-[13px] font-medium mb-2">Paste your Google Play service account</div>
      <p className="text-[11px] text-[var(--ink-muted)] mb-3 leading-[1.55]">
        Find/create in Google Cloud → IAM → Service accounts. Enable Play Publishing API and link the account in Play Console.
      </p>
      <Input
        label="Service Account upload ID (Phase 3b UI)"
        value={saID}
        onChange={setSaID}
        placeholder="storage-id"
      />
      <button
        type="button"
        onClick={validate}
        disabled={busy || !saID}
        className="mt-3 px-3 py-1.5 rounded-md text-[12px] bg-[var(--ink)] text-white disabled:opacity-40"
      >
        {busy ? "Validating…" : "Validate"}
      </button>
      {result && (
        <div
          className={
            "mt-3 px-3 py-2 rounded-md text-[12px] border " +
            (result.valid
              ? "bg-[rgba(122,155,118,0.12)] border-[var(--success)]/40 text-[var(--ink)]"
              : "bg-[var(--accent-soft)] border-[var(--accent)]/40 text-[var(--accent)]")
          }
        >
          {result.valid ? "✓" : "✗"} {result.message}
          {result.apps && result.apps.length > 0 && (
            <ul className="mt-1 text-[11px] text-[var(--ink-muted)] pl-4 list-disc">
              {result.apps.slice(0, 5).map((a) => <li key={a}>{a}</li>)}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}

function Input({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">{label}</span>
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="bg-[var(--bg)] border border-[var(--border)] rounded-md px-3 py-1.5 text-[12px] font-mono outline-none focus:border-[var(--accent)]"
      />
    </label>
  );
}
```

**Note:** Phase 3a intentionally uses a raw "storage id" input for the `.p8` / service account. A proper file-upload UI lands in Phase 3b alongside the upload presign flow. The plan doc records this limitation; the commit message flags it.

- [ ] **Step 2: Don't commit yet — proceed to Tasks 8–11.**

---

## Task 8: Frontend — `StepIdentity`

**Files:**
- Create: `src/components/ship/pilot/step-identity.tsx`

```tsx
"use client";

import type { PilotState } from "./pilot-state";

export function StepIdentity({
  state,
  onPatch,
}: {
  state: PilotState;
  onPatch: (p: Partial<PilotState>) => void;
}) {
  return (
    <div className="px-5 py-5">
      <h2 className="font-serif text-[20px] mb-2">Identity</h2>
      <p className="text-[12px] text-[var(--ink-muted)] mb-4">
        These values come from your project. Review before shipping.
      </p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-2 text-[12px]">
        <dt className="text-[var(--ink-muted)]">Bundle ID</dt>
        <dd className="font-mono">{state.bundleID || "—"}</dd>
        <dt className="text-[var(--ink-muted)]">Version</dt>
        <dd className="font-mono">{state.version || "not yet built"}</dd>
      </dl>
      <div className="mt-4 text-[11px] text-[var(--ink-faint)]">
        To change these, update the project settings and trigger a new build.
      </div>
    </div>
  );
}
```

---

## Task 9: Frontend — `StepDescribe`

**Files:**
- Create: `src/components/ship/pilot/step-describe.tsx`

```tsx
"use client";

import type { PilotState } from "./pilot-state";

export function StepDescribe({
  state,
  onPatch,
}: {
  state: PilotState;
  onPatch: (p: Partial<PilotState>) => void;
}) {
  return (
    <div className="px-5 py-5">
      <h2 className="font-serif text-[20px] mb-2">Describe your app</h2>
      <p className="text-[12px] text-[var(--ink-muted)] mb-4">
        The listing the store shows to users. AI drafts coming in Phase 3b.
      </p>

      <label className="flex flex-col gap-1 mb-4">
        <span className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">Title (2–30)</span>
        <input
          type="text"
          value={state.title}
          onChange={(e) => onPatch({ title: e.target.value })}
          className="bg-[var(--bg)] border border-[var(--border)] rounded-md px-3 py-2 text-[13px]"
          placeholder="TaskTracker Pro"
        />
        <span className="text-[10px] text-[var(--ink-faint)] mt-0.5">{state.title.length} chars</span>
      </label>

      <label className="flex flex-col gap-1">
        <span className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">Description</span>
        <textarea
          value={state.description}
          onChange={(e) => onPatch({ description: e.target.value })}
          rows={6}
          className="bg-[var(--bg)] border border-[var(--border)] rounded-md px-3 py-2 text-[13px] leading-[1.55] resize-y"
          placeholder="Stay on top of your day. Capture tasks the moment you think of them…"
        />
        <span className="text-[10px] text-[var(--ink-faint)] mt-0.5">
          {state.description.length} chars · 170+ recommended
        </span>
      </label>
    </div>
  );
}
```

---

## Task 10: Frontend — `StepShow`

**Files:**
- Create: `src/components/ship/pilot/step-show.tsx`

```tsx
"use client";

import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import type { PilotState } from "./pilot-state";

export function StepShow({
  state,
  data,
  onPatch,
}: {
  state: PilotState;
  data: LaunchpadResponse;
  onPatch: (p: Partial<PilotState>) => void;
}) {
  return (
    <div className="px-5 py-5">
      <h2 className="font-serif text-[20px] mb-2">Show your app</h2>
      <p className="text-[12px] text-[var(--ink-muted)] mb-4">
        Screenshots. Phase 3a reuses what's already on your listing; full upload + AI generation lands in Phase 3b.
      </p>

      <div className="bg-[var(--bg-raised)] border border-dashed border-[var(--border)] rounded-xl p-6 text-center text-[12px] text-[var(--ink-muted)]">
        Current listing has {data.listing.tiles.find((t) => t.key === "screenshots")?.summary ?? "0 screenshots"}.
        <div className="mt-2">
          <span className="text-[11px] text-[var(--ink-faint)]">Full upload + AI generation — Phase 3b.</span>
        </div>
      </div>
    </div>
  );
}
```

---

## Task 11: Frontend — `StepReview` + submit

**Files:**
- Create: `src/components/ship/pilot/step-review.tsx`

```tsx
"use client";

import { useState } from "react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api";
import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import type { PilotState } from "./pilot-state";

interface StepReviewProps {
  state: PilotState;
  data: LaunchpadResponse;
  onSubmitted: () => void;
  onClose: () => void;
}

export function StepReview({ state, data, onSubmitted, onClose }: StepReviewProps) {
  const [busy, setBusy] = useState(false);

  const platform = state.target === "app_store" ? "ios" : "android";
  const v1ProjectID = data.app.v1_project_id;

  const checklist = [
    { label: "Connection selected or validated", ok: state.method === "wemob" || state.connectionValidated },
    { label: "Title set (2–30 chars)", ok: state.title.trim().length >= 2 && state.title.trim().length <= 30 },
    { label: "Description set (≥ 10 chars)", ok: state.description.trim().length >= 10 },
    { label: "Bundle ID present", ok: !!state.bundleID },
    { label: "Build artifact ready", ok: data.app.build_number > 0 },
  ];
  const allGood = checklist.every((c) => c.ok);

  async function submit() {
    if (!v1ProjectID) {
      toast.error("This app hasn't finished provisioning yet.");
      return;
    }
    setBusy(true);
    try {
      // Find the latest BuildID for this platform — any completed build
      // will do for 3a. Frontend pulls from the Launchpad's recent builds
      // (filter by platform, pick first).
      const candidates = [...data.builds.active, ...data.builds.recent]
        .filter((b) => b.platform === platform && b.finished);
      const latest = candidates[0];
      if (!latest) {
        toast.error(`No completed ${platform} build to submit. Run a build first.`);
        setBusy(false);
        return;
      }
      await apiClient.createDeployment({
        project_id: v1ProjectID,
        build_id: latest.build_id,
        platforms: [platform],
        icon_id: state.iconS3Key,
        title: state.title.trim(),
        description: state.description.trim(),
        locale: state.locale,
        method: state.method,
        google_play_credentials_id: state.method === "user" && platform === "android" ? state.connectionId : "",
        app_store_credentials_id: state.method === "user" && platform === "ios" ? state.connectionId : "",
        screenshot_paths: state.screenshotIds,
      });
      toast.success("Submission started", {
        description: state.method === "wemob"
          ? "Our team will publish within 2–3 business days."
          : "Uploading to the store now. Review windows still apply.",
      });
      onSubmitted();
      onClose();
    } catch (err) {
      toast.error("Couldn't submit", { description: err instanceof Error ? err.message : String(err) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="px-5 py-5">
      <h2 className="font-serif text-[20px] mb-2">Review &amp; submit</h2>
      <p className="text-[12px] text-[var(--ink-muted)] mb-4">
        Last check before we send this to the store.
      </p>

      <div className="border border-[var(--border)] rounded-xl p-4 mb-4">
        <div className="text-[11px] uppercase tracking-wider text-[var(--ink-faint)] mb-2">Checklist</div>
        <ul className="flex flex-col gap-1.5">
          {checklist.map((c) => (
            <li key={c.label} className="text-[12px] flex items-center gap-2">
              <span
                className={
                  "w-4 h-4 rounded-full inline-flex items-center justify-center text-[9px] " +
                  (c.ok ? "bg-[var(--success)] text-white" : "bg-[var(--accent-soft)] text-[var(--accent)]")
                }
              >
                {c.ok ? "✓" : "!"}
              </span>
              <span className={c.ok ? "" : "text-[var(--accent)]"}>{c.label}</span>
            </li>
          ))}
        </ul>
      </div>

      <button
        type="button"
        onClick={submit}
        disabled={busy || !allGood}
        className="w-full px-3.5 py-2.5 bg-[var(--accent)] text-white rounded-lg text-[13px] font-medium disabled:opacity-40"
      >
        {busy ? "Submitting…" : `Submit to ${state.target === "app_store" ? "App Store" : "Play Store"}`}
      </button>
    </div>
  );
}
```

---

## Task 12: Wire Pilot into Launchpad + store tiles

**Files:**
- Modify: `src/components/ship/launchpad.tsx`
- Modify: `src/components/ship/store-tile.tsx`

- [ ] **Step 1: Host the Pilot in Launchpad**

Update `launchpad.tsx` to manage Pilot state and render it when open. Pass a callback to `StoreTile` that opens the Pilot for the clicked store.

Replace `launchpad.tsx` with:

```tsx
"use client";

import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import { usePilot } from "@/hooks/use-pilot";
import { BuildsFeed } from "./builds-feed";
import { IdentityPanel } from "./identity-panel";
import { LaunchpadHeader } from "./launchpad-header";
import { ListingGrid } from "./listing-grid";
import { NewBuildButton } from "./new-build-button";
import { Pilot } from "./pilot/pilot";
import { StoreTile } from "./store-tile";

interface LaunchpadProps {
  data: LaunchpadResponse;
  onRefetch: () => void;
}

export function Launchpad({ data, onRefetch }: LaunchpadProps) {
  const pilot = usePilot();

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="px-8 py-6 pb-16 max-w-[880px] mx-auto">
        <LaunchpadHeader data={data} />

        <section className="mt-6">
          <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)] mb-2">
            Stores
          </div>
          <div className="grid grid-cols-2 gap-2.5">
            <StoreTile kind="play_store" tile={data.stores.play_store} onAction={() => pilot.open("play_store", data)} />
            <StoreTile kind="app_store" tile={data.stores.app_store} onAction={() => pilot.open("app_store", data)} />
          </div>
        </section>

        <ListingGrid listing={data.listing} />

        <section className="mt-6">
          <div className="flex items-center justify-between mb-2">
            <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)]">
              Builds
            </div>
            <NewBuildButton app={data.app} onTriggered={onRefetch} />
          </div>
          <BuildsFeed builds={data.builds} />
        </section>

        <IdentityPanel identity={data.identity} />
      </div>

      {pilot.state && (
        <Pilot
          state={pilot.state}
          data={data}
          onPatch={pilot.patch}
          onGoto={pilot.goto}
          onClose={pilot.close}
          onSubmitted={onRefetch}
        />
      )}
    </div>
  );
}
```

- [ ] **Step 2: Wire `StoreTile`'s primary action button**

Add an `onAction` prop and call it from the button click. Open `src/components/ship/store-tile.tsx`. Update the interface:

```tsx
interface StoreTileProps {
  kind: "play_store" | "app_store";
  tile: LaunchpadStoreTile;
  onAction?: () => void;
}
```

Update the destructuring and the button:

```tsx
export function StoreTile({ kind, tile, onAction }: StoreTileProps) {
  // ...

  {tile.primary_action && (
    <button
      type="button"
      onClick={onAction}
      className="px-3 py-1.5 bg-[var(--ink)] text-white rounded-md text-[11px] font-medium hover:opacity-90"
    >
      {tile.primary_action}
    </button>
  )}
}
```

- [ ] **Step 3: Verify + commit all Pilot files at once**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/pilot/ src/hooks/use-pilot.ts src/components/ship/launchpad.tsx src/components/ship/store-tile.tsx
git commit -m "feat(ship): Publish Pilot shell + 5-step flow + connection validation wiring

New slide-over opens from store-tile 'Start setup' / 'Submit update'
action. Five steps: Connection, Identity, Describe, Show, Review.

Connection step offers managed (default) vs DIY; DIY validates the
user's Apple App Store Connect key / Google Play service account via
the new POST /v1/connections/:platform/validate endpoints before the
flow can advance.

Submit creates a Deployment via the existing /v1/deployments endpoint
using the latest completed build for the chosen platform.

Phase 3a scope: no AI drafts, no phone-frame preview, no file-upload
UI for credentials (user pastes S3 storage ids for now). 3b adds all
three."
```

---

## Task 13: End-to-end smoke

- [ ] **Step 1: Documented for the user**

```
# Backend
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go &

# Frontend
cd /Users/knid/Projects/wemob/wemob-app && npm run dev

# Browser: http://localhost:3000/apps/<appId>/ship
# 1. Store tile's primary action ("Start setup" / "Submit update") opens the Pilot as a right-side slide-over.
# 2. Step 1 — Connection: pick "Wemob ships" → continue enabled. Or pick DIY → connection form appears → paste creds → Validate button → toast-style result.
# 3. Steps 2-4: identity (read-only), describe (title + description), show (stub).
# 4. Step 5: checklist; Submit → POST /v1/deployments → toast success → Pilot closes, Launchpad refetches.
```

No runtime call in-sandbox; this is a hand-off step. Task complete without a commit.

---

## Phase 3a completion criteria

- Backend + frontend build/vet/lint clean.
- `POST /v1/connections/apple/validate` returns `Valid=true` with an `apps` list when given a working p8 + Team/Key/Issuer IDs; `Valid=false` on bad input.
- `POST /v1/connections/google-play/validate` returns a sensible `ValidationResult` (may be temporarily `Valid=false` with "coming in 3b" message if the service-account helper isn't present — note in report).
- Clicking a store tile's primary action opens the Pilot.
- Pilot's Continue button respects `canContinue` per step.
- Submit calls `createDeployment` and refetches the Launchpad.

Phase 3b (phone-frame preview + AI drafts + file upload UI) unblocked.

## Out of scope (Phase 3b)

- Live Play/App Store phone-frame preview on the Review step.
- AI-drafted description, release notes, screenshots.
- File upload UI for p8 / service-account JSON (currently raw S3 IDs).
- Fastlane `produce` + `deliver --download-metadata` / `supply --download` (Phase 3c).
- ⌘K, /ship table, Quick Ship (Phase 4).
