# Ship UX — Phase 3c · First-Submission Auto-Create Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the manual "create the app in App Store Connect / Play Console before first submission" step. Wire `fastlane produce` into the iOS deploy workflow and `supply --create` into the Android deploy workflow, gated by the existing `CM_NEW_APP=true` input. Seed a build-annotation entry so first-submission produce failures render as actionable messages. Metadata-download-and-import (`deliver --download-metadata` / `supply --download`) is deferred to a future phase.

**Architecture:** Pure codemagic.yaml edits in `wemob-flutter-template`, plus one annotation seed in the backend. No new backend endpoints, no new frontend surfaces. The existing `CM_NEW_APP` input (already passed by the backend when creating a deployment for a project that has no prior submission) gates both new behaviors.

**Tech stack:** codemagic.yaml, fastlane (`produce`, `supply --create`), Go (seed addition).

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` (fastlane integration).

---

## Test strategy

- Backend: `go build` + `go vet` clean after the annotation seed addition.
- Flutter-template: `yaml.safe_load` the modified codemagic.yaml to verify it's still parseable.
- Runtime: deferred — first-submission is a live-only test (no way to simulate in sandbox).

## File structure overview

### Flutter-template (wemob-flutter-template) — modified

```
codemagic.yaml  — ios-deploy + android-deploy get produce / supply --create steps
```

### Backend (wemob-backend) — modified

```
internal/database/annotations_seed.go  — +1 entry: produce_first_submission_failed
```

---

## Task 1: Add `fastlane produce` to `ios-deploy`

**Why:** When `CM_NEW_APP=true`, create the App Store Connect app record before calling `deliver`. Removes a hard blocker from cold-start iOS.

**Files:**
- Modify: `/Users/knid/Projects/wemob/wemob-flutter-template/codemagic.yaml`

- [ ] **Step 1: Read the current `ios-deploy` workflow**

Locate the `ios-deploy:` workflow. Find the "Create Fastfile + deploy" script (around line 640 in the `feat/ship-ux-phase-1` branch). Its Fastfile generator currently declares:

```ruby
default_platform(:ios)
platform :ios do
  lane :deploy do
    api_key = app_store_connect_api_key(...)
    precheck(...)
    # optional deliver(...) for binary upload
    deliver(...)  # metadata only
  end
end
```

- [ ] **Step 2: Insert `produce` before `precheck`**

Wrap the insertion so it only runs when `CM_NEW_APP=true` — this is a bash env variable consumed by the Fastfile. In Ruby:

```ruby
if ENV['CM_NEW_APP'] == 'true'
  produce(
    app_identifier: ENV['CM_PACKAGE_NAME'],
    app_name: ENV['CM_TITLE'],
    language: ENV['CM_LOCALE'] || 'en-US',
    app_version: ENV['CM_SEMVER'] || '1.0.0',
    team_id: ENV['CM_TEAM_ID'],
    sku: ENV['CM_PACKAGE_NAME'],
    skip_itc: false,
    skip_devcenter: true,
    enabled_features: {},
  )
end
```

Place it after the `api_key = app_store_connect_api_key(...)` block and before `precheck`. The lane must keep working when `CM_NEW_APP` is anything other than `"true"` — `produce` is skipped entirely for subsequent submissions.

Note: `produce` does NOT require the `api_key` variable — it picks up credentials from `APP_STORE_CONNECT_API_KEY_ID` / `APP_STORE_CONNECT_API_KEY_ISSUER_ID` / `APP_STORE_CONNECT_API_KEY_KEY` environment variables, which are already set by the existing workflow's earlier steps. Verify this is true by inspecting the script that precedes Fastfile generation; if it's not, explicitly pass `api_key: api_key` to `produce` the same way `deliver` does.

- [ ] **Step 3: Extend the "Report result" script to tag produce failures**

In the final `Report result` publishing script, the existing grep-for-precheck logic already maps a failed fastlane run to either `stage="precheck"` or `stage="deliver"`. Add a produce check in the same chain (place BEFORE the precheck check — `produce` runs first):

```bash
if [ -f ~/SUCCESS ]; then
  S="Deployed"; M="iOS deployed"; STAGE=""
