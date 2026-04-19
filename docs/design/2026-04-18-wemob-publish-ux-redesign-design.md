# Wemob Publish UX Redesign — Design Spec

**Date:** 2026-04-18
**Status:** Approved for implementation planning
**Owner:** @knid

## Goal

Rebuild the end-to-end publish lifecycle for Wemob (AI Flutter-app builder) so that a non-technical user can go from "my app is done" to "live on the App Store / Play Store" without prior knowledge of certificates, bundle IDs, keystores, or wizards. Power-user affordances ride on top; they never compromise the simple path.

Current "`feat/mobile-publish-ux-overhaul`" shipped a good foundation (`PublishMissionControl`, durable `AppStoreListing`, Codemagic-backed builds with stage reporting). This redesign replaces the user-facing surface while preserving the backend infrastructure we just built.

## Target user

- **Primary:** non-technical founder / solopreneur. Does not know what a `.p8` is. May not yet have an Apple Developer or Google Play account. Wants their app in the store with minimum ceremony.
- **Secondary:** power user / indie developer. Comfortable with the concepts. Wants keyboard shortcuts, cross-app visibility, direct actions, and no explainers.

Primary drives every default. Secondary gets overlay affordances (⌘K, shortcuts, dense tables, collapsed explainers).

## Core shape — B + A

Two surfaces, not three or six:

- **Launchpad** (B): the always-visible home per app. Five zones: Status hero, Stores, Listing tiles, Builds feed, Identity (collapsed). Answers "what's my state, what do I do next?" at a glance.
- **Publish Pilot** (A): a slide-over from the right, first time only per-app-per-store. Five steps with AI pre-fill and inline teaching. ⌘K jumps to review.
- **Quick Ship**: a *different* component from Pilot, not a cut-down version. One-screen confirm for subsequent submissions. Target: 30 seconds, `⌘⏎` to ship.

## Information architecture

### Routes

| Old | New | Notes |
|---|---|---|
| Studio → "Deploy" tab | Studio → **"Ship"** tab | Renamed. Hosts the Launchpad. |
| `/apps/:id/publish` | `/apps/:id/ship` | Full-page Launchpad. Old path aliased for two releases. |
| `/builds` | `/ship` | Cross-app table. Power-user home. |
| `/credentials` | *deleted* | Replaced by inline connection collection in Pilot + Settings → Connections. |
| Studio → "Native" tab | *deleted* | Builds feed moves into Launchpad. |

### Why "Ship"

"Deploy" is ambiguous (web vs mobile vs stores). "Publish" is overloaded (draft, submit, review). "Ship" concretely owns the whole trajectory — binary → sign → submit → live — and is a verb the AI agent can use naturally: "I shipped v2 to Play Store."

## Launchpad — five zones, top to bottom

1. **Status hero** — computed one-sentence summary ("Live on Play Store — App Store needs 3 things") + single primary CTA. Always answers "what do I do now?"
2. **Stores** — Play Store and App Store tiles. The primary objects. Each shows health (`Live / Submitted / In review / Needs setup / Rejected / Update available`), current version, install/review counts when live, primary action button.
3. **Listing** — 4×2 tile grid for icon, title, description, screenshots, locale, category, privacy URL, release notes. Each tile shows `✓ / ⚠ / —` with inline remediation for missing items ("Generate with AI →").
4. **Builds** — feed, active builds at top with stage ladder + ETA, historical builds collapse after 5. Failed builds show annotated errors with "Retry with fix" / "Retry as-is" / "Show full log".
5. **Identity** — collapsed one-liner: `com.tasktracker.pro · 1.0.3 · 3 permissions`. Expand for advanced users. Rarely touched after first build.

### Status vocabulary per store

`not_configured` → `configuring` → `ready_to_ship` → `building` → `needs_review` → `submitted` → `in_review` → `live` → `update_available` | `rejected` | `failed`

## Publish Pilot — first-time slide-over

Opens as a right-side slide-over (~560px wide) over a subtly scrimmed Launchpad. User never loses context.

### Behavior rules

