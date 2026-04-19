# Ship UX — Phase 3b · Pilot Polish (Uploads + AI Drafts + Preview) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Polish the Phase 3a Pilot with the three user-visible pieces the spec calls for: (1) file upload UI for `.p8` (Apple) and service-account JSON (Google) replacing the raw S3 IDs; (2) AI-drafted title / description / release notes rendered inline with regenerate; (3) Play Store / App Store phone-frame preview on the Review step. Fastlane `produce` + `download-metadata` still deferred to 3c.

**Architecture:** Backend gains one new endpoint — `POST /v1/apps/:appId/listing/draft` — that uses the existing `utils.GetJSONFromLLM` helper + token-tracking pattern (same as `NavBarAutoFillWithAI`) to generate title, description, and release notes based on project context. Frontend reuses the existing `/v1/storage/credentials/{apple,google}` presign endpoints for uploads (no new backend upload endpoints) and wraps them in a small `<FileUpload>` component. Phone preview is pure HTML/CSS, zero round-trips.

**Tech stack:** Go 1.22+, Bun, chi/v5, `utils.GetJSONFromLLM` helper with Claude Haiku. Frontend: Next.js 16, React 19, Tailwind 4, existing presign flow.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` (Publish Pilot, Listing preview).

---

## Test strategy

Same as prior phases: `go build` + `go vet` clean on backend; `npm run build` + lint clean on frontend; Postgres MCP where useful; runtime smoke documented for user.

## File structure overview

### Backend — new files

```
internal/requests/listing_draft.go            — draft request shape
internal/responses/listing_draft.go           — draft response shape
internal/controllers/listing_draft_controller.go — DraftListing uses GetJSONFromLLM
internal/views/listing_draft_views.go         — HTTP handler
internal/constants/listing_draft_prompts.go   — system prompt constant
```

### Backend — modified

```
cmd/main.go  — register POST /v1/apps/:appId/listing/draft under the existing /apps/:appId subtree
```

### Frontend — new files

```
src/components/ship/pilot/file-upload.tsx        — reusable file picker → presign → upload → storage id
src/components/ship/pilot/listing-preview.tsx    — Play / App Store phone-frame preview (HTML/CSS)
```

### Frontend — modified

```
src/lib/api.ts                                       — add draftListing method
src/lib/ship/launchpad-types.ts                      — add ListingDraft + ListingDraftResponse types
src/components/ship/pilot/step-connection.tsx        — swap raw p8/SA inputs for <FileUpload>
src/components/ship/pilot/step-describe.tsx          — add "Draft with AI" buttons + regenerate
src/components/ship/pilot/step-review.tsx            — host <ListingPreview> above the checklist
```

## Why these boundaries

- **Listing draft endpoint separate from listing CRUD.** The existing `AppStoreListing` model has CRUD paths. Draft is an LLM call that returns candidate text but doesn't persist — user accepts or edits. Own controller, own handler.
- **Reuse existing `/v1/storage/credentials/*` presign endpoints.** They already upload to `user_api_keys` bucket, return storage IDs. No need for new upload endpoints.
- **Phone preview as a single component** for both stores. One file; store variant toggled via prop. HTML/CSS only — the design spec describes this exact pattern.

---

## Task 1: Listing-draft request + response types

**Files:**
- Create: `internal/requests/listing_draft.go`
- Create: `internal/responses/listing_draft.go`

- [ ] **Step 1: Requests**

`internal/requests/listing_draft.go`:

```go
package requests

// DraftListingRequest asks the backend to generate candidate title +
// description + release-notes copy for an app's store listing. The
// Pilot calls this when the user clicks "Draft with AI" on a listing
// field; the user can accept, regenerate, or edit before submission.
type DraftListingRequest struct {
	// Target store — affects tone + length: App Store tends to want
	// richer copy, Play Store accepts plainer text.
	Target string `json:"target"` // "app_store" | "play_store"

	// Kind scopes the generation to a single artefact at a time so
	// users can regenerate one field without disturbing others.
	Kind string `json:"kind"` // "title" | "description" | "release_notes"

	// Optional hints the user may provide to steer the draft.
	Hint string `json:"hint,omitempty"`
}
```

- [ ] **Step 2: Responses**

`internal/responses/listing_draft.go`:

```go
package responses

// ListingDraftResponse carries a single drafted field. The Pilot
// calls the endpoint once per field (title / description /
// release_notes) and substitutes the result into the in-progress form.
type ListingDraftResponse struct {
	Kind  string `json:"kind"`  // echo of the request
	Value string `json:"value"` // the drafted copy
}
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/requests/listing_draft.go internal/responses/listing_draft.go
git commit -m "feat(ship): define listing-draft request + response types

Single draft-per-field shape lets the Pilot regenerate title,
description, or release notes in isolation without disturbing the
other form fields."
```

---

## Task 2: Listing-draft prompt constants + controller

**Files:**
- Create: `internal/constants/listing_draft_prompts.go`
- Create: `internal/controllers/listing_draft_controller.go`

- [ ] **Step 1: Prompts**

`internal/constants/listing_draft_prompts.go`:

```go
package constants

// LISTING_DRAFT_PROMPT is the system prompt used by
// Controller.DraftListing. Returns a tightly-constrained JSON object
// so the HTTP handler can forward the result unchanged.
//
// The prompt is intentionally provider-agnostic and small so it stays
// under cost thresholds on Claude Haiku 4.5 or similar low-tier
// models.
const LISTING_DRAFT_PROMPT = `You draft store-listing copy for a mobile app.

Inputs (as a JSON user message):
- app_name: the app's name
- app_description: 1-3 sentence summary of what the app does
- target: "app_store" or "play_store"
- kind: "title" | "description" | "release_notes"
- hint: optional string the user typed to steer the output

Output: a single JSON object { "kind": "<same as input>", "value": "<drafted copy>" }.

Constraints by kind:
- title: 2-30 chars. Punchy, reflects the app's core job. No taglines, no "beta", no emoji.
- description: 170-600 chars. Plain prose. Opens with the job the app does. Avoids hype.
- release_notes: 30-400 chars. Bullet list (leading "- " per line). Each bullet describes one change, in user-visible terms.

Hard rules:
- Respect the target store's tone (App Store favours polished prose; Play Store accepts plainer text).
- Never include URLs.
- Never mention "AI", "LLM", or the model.
- Output valid JSON only — no prose, no code fences.`
```

- [ ] **Step 2: Controller**

`internal/controllers/listing_draft_controller.go`:

```go
package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// DraftListing generates a single drafted field (title / description /
// release_notes) for an app's store listing. Uses the existing
// GetJSONFromLLM helper with the same token-tracking + billing pattern
// as NavBarAutoFillWithAI.
func (c *Controller) DraftListing(ctx context.Context, user *models.User, appID uuid.UUID, req requests.DraftListingRequest) (*responses.ListingDraftResponse, error) {
	if user == nil {
		return nil, errors.New("user required")
	}

	// Validate the kind / target.
	kind := strings.TrimSpace(req.Kind)
	target := strings.TrimSpace(req.Target)
	if kind != "title" && kind != "description" && kind != "release_notes" {
		return nil, fmt.Errorf("invalid kind %q", kind)
	}
	if target != "app_store" && target != "play_store" {
		return nil, fmt.Errorf("invalid target %q", target)
	}

	// Load the app for name + description.
	var project models.AppProject
	if err := c.DB.DB.NewSelect().
		Model(&project).
		Where("id = ?", appID).
		Scan(ctx); err != nil {
		return nil, err
	}
	if project.UserID != user.ID {
		return nil, errors.New("not authorized")
	}

	// Compose the user message as a compact JSON payload.
	userPayload := map[string]string{
		"app_name":        project.Name,
		"app_description": strings.TrimSpace(project.Prompt),
		"target":          target,
		"kind":            kind,
		"hint":            strings.TrimSpace(req.Hint),
	}
	userJSON, _ := json.Marshal(userPayload)

	// Token tracking — same pattern as NavBarAutoFillWithAI.
	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	ctx = utils.WithTokenTracker(ctx, tracker)

	raw, err := utils.GetJSONFromLLM(
		ctx,
		"claude-haiku-4-5",
		constants.LISTING_DRAFT_PROMPT,
		string(userJSON),
		c.AnthropicProvider(),
	)
	if err != nil {
		return nil, fmt.Errorf("listing draft LLM call failed: %w", err)
	}

	var out responses.ListingDraftResponse
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("listing draft LLM returned invalid JSON: %w", err)
	}
	if out.Kind == "" {
		out.Kind = kind
	}

	// Record token usage + deduct credits so draft generation contributes
	// to the same post-hoc billing ledger as other AI features.
	c.RecordTokenUsage(user, "listing_draft",
		"claude-haiku-4-5",
		"anthropic", "", "standard", tracker)
	prompt, completion, total, _ := tracker.Totals()
	if total > 0 {
		credits := models.TokensToCredits(prompt, completion)
		if err := c.DeductAICreditsUnchecked(user, credits, "listing_draft", ""); err != nil {
			// non-fatal — we don't block delivery on a billing write
			fmt.Printf("listing_draft: deduct credits failed: %v\n", err)
		}
	}

	return &out, nil
}
```

**Before writing:** verify imports + helper names by inspecting `internal/views/auto_fill.go` lines 80-98 for the exact pattern (`NewTokenTracker`, `WithTokenTracker`, `GetJSONFromLLM`, `RecordTokenUsage`, `DeductAICreditsUnchecked`, `TokensToCredits`, `AnthropicProvider`). Adapt any name that's different. If `c.Controller` exposes these as methods or helpers, use exactly the names present in `NavBarAutoFillWithAI`'s source.

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/constants/listing_draft_prompts.go internal/controllers/listing_draft_controller.go
git commit -m "feat(ship): add DraftListing controller + LLM system prompt

Delegates to utils.GetJSONFromLLM with Claude Haiku, same token-
tracking + credit-deduction pattern as NavBarAutoFillWithAI. Prompt
constrains output to a single { kind, value } JSON object so the
handler forwards it unchanged."
```

---

## Task 3: HTTP handler + route

**Files:**
- Create: `internal/views/listing_draft_views.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Handler**

`internal/views/listing_draft_views.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// DraftListing handles POST /v1/apps/{appId}/listing/draft.
func (v *HttpViews) DraftListing(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	appIDStr := chi.URLParam(r, "appId")
	appID, err := uuid.Parse(appIDStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid app_id", http.StatusBadRequest)
		return
	}

	var req requests.DraftListingRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := v.Controller.DraftListing(r.Context(), user, appID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
```

Match helper names to the actual repo conventions (`requireActiveSubscription`, `checkMinimumCredits` appear in `auto_fill.go` as package-level functions — use them identically).

- [ ] **Step 2: Register the route**

In `cmd/main.go`, find the existing `/apps/{appId}/launchpad` registration (from Phase 2a). Add a sibling route inside the same `/apps/{appId}` subtree:

```go
r.Post("/listing/draft", httpViews.DraftListing)
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/views/listing_draft_views.go cmd/main.go
git commit -m "feat(ship): wire POST /v1/apps/:appId/listing/draft route

Auth-gated, subscription + minimum-credits gate (same pattern as
NavBarAutoFillWithAI), delegates to Controller.DraftListing, returns
the ListingDraftResponse as 200 JSON."
```

---

## Task 4: Frontend — draft API method + shared types

**Files:**
- Modify: `src/lib/ship/launchpad-types.ts`
- Modify: `src/lib/api.ts`

- [ ] **Step 1: Types**

Append to `src/lib/ship/launchpad-types.ts`:

```ts
export type ListingDraftKind = "title" | "description" | "release_notes";
export type ListingDraftTarget = "app_store" | "play_store";

export interface DraftListingRequest {
  target: ListingDraftTarget;
  kind: ListingDraftKind;
  hint?: string;
}

export interface ListingDraftResponse {
  kind: ListingDraftKind;
  value: string;
}
```

- [ ] **Step 2: API client method**

Add after `validateGooglePlayConnection` in `src/lib/api.ts`:

```ts
async draftListing(
  appId: string,
  req: import("./ship/launchpad-types").DraftListingRequest,
): Promise<import("./ship/launchpad-types").ListingDraftResponse> {
  return this.request<import("./ship/launchpad-types").ListingDraftResponse>(
    `/v1/apps/${appId}/listing/draft`,
    { method: "POST", body: JSON.stringify(req) },
  );
}
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/lib/ship/launchpad-types.ts src/lib/api.ts
git commit -m "feat(ship): add draftListing API method + ListingDraft types

Calls POST /v1/apps/:appId/listing/draft to get AI-drafted copy for
title, description, or release notes. Used by StepDescribe's 'Draft
with AI' buttons in Task 7."
```

---

## Task 5: Frontend — `FileUpload` component

**Files:**
- Create: `src/components/ship/pilot/file-upload.tsx`

- [ ] **Step 1: Write the component**

```tsx
"use client";

import { useRef, useState } from "react";
import { Upload, FileCheck2, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api";
import { cn } from "@/lib/utils";

type PresignFn = () => Promise<{
  id: string;
  presigned_request: { URL: string; [key: string]: unknown };
}>;

interface FileUploadProps {
  label: string;
  accept: string;          // e.g. ".p8" or "application/json,.json"
  hint?: string;
  presignFn: PresignFn;    // apiClient.getAppleCredentialPresignedUrl | getGoogleCredentialPresignedUrl
  onUploaded: (storageId: string, fileName: string) => void;
}

/**
 * File picker that runs the existing presign → PUT flow and calls
 * onUploaded with the storage id. Extracted to one place so the Pilot's
 * Connection step + any future upload surface share the same UX.
 */