else
  S="error"
  if grep -q "produce" ~/deploy.log 2>/dev/null && grep -q "^\s*\[produce\]" ~/deploy.log 2>/dev/null; then
    STAGE="produce"
    M="App Store Connect app record creation failed"
  elif grep -q "fastlane_version: 2" ~/deploy.log 2>/dev/null && grep -q "precheck" ~/deploy.log 2>/dev/null; then
    STAGE="precheck"
    M="App Store metadata precheck failed"
  else
    STAGE="deliver"
    M="iOS deploy failed"
  fi
fi
```

Match the real indentation + surrounding structure of the existing script.

- [ ] **Step 4: Validate YAML + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-flutter-template
ruby -e "require 'yaml'; YAML.load_file('codemagic.yaml'); puts 'ok'"
git add codemagic.yaml
git commit -m "feat(ci): fastlane produce creates App Store Connect record on first submit

When CM_NEW_APP=true, the ios-deploy lane runs produce before precheck
to register the app in App Store Connect. Subsequent submissions skip
produce (the record already exists).

Removes the 'create app in App Store Connect first' manual step from
cold-start iOS publishing. Paired with the backend annotation
produce_first_submission_failed so failures render as actionable
messages."
```

---

## Task 2: Add `supply --create` to `android-deploy`

**Why:** Mirror Task 1 for Android. Play Console app records must exist before `supply` can publish to a track; when `CM_NEW_APP=true`, create the record first.

**Files:**
- Modify: `/Users/knid/Projects/wemob/wemob-flutter-template/codemagic.yaml`

- [ ] **Step 1: Read the current `android-deploy` workflow**

Locate the `android-deploy:` workflow. Its Fastfile generator calls:

```ruby
default_platform(:android)
platform :android do
  lane :deploy do
    supply(
      json_key: "/tmp/service_account.json",
      package_name: "$CM_PACKAGE_NAME",
      aab: "/tmp/release.aab",
      track: "production",
      ...
    )
  end
end
```

- [ ] **Step 2: Branch the call based on `CM_NEW_APP`**

Google Play's canonical `supply --create` requires a separate first-time upload to the Play Console before the API can take over; Google's limitation, not fastlane's. But `supply` itself accepts `track: "internal"` on first submission, which Google does accept via the API (they loosened this in 2023 for internal testing track only).

Simplest + most reliable: when `CM_NEW_APP=true`, force `track: "internal"` for the first submission and surface a one-time "now promote from Internal → Production in Play Console" hint in the report. Subsequent submissions use the existing `track` value.

Modify the Fastfile generator so the track is conditional:

```ruby
default_platform(:android)
platform :android do
  lane :deploy do
    effective_track = ENV['CM_NEW_APP'] == 'true' ? 'internal' : 'production'
    supply(
      json_key: "/tmp/service_account.json",
      package_name: "$CM_PACKAGE_NAME",
      aab: "/tmp/release.aab",
      track: effective_track,
      version_code: $CM_VERSION,
      skip_upload_metadata: $SKIP_META,
      skip_upload_images: $SKIP_IMAGES,
      skip_upload_screenshots: $SKIP_IMAGES,
      metadata_path: "fastlane/metadata/android",
    )
  end
end
```

**Before writing this, inspect the actual existing lane** — the plan above assumes the structure seen in my earlier Phase 1 work; adapt to what's in the current yaml (the `$CM_VERSION` / `$SKIP_META` / `$SKIP_IMAGES` references are bash-interpolated into the heredoc, so preserve the `$` prefix where needed).

- [ ] **Step 3: Add a note to the deploy report**

In the `Report result` publishing script for android-deploy, when `CM_NEW_APP=true` and success, append a message noting the manual Play Console promotion step:

```bash
if [ -f ~/SUCCESS ]; then
  if [ "$CM_NEW_APP" = "true" ]; then
    S="Deployed"
    M="First Android submission published to Internal track. Promote to Production in Play Console when ready."
  else
    S="Deployed"
    M="Android deployed"
  fi
else
  # ... existing failure branch
fi
```

Keep the existing failure branch unchanged.

- [ ] **Step 4: Validate YAML + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-flutter-template
ruby -e "require 'yaml'; YAML.load_file('codemagic.yaml'); puts 'ok'"
git add codemagic.yaml
git commit -m "feat(ci): first-submission Android publishes to Internal track via supply

Google Play's API rejects first-submissions to Production — the app
record has to exist with a prior Play-Console-hosted upload. When
CM_NEW_APP=true, supply publishes to Internal (which Google accepts)
and the deploy report tells the user to manually promote
Internal → Production from Play Console.