1. **AI fills everything first.** Title from project name, description from the chat transcript + repo, release notes from commit log since last build, screenshots from the deployed web preview (iPhone 6.7", iPad, Android phone frames). User confirms or edits.
2. Every field has a **"Why this matters"** accordion for non-tech users. Power users never open it.
3. **`⌘K` jumps to dense review** from any step. Power-user escape hatch; same component, different mode.
4. **Auto-save on field blur.** Closing the Pilot never loses work. Reopening resumes at the last step touched.
5. **First Play Store + First App Store are separate Pilot runs.** Second store reuses shared data with "You already wrote this for Play Store — keep it?"

### Five steps (first-time Play Store)

1. **Connect Play Console** — managed (default) or DIY (paste service account JSON, with inline help).
2. **Name your app** — title, bundle ID preview.
3. **Describe your app** — short + full description, AI-drafted, editable.
4. **Show your app** — AI-generated screenshots from web preview, or upload/edit.
5. **Review & submit** — split view: checklist + "what happens next" on left, live Play Store phone preview on right.

## Quick Ship — subsequent submissions

One screen. Designed for 30-second ship.

- "What's new in this update" — AI-drafted from commit log / chat since last build, editable.
- "Unchanged since last release" — collapsed list of title/icon/description/screenshots/privacy with `✓` markers. If user wants to change any, click to open inline editor.
- Build binary summary: v4 · 1.0.3 · AAB 28 MB · connection used.
- Rollout selector: Internal / Closed / Open / Production (backed by `fastlane pilot` for iOS, `supply track` for Android).
- `⌘⏎` to ship.

## Connections (renamed Credentials)

### Why

"Credentials" makes users think of passwords and a dedicated page. "Connection" matches the Slack/Notion/Figma mental model — integrations attached to your account, used contextually.

### Model renames

- `app_store_credentials` → `apple_connections`
- `google_play_credentials` → `google_play_connections`

### Collection point

- **Primary:** collected inline in Pilot step 1 at the moment they're needed, with a 2-minute accordion walkthrough ("How to get your App Store Connect API key").
- **Secondary:** Settings → Connections for view / rotate / revoke. Shows which apps use each connection.
- **Validate on save.** Hit App Store Connect API / Play Publishing API immediately to confirm credentials work — tell user "✓ We saw 3 apps in your account" or "✗ Team ID doesn't match this key" before they burn a 15-minute build.

### Managed vs DIY

- **Managed (default for non-tech)**: Wemob's Apple Developer + Play Console accounts submit the app. No $99 Apple fee. 12 credits per submission. App appears under "Wemob Ltd".
- **DIY**: user's own Developer accounts. 4 credits per submission. User owns the listing.

## Build & sign mechanics

### No more CreateBuildDialog

"New build" is a one-click action on the Launchpad. No dialog. App name, icon, splash color come from **project build defaults** set once. Version auto-bumps patch. Platforms default to what connections are present.

### iOS signing is automatic

New state machine:

```
queued → preparing → building → uploading → built
                                               │
                                         (iOS) ↓
                                          signing → signed
                                               │
                                    any ─────> failed
                                    any ─────> canceled
```

Auto-chain: iOS build success + `apple_connection` present → backend enqueues `sign` task automatically. No user action. Separate `/v1/sign-ipa` trigger is removed. If no Apple connection, iOS stays at `built (unsigned)` with CTA "Connect Apple to sign".

### Annotated failures

New `build_annotations` table: regex pattern → human explanation + suggested fix + auto-retry strategy. Seeded with the top 5 patterns from current support tickets (pod install failure, bundle ID mismatch, keystore corruption, Flutter version conflict, missing permission).

- **"Retry with fix"** applies the remediation (sets env vars, clears cache, pins Flutter version) then re-runs.
- **"Retry as-is"** re-runs without changes.
- **Unknown failures** show a generic template with log tail + "Report to support" button that attaches everything.
- **Outcome tracking.** Each annotated retry tracks success/failure; we promote good annotations and retire bad ones over time.

### Versioning visible

`1.0.3 (Build 12)` everywhere. Semver (`version_semver`) for store-facing version; monotonic `build_number` for internal tracking. Auto-bump defaults to patch; advanced expander lets user pick minor/major or set exact. Release notes default to AI draft from commit log since last build.

## Listing preview + managed observability

### Listing preview

Pilot's Review step is a split view:
- **Left:** pre-submit checklist (6 items, including `fastlane precheck` results) + "what happens after submit" explainer + submit button.
- **Right:** live Play Store / App Store phone frame preview. HTML/CSS, re-renders as user edits, zero round-trips. Toggle between Play Store / App Store in preview.

### Managed observability

Replace the opaque `status` string with an **event stream**:

```
deployment_events (id, deployment_id, event_type, actor_type, actor_id, metadata, occurred_at)
```

Timeline rendered from events: `submitted → triaged (by Sam K.) → uploaded_to_store → store_review_started → store_live`. Each event has a timestamp, human attribution (when a Wemob team member takes an action), and an inline explanation.

### Real ETAs

`eta_median_seconds` computed from rolling 30-submission median per store per event type. Shows "typically ~20h" not "2–3 business days" — auto-tuned as performance changes.

### Scoped messaging

"Message Wemob team" on a managed submission opens a deployment-scoped ticket thread, not generic support. `tickets.deployment_id` FK.

### Store-Connect polling

Periodic worker polls App Store Connect + Play Publishing APIs for state changes on user-owned connections. For managed track, polls Wemob's own accounts. Writes events into `deployment_events`.

## Cold start

User has no Apple / Google Play accounts yet.

### Three parallel paths

When Pilot detects no Apple connection + DIY chosen:

1. **Switch to Wemob managed** (most pressed). Zero accounts needed, ship in ~2 days.
2. **Set up my Apple account — we'll wait.** Listing draft persists with `deployment.status = "pending_connection"`. Email nudge every 2 days, max 3 emails; each nudge includes "switch to managed" escape.
3. **Ship Play Store first.** Soft suggestion: Android approves in 24h, $25 one-time — a 10x easier path to the first launch.

### Draft submissions are first-class

Reuse `Deployment` model with a new `pending_connection` status value. When the matching connection is added later, the deployment auto-unblocks and submits.

## Power user

### ⌘K universal palette

Scope: current app first, then "across Wemob". Each row shows credit cost + keyboard shortcut hint. `⌘⇧K` scopes to current app only.

### Keyboard shortcuts (when no input focused)

- `b` — new build
- `s` — ship
- `l` — open Launchpad
- `g b` — go to `/ship`
- `?` — cheat sheet
- `⌘⏎` — submit in Quick Ship
- `esc` — close Pilot
- `/` — focus palette search

### /ship cross-app table

Replaces `/builds`. Keyboard-navigable. Single-key filters: `b` = building, `r` = in review, `!` = failed/rejected. Row click = jump to that app's Launchpad.

### Slash commands in chat

Mirror the palette: `/ship android`, `/new-build`, `/retry`, `/connect apple`. AI agent renders inline cards that act as palette results.

### Power-user detection

System auto-hides "Why this matters" accordions for users who (a) never open them across N Pilots, (b) use ⌘K more than mouse for ship actions, (c) close out of Pilots early to review. Toggle in Settings.

## Fastlane integration

We already use `supply` (Android deploy) and `deliver` (iOS deploy). Four high-value additions slot into existing phases:

1. **`fastlane produce`** + `supply --create`: auto-create App Store Connect / Play Console app records on first submission. Removes the manual "create app in console first" step from cold-start iOS. Phase 3.
2. **`fastlane precheck`**: Apple's own metadata rule engine. Replaces our client-side `runStoreListingChecks` heuristics. Catches URL-in-description, future-dated content, private APIs, "beta" in copy. Phase 1.
3. **`deliver --download-metadata`** + `supply --download`: pull existing listings down when a user connects an account with an existing live app. "We found TaskTracker Pro in your App Store Connect — import its current listing?" Phase 3.
4. **`fastlane pilot` (TestFlight)**: powers the Internal/External/Production rollout selector in Quick Ship. Android's `supply track: internal|alpha|beta|production` already works; just surface the selector. Phase 4.

Not adding yet:
- **`fastlane match`** — our S3 + app-store-connect CLI approach is simpler; match is for teams. Revisit for team plans.
- **`snapshot + frameit`** — requires user's app to have integration_test scenarios. Phase 6 upgrade path for apps that do; default stays AI web-preview screenshots.
- **`fastlane gym`** — marginal over `flutter build ios`. Skip.

## Data model changes

### New tables

```
deployment_events (id, deployment_id, event_type, actor_type, actor_id,
                   metadata jsonb, occurred_at)

build_annotations (id, name, log_regex, human_message, suggested_fix,
                   retry_strategy, success_count, failure_count)
```

### Renamed tables

- `app_store_credentials` → `apple_connections` (columns unchanged)
- `google_play_credentials` → `google_play_connections` (columns unchanged)

### Extended

- `deployment`: +`reviewer_user_id` FK, +`external_review_url`, +`pending_connection` enum value on `status`.
- `app_build_task`: +`error_code` enum, +`annotation_id` FK, +`release_notes` text.
- `build`: +`version_semver` text (keep `build_number` as monotonic int).
- `app_project.settings`: +`build_defaults` key (icon_s3_key, splash_color, url_scheme, remove_branding).
- `ticket`: +`deployment_id` FK.

## API changes

### New

- `GET /v1/apps/:id/launchpad` — single aggregate for the Launchpad surface.
- `POST /v1/builds/:id/retry` — body `{ apply_fix: bool }`.
- `GET /v1/deployments/:id/events` — event stream for timeline.
- `POST /v1/deployments/:id/message` — scoped ticket thread.
- `POST /v1/apple-connections/validate` — pre-save App Store Connect check.
- `POST /v1/google-play-connections/validate` — pre-save Play Publishing check.
- `GET /v1/ship` — cross-app table data.

### Renamed (with alias)

- `/v1/app-store-credentials/*` → `/v1/apple-connections/*`
- `/v1/google-play-credentials/*` → `/v1/google-play-connections/*`

Old routes alias to new for two releases.

### Removed

- `POST /v1/sign-ipa` trigger. Sign is auto-chained via Codemagic `build_completed` webhook when iOS + apple_connection present.

## Frontend file organization

### New (`wemob-app/src/`)

```
app/apps/[appId]/ship/page.tsx
app/ship/page.tsx
components/ship/launchpad.tsx
components/ship/launchpad-header.tsx
components/ship/store-tile.tsx
components/ship/listing-grid.tsx
components/ship/builds-feed.tsx
components/ship/identity-panel.tsx
components/ship/pilot/pilot.tsx
components/ship/pilot/step-connection.tsx
components/ship/pilot/step-identity.tsx
components/ship/pilot/step-describe.tsx
components/ship/pilot/step-screenshots.tsx
components/ship/pilot/step-review.tsx
components/ship/pilot/listing-preview.tsx
components/ship/quick-ship.tsx
components/ship/deployment-timeline.tsx
components/ship/command-palette.tsx
components/ship/connection-form/apple-connection-form.tsx
components/ship/connection-form/google-play-connection-form.tsx
hooks/use-launchpad.ts
hooks/use-deployment-timeline.ts
hooks/use-command-palette.ts
lib/ship/commands.ts
lib/ship/annotations.ts
```

### Delete (after rollout complete)

```
components/mobile-studio/publish-mission-control.tsx
components/mobile-studio/deploy-wizard.tsx        (1,711 lines)
components/mobile-studio/create-build-dialog.tsx
components/mobile-studio/deploy-build-dialog.tsx
components/mobile-studio/sign-ipa-dialog.tsx
components/mobile-studio/builds-tab.tsx
app/builds/page.tsx
app/credentials/page.tsx
```

Keep `components/credentials/*` form components; repurpose as `components/ship/connection-form/*`.

## Codemagic yaml changes

- **Merge `ios-build` + `ios-sign`** into one workflow (`ios-build-and-sign`) when Apple connection is passed. Sign becomes a visible stage in the ladder, avoids second Codemagic trigger round-trip.
- **Rename env vars:** `CM_VERSION` → `CM_BUILD_NUMBER`; add `CM_SEMVER`. Update all `pubspec.yaml` + `Info.plist` substitutions.
- **Per-step duration reporting** to backend — each script posts start/end so real ETAs can be computed and stage-specific failures annotated.
- **Stage field on failures**: report step posts computed `stage` ("pod_install", "flutter_build", "codesign") alongside log tail so backend can narrow the annotation match space.
- **New `CM_APPLY_FIX` env** for retry-with-fix — triggers remediation identified by the `annotation_id`.
- **Add `fastlane precheck`** to `ios-deploy` lane (Phase 1).
- **Add `fastlane produce` / `supply --create`** to first-submission paths (Phase 3).
- **Add metadata download** actions for existing-app import (Phase 3).
- **Add `fastlane pilot` (TestFlight)** for rollout track selection (Phase 4).

## Migration strategy

- **Feature flag `new_ship_ux`** via PostHog. `/apps/:id/ship` serves new Launchpad when on; falls back to existing `PublishMissionControl` when off.
- **Rollout ladder:** 5% → 25% → 50% → 100%. Killswitch available. Ramp gated on crash-free rate + key funnel metrics (submission completion, time-to-first-submit).
- **API aliases** on renamed routes for two full releases. Mobile OAuth and any iframe bridges keep working.
- **One-time rename migration** for `app_store_credentials` → `apple_connections` (table rename + FK updates, one transaction).
- **Backfill `build_defaults`** on `app_project.settings` from each app's latest successful build. One-shot Go migration.
- **Auto-sign existing unsigned iOS builds** on first Launchpad view when the user now has an Apple connection.

## Phased rollout — ~8 weeks

### Phase 1 · Foundation (2 weeks) — invisible

- Rename `credentials` → `connections` with route aliases.
- New `deployment_events` + `build_annotations` tables.
- Auto-sign iOS post-build when `apple_connection` present.
- Seed 5 most common build annotations.
- `build_defaults` backfill migration.
- Add `fastlane precheck` to existing `ios-deploy` lane; pipe structured results back via report API.

### Phase 2 · Launchpad (2 weeks) — 5% flag

- `/apps/:id/ship` route.
- `GET /v1/apps/:id/launchpad` aggregate endpoint.
- Launchpad components: header, stores, listing grid, builds feed, identity panel.
- One-click "New build" using project build defaults.

### Phase 3 · Pilot (2 weeks) — 5% flag

- Pilot slide-over + 5-step flow.
- Listing preview (Play + App Store phone frames, HTML/CSS).
- Connection walkthroughs + live validation (`/validate` endpoints).
- AI-drafted description, release notes, screenshots.
- Add `fastlane produce` / `supply --create` to first-submission paths.
- Add `deliver --download-metadata` / `supply --download` for existing-app import.

### Phase 4 · Quick Ship + Power (1 week) — 25% flag

- Quick Ship confirm screen.
- ⌘K command palette + keyboard shortcuts.
- `/ship` cross-app table.
- Slash commands in chat (`/ship`, `/new-build`, `/retry`, `/connect`).
- Wire `fastlane pilot` (TestFlight) into rollout selector.

### Phase 5 · Cold start + Managed polish (1 week) — 50% flag

- `pending_connection` submissions + draft persistence.
- Email nudges for pending connections (max 3 over a week, with "switch to managed" escape).
- Managed timeline view with human attribution.
- Real ETA computation from `deployment_events` medians.
- Store-Connect + Play Publishing periodic poll worker.
- Ramp to 100% → delete old components two releases later.

### Phase 6 (post-launch, optional)

- Multi-locale listings.
- `snapshot + frameit` upgrade path for apps with integration tests.
- Linear / Slack connection types for deployment notifications.

## Out of scope

- Multi-locale listings in v1 (single locale today; Phase 6 follow-up).
- Org-level shared connections (connections remain user-scoped; team plans revisit later).
- TestFlight external-tester group management from within Wemob (users still configure groups in App Store Connect).
- OTA updates (à la EAS Update). Full rebuild per change remains.
- In-app purchase configuration. IAPs stay in store consoles.
- Direct Linear / Slack integrations for deploy notifications (deferred to Phase 6 as connection types).

## Success criteria

- **Time to first submission** (a new user from signup to `submitted`) drops by >50% vs current median.
- **Submission completion rate** (users who open Pilot → successfully submit) improves from current baseline (TBD — collect during Phase 1).
- **Build failure → retry success rate** improves measurably once annotation registry is seeded.
- **Managed submission 95th percentile time-to-live** stays under Apple/Google review windows + 48h of Wemob triage.
- **Zero regressions** in currently-working flows during rollout (flag-gated).
- **Delete >2,500 lines of frontend code** across the listed deletions.

## Open questions for implementation planning

- Exact credit cost numbers for managed vs DIY submissions — design uses 12 / 4 as illustrative; real numbers come from billing.
- Policy for `CM_APPLY_FIX` — which remediations are safe to auto-apply vs. require explicit user consent. Initial pass: conservative, ask before modifying build config.
- Mobile parity of Launchpad — the current dashboard is desktop-optimized; do we support Launchpad on mobile browsers in v1?
- Team/org-level connections — explicitly deferred; confirm timing is post-v1.