export function FileUpload({ label, accept, hint, presignFn, onUploaded }: FileUploadProps) {
  const [busy, setBusy] = useState(false);
  const [fileName, setFileName] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  async function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = "";
    setFileName(file.name);
    setDone(false);
    setBusy(true);
    try {
      const presigned = await presignFn();
      await apiClient.uploadToPresignedUrl(presigned.presigned_request, file);
      onUploaded(presigned.id, file.name);
      setDone(true);
    } catch (err) {
      toast.error("Upload failed", {
        description: err instanceof Error ? err.message : String(err),
      });
      setFileName(null);
    } finally {
      setBusy(false);
    }
  }

  return (
    <label className="flex flex-col gap-1">
      <span className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">
        {label}
      </span>
      <div
        onClick={() => inputRef.current?.click()}
        className={cn(
          "flex items-center gap-2 bg-[var(--bg)] border border-dashed rounded-md px-3 py-2 text-[12px] cursor-pointer transition-colors",
          done
            ? "border-[var(--success)]/40 text-[var(--ink)]"
            : "border-[var(--border-strong)] hover:border-[var(--accent)]",
        )}
      >
        {busy ? (
          <Loader2 size={14} className="animate-spin shrink-0" />
        ) : done ? (
          <FileCheck2 size={14} className="text-[var(--success)] shrink-0" />
        ) : (
          <Upload size={14} className="shrink-0 text-[var(--ink-muted)]" />
        )}
        <span className="truncate flex-1">
          {fileName ? fileName : hint ?? "Click to choose a file"}
        </span>
        <input
          ref={inputRef}
          type="file"
          accept={accept}
          className="hidden"
          onChange={handleFile}
        />
      </div>
    </label>
  );
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/components/ship/pilot/file-upload.tsx
git commit -m "feat(ship): add reusable FileUpload component