Subsequent submissions restore the Production track and there's no
manual step. Matches the fastlane produce story from Task 1 on iOS
(which does not have this restriction)."
```

---

## Task 3: Seed `produce_first_submission_failed` annotation

**Why:** When `produce` or `supply --create` fails on a first submission, the backend already matches the deploy error_message against `build_annotations`. Add a targeted annotation so users see "Couldn't create the App Store Connect record — check your team ID and bundle ID availability" instead of a raw log.

**Files:**
- Modify: `/Users/knid/Projects/wemob/wemob-backend/internal/database/annotations_seed.go`

- [ ] **Step 1: Extend the seeds slice**

Add a 7th entry to the `seeds` slice in `SeedBuildAnnotations`:

```go
{
    Name:          "produce_first_submission_failed",
    LogRegex:      `(?i)(fastlane produce|App Store Connect app record creation failed|bundle identifier .* is already taken|Google returned|supply .* track: 'internal')`,
    HumanMessage:  "Couldn't create the store record for this app.",
    SuggestedFix:  "Check two things: (1) the bundle ID isn't already used by another developer, (2) your Apple Team ID / Play Console service account has permission to create apps. iOS: produce runs on first submission. Android: first submissions publish to the Internal track — promote to Production in Play Console when ready.",
    RetryStrategy: "",
    Enabled:       true,
},
```

- [ ] **Step 2: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/database/annotations_seed.go
git commit -m "feat(builds): seed produce_first_submission_failed annotation

Covers fastlane produce (iOS first-submission) and supply --create
(Android first-submission) failures. Most common cause is a bundle
ID collision or insufficient account permissions — the annotation
surfaces both paths inline instead of requiring users to read the
fastlane log."
```

- [ ] **Step 3: Insert the row into the running dev DB via Postgres MCP**

Since the sandbox can't restart the backend, insert the 7th row directly:

```sql
INSERT INTO build_annotations (name, log_regex, human_message, suggested_fix, retry_strategy, enabled)
VALUES (
    'produce_first_submission_failed',
    '(?i)(fastlane produce|App Store Connect app record creation failed|bundle identifier .* is already taken|Google returned|supply .* track: ''internal'')',
    'Couldn''t create the store record for this app.',
    'Check two things: (1) the bundle ID isn''t already used by another developer, (2) your Apple Team ID / Play Console service account has permission to create apps. iOS: produce runs on first submission. Android: first submissions publish to the Internal track — promote to Production in Play Console when ready.',
    '',
    true
)
ON CONFLICT (name) DO NOTHING;
```

Then verify 7 rows:

```sql
SELECT count(*) FROM build_annotations WHERE enabled;
```

---

## Task 4: End-to-end smoke (documented for user)

First-submission is a live-only test; the sandbox can't exercise it. Smoke steps for the user:

1. Pick an AppProject that has no prior deployments — `SELECT * FROM deployments WHERE project_id = 'v1-id' LIMIT 1;` returns nothing.
2. In the Ship tab, Start setup for App Store (or Play Store) → Pilot → DIY → valid creds → submit.
3. Expected:
   - **iOS:** first submission — `produce` creates the App Store Connect record, `deliver` uploads the binary + metadata, deployment goes to `Submitted` status. No "create app in App Store Connect first" manual step required.
   - **Android:** first submission — `supply` publishes to the **Internal** track (Google API limitation). Deploy report includes "Promote to Production in Play Console when ready." message. User clicks through Play Console to promote.
4. Second submission for the same project: `CM_NEW_APP=false` (backend already sends this for projects with at least one prior deployment) → produce is skipped on iOS; Android track reverts to Production.

---

## Phase 3c completion criteria

- `codemagic.yaml` still parses (`ruby -e "require 'yaml'; YAML.load_file('codemagic.yaml')"`).
- `go build ./...` + `go vet ./...` clean.
- `build_annotations` has 7 enabled rows on dev DB after seed/insert.
- First-submission iOS deploys include a `produce` call gated on `CM_NEW_APP=true`.
- First-submission Android deploys target the Internal track; subsequent submissions target Production.

## Out of scope (3d + beyond)

- `deliver --download-metadata` / `supply --download` for existing-app listing import ("We found TaskTracker Pro in your App Store Connect — import the listing?"). Needs a new Codemagic workflow + report endpoint + Pilot UI; own plan.
- Automated Play Console Internal → Production promotion (currently requires a human click).
- Any Phase 4 (Quick Ship, ⌘K, /ship table) or Phase 5 (managed observability) work.