Wraps the existing presign → PUT flow and calls onUploaded with the
returned storage id. Used by StepConnection's p8 + service-account
slots in Task 6."
```

---

## Task 6: Frontend — swap raw IDs in `step-connection.tsx` for `<FileUpload>`

**Files:**
- Modify: `src/components/ship/pilot/step-connection.tsx`

- [ ] **Step 1: Replace the `P8 upload ID` raw input**

Inside `AppleConnectionValidator`, remove the plain text `<Input label="P8 upload ID..." />`. Replace with:

```tsx
<FileUpload
  label="App Store Connect API key (.p8)"
  accept=".p8"
  hint="Upload the .p8 file you downloaded from Apple"
  presignFn={apiClient.getAppleCredentialPresignedUrl.bind(apiClient)}
  onUploaded={(id) => setP8KeyID(id)}
/>
```

Keep the `p8KeyID` state — `FileUpload` writes into it via `onUploaded`.

Add the import at the top:

```tsx
import { FileUpload } from "./file-upload";
```

- [ ] **Step 2: Replace the service-account input in `GoogleConnectionValidator`**

Remove the `<Input label="Service Account upload ID..." />`. Replace with:

```tsx
<FileUpload
  label="Play Console service account (.json)"
  accept="application/json,.json"
  hint="Upload the service-account JSON from Google Cloud"
  presignFn={apiClient.getGoogleCredentialPresignedUrl.bind(apiClient)}
  onUploaded={(id) => setSaID(id)}
/>
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/pilot/step-connection.tsx
git commit -m "feat(ship): swap raw storage-id inputs for FileUpload in Pilot

Phase 3a left the .p8 + service-account fields as raw text inputs
expecting an S3 storage id — no user would ever figure that out. 3b
replaces them with FileUpload, which runs presign → PUT through the
existing /v1/storage/credentials endpoints and populates the storage
id transparently."
```

---

## Task 7: Frontend — AI draft buttons in `StepDescribe`

**Files:**
- Modify: `src/components/ship/pilot/step-describe.tsx`

- [ ] **Step 1: Update the component**

Replace the contents with:

```tsx
"use client";

import { useState } from "react";
import { Sparkles, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api";
import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import type { PilotState } from "./pilot-state";

interface StepDescribeProps {
  state: PilotState;
  data: LaunchpadResponse;
  onPatch: (p: Partial<PilotState>) => void;
}

export function StepDescribe({ state, data, onPatch }: StepDescribeProps) {
  const [draftingTitle, setDraftingTitle] = useState(false);
  const [draftingDesc, setDraftingDesc] = useState(false);

  async function draft(kind: "title" | "description") {
    const target = state.target;
    const setBusy = kind === "title" ? setDraftingTitle : setDraftingDesc;
    setBusy(true);
    try {
      const r = await apiClient.draftListing(data.app.id, { target, kind });
      if (kind === "title") onPatch({ title: r.value });
      else onPatch({ description: r.value });
    } catch (err) {
      toast.error("Draft failed", {
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="px-5 py-5">
      <h2 className="font-serif text-[20px] mb-2">Describe your app</h2>
      <p className="text-[12px] text-[var(--ink-muted)] mb-4">
        The listing the store shows to users. AI drafts use your project name and description.
      </p>

      <div className="flex flex-col gap-1 mb-4">
        <div className="flex items-center justify-between">
          <span className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">
            Title (2–30)
          </span>
          <button
            type="button"
            onClick={() => draft("title")}
            disabled={draftingTitle}
            className="text-[10px] px-2 py-0.5 rounded-md bg-[var(--accent-soft)] text-[var(--accent)] hover:bg-[var(--accent)] hover:text-white disabled:opacity-50 inline-flex items-center gap-1"
          >
            {draftingTitle ? <Loader2 size={10} className="animate-spin" /> : <Sparkles size={10} />}
            {draftingTitle ? "Drafting…" : state.title ? "Regenerate" : "Draft with AI"}
          </button>
        </div>
        <input
          type="text"
          value={state.title}
          onChange={(e) => onPatch({ title: e.target.value })}
          className="bg-[var(--bg)] border border-[var(--border)] rounded-md px-3 py-2 text-[13px]"
          placeholder="TaskTracker Pro"
        />
        <span className="text-[10px] text-[var(--ink-faint)] mt-0.5">{state.title.length} chars</span>
      </div>

      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between">
          <span className="text-[10px] uppercase tracking-[0.5px] text-[var(--ink-faint)]">
            Description
          </span>
          <button
            type="button"
            onClick={() => draft("description")}
            disabled={draftingDesc}
            className="text-[10px] px-2 py-0.5 rounded-md bg-[var(--accent-soft)] text-[var(--accent)] hover:bg-[var(--accent)] hover:text-white disabled:opacity-50 inline-flex items-center gap-1"
          >
            {draftingDesc ? <Loader2 size={10} className="animate-spin" /> : <Sparkles size={10} />}
            {draftingDesc ? "Drafting…" : state.description ? "Regenerate" : "Draft with AI"}
          </button>
        </div>
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
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Update the Pilot to pass `data` to this step**

Open `src/components/ship/pilot/pilot.tsx`. The current signature for `StepDescribe` takes `state` and `onPatch`; add `data`:

```tsx
{state.currentStep === 3 && <StepDescribe state={state} data={data} onPatch={onPatch} />}
```

- [ ] **Step 3: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/pilot/step-describe.tsx src/components/ship/pilot/pilot.tsx
git commit -m "feat(ship): add 'Draft with AI' buttons to StepDescribe

Title + description each get a button that calls
POST /v1/apps/:appId/listing/draft and populates the form field. The
button changes label from 'Draft with AI' to 'Regenerate' once the
field has content. Release notes draft stays manual for 3b (used only
on subsequent builds — Phase 4 Quick Ship wires that path)."
```

---

## Task 8: Frontend — `ListingPreview` component

**Files:**
- Create: `src/components/ship/pilot/listing-preview.tsx`

- [ ] **Step 1: Write the component**

```tsx
"use client";

import { useState } from "react";
import type { LaunchpadResponse } from "@/lib/ship/launchpad-types";
import type { PilotState } from "./pilot-state";
import { cn } from "@/lib/utils";

interface ListingPreviewProps {
  state: PilotState;
  data: LaunchpadResponse;
}

/**
 * HTML/CSS preview of the store listing card as it'll appear in the
 * Play Store or App Store. Pure client — no round-trips. Toggle shows
 * the opposite store so the user confirms the listing works in both.
 */
export function ListingPreview({ state, data }: ListingPreviewProps) {
  const [variant, setVariant] = useState<"play_store" | "app_store">(state.target);

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <div className="text-[10px] uppercase tracking-[1px] text-[var(--ink-faint)]">
          Preview
        </div>
        <div className="flex bg-[var(--bg-raised)] rounded-full p-0.5 text-[10px]">
          <button
            type="button"
            onClick={() => setVariant("play_store")}
            className={cn(
              "px-2.5 py-0.5 rounded-full",
              variant === "play_store"
                ? "bg-[var(--bg-surface)] text-[var(--ink)] shadow-sm"
                : "text-[var(--ink-muted)]",
            )}
          >
            Play Store
          </button>
          <button
            type="button"
            onClick={() => setVariant("app_store")}
            className={cn(
              "px-2.5 py-0.5 rounded-full",
              variant === "app_store"
                ? "bg-[var(--bg-surface)] text-[var(--ink)] shadow-sm"
                : "text-[var(--ink-muted)]",
            )}
          >
            App Store
          </button>
        </div>
      </div>

      <div className="rounded-2xl border border-[var(--border)] bg-[var(--bg-surface)] overflow-hidden">
        <div className="h-4 bg-[var(--bg-raised)] border-b border-[var(--border)] flex items-center justify-center text-[9px] text-[var(--ink-faint)] tracking-wider">
          9:41 ●●●
        </div>
        {variant === "app_store" ? (
          <AppStoreCard state={state} data={data} />
        ) : (
          <PlayStoreCard state={state} data={data} />
        )}
      </div>
    </div>
  );
}

function AppStoreCard({ state, data }: ListingPreviewProps) {
  const initial = (state.title || data.app.name || "?").charAt(0).toUpperCase();
  return (
    <div className="p-4">
      <div className="flex items-start gap-3 mb-3">
        <div className="w-[62px] h-[62px] rounded-[14px] bg-[var(--accent)] text-white flex items-center justify-center font-serif text-[26px]">
          {initial}
        </div>
        <div className="flex-1 min-w-0">
          <div className="font-semibold text-[13px] leading-tight truncate">
            {state.title || "Untitled"}
          </div>
          <div className="text-[10px] text-[var(--ink-muted)] truncate">
            {data.listing.tiles.find((t) => t.key === "category")?.summary ?? "Productivity"}
          </div>
        </div>
      </div>
      <div className="flex gap-1.5 mb-3">
        <div className="flex-1 py-1.5 bg-[#007aff] text-white rounded-md text-[10px] text-center font-semibold">
          GET
        </div>
        <div className="w-7 h-7 rounded-md bg-[var(--bg-raised)] flex items-center justify-center text-[10px]">
          ↑
        </div>
      </div>
      <div className="text-[9px] font-semibold text-[var(--ink-faint)] mb-1">PREVIEW</div>
      <div className="flex gap-1 mb-3 overflow-hidden">
        {[1, 2, 3].map((n) => (
          <div
            key={n}
            className="w-[46px] h-[88px] bg-[var(--bg-raised)] rounded border border-[var(--border)]"
          />
        ))}
      </div>
      <div className="text-[10px] text-[var(--ink)] leading-[1.5] line-clamp-4">
        {state.description || "A short description of what your app does will appear here once drafted."}
      </div>
    </div>
  );
}

function PlayStoreCard({ state, data }: ListingPreviewProps) {
  const initial = (state.title || data.app.name || "?").charAt(0).toUpperCase();
  return (
    <div className="p-4">
      <div className="flex items-start gap-3 mb-2.5">
        <div className="w-[58px] h-[58px] rounded-[12px] bg-[var(--accent)] text-white flex items-center justify-center font-serif text-[24px]">
          {initial}
        </div>
        <div className="flex-1 min-w-0">
          <div className="font-medium text-[13px] leading-tight truncate">
            {state.title || "Untitled"}
          </div>
          <div className="text-[10px] text-[#00875f] mt-0.5">
            {data.listing.tiles.find((t) => t.key === "category")?.summary ?? "Productivity"}
          </div>
          <div className="text-[10px] text-[var(--ink-faint)] mt-0.5">Free · Contains ads</div>
        </div>
      </div>
      <div className="mb-3">
        <div className="w-full py-2 bg-[#00875f] text-white rounded-md text-[11px] text-center font-medium">
          Install
        </div>
      </div>
      <div className="text-[9px] font-semibold text-[var(--ink-faint)] mb-1">ABOUT THIS APP</div>
      <div className="text-[10px] text-[var(--ink)] leading-[1.5] line-clamp-5">
        {state.description || "A short description of what your app does will appear here once drafted."}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run build
git add src/components/ship/pilot/listing-preview.tsx
git commit -m "feat(ship): add ListingPreview phone-frame component

Pure HTML/CSS rendering of the Play Store / App Store card the user's
listing will produce. Toggles between the two stores so the user
confirms both variants before submitting. Hosted by the Pilot's Review
step in Task 9."
```

---

## Task 9: Frontend — host `ListingPreview` on `StepReview`

**Files:**
- Modify: `src/components/ship/pilot/step-review.tsx`

- [ ] **Step 1: Add the preview above the checklist**

At the top of the returned JSX in `StepReview`:

```tsx
import { ListingPreview } from "./listing-preview";

// ...inside the return, right after the <p> subtitle:
<div className="mb-5">
  <ListingPreview state={state} data={data} />
</div>
```

Keep the existing checklist + submit button intact.

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-app
npm run lint
npm run build
git add src/components/ship/pilot/step-review.tsx
git commit -m "feat(ship): host ListingPreview on StepReview

Phone-frame preview renders above the pre-submit checklist so users
see the exact Play/App Store card their listing produces before
submitting. Toggle flips between stores; defaults to the store the
user is shipping to."
```

---

## Task 10: End-to-end smoke

- [ ] **Step 1: Documented for the user**

```
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go &

cd /Users/knid/Projects/wemob/wemob-app && npm run dev
# Browser: http://localhost:3000/apps/<appId>/ship → click a store tile.
```

Expected:
1. Open the Pilot by clicking a store tile's primary action.
2. Step 1 (Connection): pick DIY → file uploads show for .p8 and .json. Upload a test file → storage id populates automatically → Validate button → live result.
3. Step 3 (Describe): Title field shows "Draft with AI" → click → 2-3 seconds → title populates. Same for description. Regenerate changes text.
4. Step 5 (Review): phone-frame preview renders above the checklist. Toggle Play Store / App Store. Edit title back in Step 3 → preview updates on revisit.
5. Submit → POST /v1/deployments → Launchpad refetches.

- [ ] **Step 2: No commit — final hand-off.**

---

## Phase 3b completion criteria

- Backend + frontend build/vet/lint clean.
- `POST /v1/apps/:appId/listing/draft` returns JSON { kind, value } for valid kind / target combos; returns 400 on invalid input; subscription + credit gates enforced.
- File uploads work for .p8 and service-account JSON with the existing presign endpoints.
- `StepDescribe` regenerates title + description on demand.
- `StepReview` shows the phone-frame preview with Play/App Store toggle.
- No regressions in Phase 2a/2b/3a flows.

Phase 3c (fastlane `produce` + `deliver --download-metadata`) unblocked.

## Out of scope (3c + beyond)

- `fastlane produce` / `supply --create` for first-submission App Store Connect / Play Console app record creation.
- `deliver --download-metadata` for existing-app listing import ("We found TaskTracker Pro in your account — import the listing?").
- Screenshot upload + AI generation UI (the existing `AutoGenerateScreenshots` helper will wire into `StepShow` in 3c).
- ⌘K, /ship table, Quick Ship, per-locale listings, org-level connections.
