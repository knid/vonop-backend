# Remove the Starter Plan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Kill the free tier. Soft-delete the Starter row, NULL out the 478 existing Starter orgs and zero their credit balances, strip all auto-Starter-assignment code paths, demote cancelled paid orgs to NULL instead of free tier.

**Architecture:** One Go PR plus a one-off SQL migration script. Code changes remove every path that defaults an organization to Starter. The migration soft-deletes the row, NULLs `subscription_id` on the 478 ex-Starter orgs, and zeros their AI credit balances. Feature gating is already nil-safe on `subscription_id = uuid.Nil` (lookups return `sql.ErrNoRows`, callers ignore the error and treat zero-value limits as fail-closed); we audit but do not change those paths.

**Tech Stack:** Go 1.x, Bun ORM, PostgreSQL, chi router. No formal migration tool — `cmd/main.go` auto-creates schema. The SQL migration ships as a one-off script under `scripts/migrations/`.

**Spec:** `docs/superpowers/specs/2026-05-13-remove-starter-plan-design.md`

---

## File Structure

Files modified:

- `cmd/main.go` — remove Starter from `seedPlans`'s plan slice; drop legacy Free→Starter re-home + `migrateInactiveUsersToFree`
- `internal/controllers/users.go` — delete `assignFreePlan` and `freePlanIDOrNil`; remove two call sites
- `internal/controllers/oauth_mobile_controller.go` — remove one `assignFreePlan` call site
- `internal/controllers/organizations.go` — `EnsurePersonalOrg` no longer assigns Starter
- `internal/controllers/webhook.go` — replace three `freePlanIDOrNil()` calls with `uuid.Nil`

Files created:

- `scripts/migrations/2026-05-13-remove-starter-plan.sql` — one-off SQL migration run by an operator after the code deploys

No files split or moved. Task ordering ensures every commit compiles: webhook callers swap first, then function deletions in users.go, then everything else.

---

## Task 1: Replace freePlanIDOrNil calls in webhook.go

**Files:**
- Modify: `internal/controllers/webhook.go` (three call sites: ~lines 342, 357, 717)

`freePlanIDOrNil()` will be deleted in Task 3, but its callers in `webhook.go` need to point at `uuid.Nil` first so the build never breaks.

- [ ] **Step 1: Find the three call sites**

Run: `grep -n "freePlanIDOrNil" internal/controllers/webhook.go`

Expected: three matching lines around 342, 357, 717. Verify nothing else in the file references the function.

- [ ] **Step 2: Replace the `canceled` case (around line 342)**

Old:
```go
case "canceled":
    // A5/A7/A8: re-home to the Free plan instead of nulling SubscriptionID.
    // Otherwise GetStandardUserSubscriptionByOrg errors and every
    // `if err == nil` enforcement gate falls through, letting cancelled
    // users keep deploying unbounded. With Free's tight quotas in place,
    // the gates fire correctly while requireActiveSubscription still
    // blocks "canceled" status on write paths.
    org.SubscriptionStatus = newStatus
    org.SubscriptionID = c.freePlanIDOrNil()
    // A6: schedule the 7-day grace teardown.
    if err := c.EnqueueOrgGraceTeardown(org.ID); err != nil {
        slog.Warn("canceled: failed to schedule grace teardown", "error", err, "org_id", org.ID)
    }
```

New:
```go
case "canceled":
    // Free tier is gone (2026-05-13): cancelled orgs go to no plan.
    // Every feature gate fails closed on SubscriptionID == uuid.Nil because
    // the subscription lookup returns sql.ErrNoRows and callers operate
    // on a zero-value *StandardUserLimits (all Can* false, all counts 0).
    org.SubscriptionStatus = newStatus
    org.SubscriptionID = uuid.Nil
    // A6: schedule the 7-day grace teardown.
    if err := c.EnqueueOrgGraceTeardown(org.ID); err != nil {
        slog.Warn("canceled: failed to schedule grace teardown", "error", err, "org_id", org.ID)
    }
```

- [ ] **Step 3: Replace the `default` case (around line 357)**

Old:
```go
default:
    // incomplete, incomplete_expired, paused — re-home to Free for the
    // same reason as "canceled" above (avoid fail-open enforcement gates).
    org.SubscriptionStatus = newStatus
    org.SubscriptionID = c.freePlanIDOrNil()
```

New:
```go
default:
    // incomplete, incomplete_expired, paused — null the plan for the
    // same reason as "canceled" above (no more free tier).
    org.SubscriptionStatus = newStatus
    org.SubscriptionID = uuid.Nil
```

- [ ] **Step 4: Replace the `subscription_canceled` block (around line 717)**

Old:
```go
// A5/A7/A8: re-home to Free instead of nulling SubscriptionID so the
// limit-lookup path returns Free's tight quotas, and fail-closed
// enforcement gates still see a real plan after cancellation.
org.SubscriptionStatus = "canceled"
org.SubscriptionID = c.freePlanIDOrNil()
```

New:
```go
// Free tier removed 2026-05-13: cancelled orgs go to no plan.
// Feature gates fail closed because the subscription lookup returns
// sql.ErrNoRows on uuid.Nil and callers treat the resulting zero-value
// limits as Can*=false / count=0.
org.SubscriptionStatus = "canceled"
org.SubscriptionID = uuid.Nil
```

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: succeeds. No references to `freePlanIDOrNil` left in `webhook.go`.

- [ ] **Step 6: Verify with grep**

Run: `grep -n "freePlanIDOrNil" internal/controllers/webhook.go`
Expected: no output (zero matches).

- [ ] **Step 7: Commit**

```bash
git add internal/controllers/webhook.go
git -c commit.gpgsign=false commit -m "webhook: null subscription_id on cancel instead of re-home to Starter

Free tier is being removed. Cancelled orgs now transition to
SubscriptionID = uuid.Nil; feature gates already fail closed on that
state (lookup returns sql.ErrNoRows, callers operate on zero-value
limits)."
```

---

## Task 2: Remove the two assignFreePlan call sites from users.go

**Files:**
- Modify: `internal/controllers/users.go` (around lines 262 and 446)

- [ ] **Step 1: Find the call sites**

Run: `grep -n "assignFreePlan" internal/controllers/users.go`

Expected: lines 262 and 446 (call sites), plus 503 and 522 (function body — leave those alone in this task).

- [ ] **Step 2: Delete the email-signup caller (around line 262)**

Find the block:

```go
createdUser.CustomerID = customer.ID
if _, err := c.DB.UpdateUser(createdUser); err != nil {
    slog.Error("failed to persist Stripe customer to user", "error", err, "user_id", createdUser.ID, "customer_id", customer.ID)
}

c.assignFreePlan(personalOrg)

// Apply referral code if provided during registration
```

Delete the empty line + `c.assignFreePlan(personalOrg)` + the following empty line so the next comment butts up against the `UpdateUser` block.

Result:
```go
createdUser.CustomerID = customer.ID
if _, err := c.DB.UpdateUser(createdUser); err != nil {
    slog.Error("failed to persist Stripe customer to user", "error", err, "user_id", createdUser.ID, "customer_id", customer.ID)
}

// Apply referral code if provided during registration
```

- [ ] **Step 3: Delete the Google-signup caller (around line 446)**

Find the block:

```go
user.CustomerID = customer.ID
if _, err := c.DB.UpdateUser(user); err != nil {
    slog.Error("failed to persist Stripe customer to user", "error", err, "user_id", user.ID, "customer_id", customer.ID)
}

c.assignFreePlan(personalOrg)

if err := c.GrantInitialFreeCreditsIfNeeded(user, personalOrg.ID); err != nil {
```

Delete the empty line + `c.assignFreePlan(personalOrg)` + the following empty line.

Result:
```go
user.CustomerID = customer.ID
if _, err := c.DB.UpdateUser(user); err != nil {
    slog.Error("failed to persist Stripe customer to user", "error", err, "user_id", user.ID, "customer_id", customer.ID)
}

if err := c.GrantInitialFreeCreditsIfNeeded(user, personalOrg.ID); err != nil {
```

- [ ] **Step 4: Verify only the function definition remains**

Run: `grep -n "assignFreePlan" internal/controllers/users.go`

Expected: only the function definition line (~503) and the `slog.Warn("assignFreePlan: ..."` lines inside it. No call sites remain.

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: succeeds (the function still exists; it just has no callers in this file yet — oauth_mobile_controller still calls it).

- [ ] **Step 6: Commit**

```bash
git add internal/controllers/users.go
git -c commit.gpgsign=false commit -m "users: stop auto-assigning Starter to new email/google signups

Personal orgs now created with SubscriptionID zero. assignFreePlan
function deleted in a later commit once its last caller in
oauth_mobile_controller.go is also removed."
```

---

## Task 3: Remove the assignFreePlan call from oauth_mobile_controller.go

**Files:**
- Modify: `internal/controllers/oauth_mobile_controller.go` (around line 107)

- [ ] **Step 1: Find the call site**

Run: `grep -n "assignFreePlan" internal/controllers/oauth_mobile_controller.go`

Expected: one line at 107.

- [ ] **Step 2: Delete the call**

Find the block:

```go
user.CustomerID = stripeCustomer.ID
if _, err := c.DB.UpdateUser(user); err != nil {
    slog.Error("failed to persist Stripe customer to user", "error", err, "user_id", user.ID, "customer_id", stripeCustomer.ID)
}

c.assignFreePlan(personalOrg)

if err := c.GrantInitialFreeCreditsIfNeeded(user, personalOrg.ID); err != nil {
```

Delete the empty line + `c.assignFreePlan(personalOrg)` + the following empty line.

Result:
```go
user.CustomerID = stripeCustomer.ID
if _, err := c.DB.UpdateUser(user); err != nil {
    slog.Error("failed to persist Stripe customer to user", "error", err, "user_id", user.ID, "customer_id", stripeCustomer.ID)
}

if err := c.GrantInitialFreeCreditsIfNeeded(user, personalOrg.ID); err != nil {
```

- [ ] **Step 3: Verify no remaining call sites in the package**

Run: `grep -rn "assignFreePlan" internal/controllers/`

Expected: only the function definition in `users.go` (line ~503) and its internal `slog.Warn` line — no callers anywhere.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/oauth_mobile_controller.go
git -c commit.gpgsign=false commit -m "oauth_mobile: stop auto-assigning Starter to new mobile-OAuth signups"
```

---

## Task 4: Delete assignFreePlan and freePlanIDOrNil from users.go

**Files:**
- Modify: `internal/controllers/users.go` (function bodies ~lines 503-536)

Both functions are now unused (Tasks 1-3 removed every caller). Delete them.

- [ ] **Step 1: Confirm no callers**

Run: `grep -rn "assignFreePlan\|freePlanIDOrNil" internal/`

Expected: only the function definitions themselves in `users.go`, no external callers.

- [ ] **Step 2: Delete `assignFreePlan` (lines ~503-522)**

Delete this entire block from `internal/controllers/users.go`:

```go
// assignFreePlan sets the org onto the "Starter" subscription plan (the free
// tier) and marks its subscription_status as "free". Org-scoped: billing now
// lives on Organization, not User. Callers wanting to apply this to a user
// should resolve the user's personal org first via GetPersonalOrgByUserID.
func (c *Controller) assignFreePlan(org *models.Organization) {
    if org == nil {
        return
    }
    freePlan, err := c.DB.GetSubscriptionByName("Starter")
    if err != nil {
        slog.Warn("assignFreePlan: starter plan not found", "error", err)
        return
    }
    org.SubscriptionID = freePlan.ID
    org.SubscriptionStatus = "free"
    if _, err := c.DB.UpdateOrganization(org); err != nil {
        slog.Warn("assignFreePlan: failed to update org", "error", err, "org_id", org.ID)
        return
    }
}
```

- [ ] **Step 3: Delete `freePlanIDOrNil` (lines ~524-536)**

Delete this entire block:

```go
// freePlanIDOrNil returns the Starter (free tier) plan's UUID, or uuid.Nil if
// the plan row is missing (shouldn't happen post-seedPlans). Used by the
// cancellation / dunning paths in webhook.go to re-home an org's
// subscription_id to the free tier instead of nulling it
// (A5/A7/A8 — fail-closed on enforcement).
func (c *Controller) freePlanIDOrNil() uuid.UUID {
    freePlan, err := c.DB.GetSubscriptionByName("Starter")
    if err != nil || freePlan == nil {
        slog.Warn("freePlanIDOrNil: starter plan not found", "error", err)
        return uuid.Nil
    }
    return freePlan.ID
}
```

- [ ] **Step 4: Check for orphaned imports**

If `users.go` no longer references `github.com/google/uuid` or any other package that was only used by these functions, `goimports` / `go build` will surface it. Most likely `uuid` is used elsewhere in the file (it's a common import), but check:

Run: `go build ./...`

If build fails with "imported and not used: ...", remove that import line.

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/users.go
git -c commit.gpgsign=false commit -m "users: delete assignFreePlan and freePlanIDOrNil

All callers removed in earlier commits. The Starter plan is going away."
```

---

## Task 5: EnsurePersonalOrg no longer looks up Starter

**Files:**
- Modify: `internal/controllers/organizations.go` (around lines 494-515)

- [ ] **Step 1: Find the Starter-defaulting block**

Run: `grep -n "Starter\|starterID" internal/controllers/organizations.go`

Expected: matches inside `EnsurePersonalOrg` around lines 494-505.

- [ ] **Step 2: Replace the block**

Old:
```go
// Default new personal orgs to the Starter plan. If the Starter row is
// missing the new org is born with SubscriptionID=uuid.Nil; every limits
// and credits lookup that joins through it then breaks silently. We hit
// this in 2026-05 when a botched seed left no Starter row and 328 personal
// orgs landed unattached. Log loudly so BetterStack alerts the next time.
var starterID uuid.UUID
starter, err := c.DB.GetSubscriptionByName("Starter")
if err != nil || starter == nil {
    slog.Error("EnsurePersonalOrg: Starter plan not found — new org will have NULL subscription_id",
        "user_id", user.ID, "error", err)
} else {
    starterID = starter.ID
}

org := &models.Organization{
    Name:               orgName,
    Slug:               slug,
    OwnerID:            user.ID,
    IsPersonal:         true,
    SubscriptionID:     starterID,
    SubscriptionStatus: "free",
}
```

New:
```go
// Free tier removed 2026-05-13: new personal orgs are born with no plan.
// The frontend renders a plan-picker / paywall when SubscriptionID is zero;
// every backend feature gate fails closed (subscription lookup returns
// sql.ErrNoRows, callers operate on zero-value limits).
org := &models.Organization{
    Name:       orgName,
    Slug:       slug,
    OwnerID:    user.ID,
    IsPersonal: true,
    // SubscriptionID left zero — serializes as NULL via Bun.
    // SubscriptionStatus left at column default 'inactive'.
}
```

- [ ] **Step 3: Check imports**

If `slog` is no longer used elsewhere in the file, the import block stays valid only if other functions use it. Check:

Run: `grep -n "slog\." internal/controllers/organizations.go | head -5`

If other lines still use `slog.`, leave the import. Otherwise let `go build` flag it and remove.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/organizations.go
git -c commit.gpgsign=false commit -m "organizations: new personal orgs born with no plan

EnsurePersonalOrg used to default new orgs to the Starter (free) plan and
log loudly if Starter was missing. The free tier is being removed; orgs
now start with SubscriptionID zero and SubscriptionStatus 'inactive'."
```

---

## Task 6: Remove Starter from seedPlans's plan slice

**Files:**
- Modify: `cmd/main.go` (the `Starter` entry inside `seedPlans`, around lines 898-946)

- [ ] **Step 1: Locate the `seedPlans` function and its plan slice**

Run: `grep -n "Name: *\"Starter\"\|func seedPlans" cmd/main.go`

Expected: a `func seedPlans(...)` line and a `Name: "Starter"` line inside its `plans := []planDef{...}` slice.

- [ ] **Step 2: Delete the entire Starter entry**

In `cmd/main.go::seedPlans`, the `plans` slice starts with the Starter struct literal (~line 898). Delete the entire `{Name: "Starter", ...}` block including its trailing comma. The slice should start directly with the Hobby entry:

Before:
```go
plans := []planDef{
    {
        Name:         "Starter",
        Description:  "Free tier — try Wemob with no commitment",
        MonthlyPrice: 0,
        PriceID:      "free_plan",
        Limits: models.StandardUserLimits{ /* ...lots of fields... */ },
    },
    {
        Name:          "Hobby",
        // ...
```

After:
```go
plans := []planDef{
    {
        Name:          "Hobby",
        // ...
```

- [ ] **Step 3: Delete the legacy "free up Free price_id" cleanup**

Just above the `plans := []planDef{...}` declaration (~lines 883-895), there is a one-time UPDATE that was added for the 2026-05-06 Free→Starter rename. Delete that block:

```go
// 2026-05-06: free up the "free_plan" sentinel price_id from the legacy
// "Free" row so the seed-update loop below can assign the same sentinel
// to the new "Starter" $0 row. Without this, the UPDATE hits the
// price_id unique constraint and the new Starter row keeps the OLD
// Stripe price ID and $20 monthly_price (the symptom we hit on first
// post-revamp prod boot). The legacy Free row gets soft-deleted further
// down anyway, so clearing its price_id is safe.
if _, err := db.DB.NewUpdate().
    Table("standard_user_subscriptions").
    Set("price_id = NULL").
    Where("name = ? AND price_id = ?", "Free", "free_plan").
    Exec(context.Background()); err != nil {
    slog.Warn("failed to clear legacy Free price_id pre-seed", "error", err)
}
```

The function should now go straight from the `planDef` type declaration into the `plans := []planDef{...}` slice with no intervening logic.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: succeeds.

- [ ] **Step 5: Commit**

```bash
git add cmd/main.go
git -c commit.gpgsign=false commit -m "main: drop Starter from seedPlans

The free tier is being removed. seedPlans now only knows about
Hobby / Pro / Max. Also drops the 2026-05-06 'free up free_plan price_id'
cleanup that only mattered when Starter was being seeded."
```

---

## Task 7: Remove the legacy Free→Starter re-home and migrateInactiveUsersToFree from cmd/main.go

**Files:**
- Modify: `cmd/main.go` (the trailing block of `seedPlans` ~lines 1178-1213, the `migrateInactiveUsersToFree` function ~lines 1216-1231, and the call to it ~line 1409)

- [ ] **Step 1: Locate the Free→Starter re-home block**

Run: `grep -n "Free→Starter\|re-home\|Free.*Starter\|migrateInactiveUsersToFree" cmd/main.go`

Expected: matches around lines 1178, 1188, 1199, 1216, 1409.

- [ ] **Step 2: Delete the Free→Starter re-home block**

In `seedPlans` (after the seed-create/update loop), delete this block (~lines 1178-1213):

```go
// 2026-05-06: re-home orgs from the legacy "Free" plan to the new "Starter"
// plan, then soft-delete legacy plan rows ("Free", "Professional").
//
// ORDER MATTERS: the re-home step must run AFTER the seed-create-or-update
// loop above, because the new "Starter" $0 row is created/updated by that
// loop. Soft-deleting "Free" must happen AFTER the re-home so that orgs are
// never left pointing at a soft-deleted subscription row.
//
// Use Bun's NewDelete on the model so the soft_delete struct tag fires
// (sets deleted_at instead of removing the row).
freeRow, freeErr := db.GetSubscriptionByName("Free")
starterRow, starterErr := db.GetSubscriptionByName("Starter")
if freeErr == nil && starterErr == nil {
    res, err := db.DB.NewUpdate().
        Table("organizations").
        Set("subscription_id = ?", starterRow.ID).
        Where("subscription_id = ?", freeRow.ID).
        Exec(context.Background())
    if err != nil {
        slog.Warn("failed to re-home orgs from Free to Starter", "error", err)
    } else if n, _ := res.RowsAffected(); n > 0 {
        slog.Info("re-homed orgs from Free to Starter", "count", n)
    }
}

for _, legacyName := range []string{"Free", "Professional"} {
    existing, err := db.GetSubscriptionByName(legacyName)
    if err != nil {
        continue // already gone or never seeded
    }
    if _, dErr := db.DB.NewDelete().Model(existing).Where("id = ?", existing.ID).Exec(context.Background()); dErr != nil {
        slog.Warn("failed to soft-delete legacy plan", "name", legacyName, "error", dErr)
    } else {
        slog.Info("soft-deleted legacy plan", "name", legacyName)
    }
}
```

`seedPlans` should end immediately after the `for _, p := range plans { ... }` loop's closing brace.

- [ ] **Step 3: Delete the `migrateInactiveUsersToFree` function**

Delete the entire function (~lines 1216-1231):

```go
func migrateInactiveUsersToFree(bundb *bun.DB) {
    // Assign starter plan (free tier) to inactive users
    res, err := bundb.ExecContext(context.Background(), `
        UPDATE users SET subscription_status = 'free', subscription_id = (
            SELECT id FROM standard_user_subscriptions WHERE name = 'Starter' AND deleted_at IS NULL LIMIT 1
        ) WHERE subscription_status = 'inactive'
        AND (subscription_id IS NULL OR subscription_id = '00000000-0000-0000-0000-000000000000')
        AND deleted_at IS NULL
    `)
    if err != nil {
        slog.Warn("migrate inactive users to starter plan failed", "error", err)
    } else if n, _ := res.RowsAffected(); n > 0 {
        slog.Info("migrated inactive users to starter plan", "count", n)
    }

}
```

- [ ] **Step 4: Delete the call to `migrateInactiveUsersToFree`**

Find the bootstrap call (~line 1409):

```go
seedPlans(db, sm)
migrateInactiveUsersToFree(bundb)
```

Delete the second line. `seedPlans(db, sm)` remains.

- [ ] **Step 5: Verify no references remain**

Run: `grep -n "migrateInactiveUsersToFree\|re-home" cmd/main.go`
Expected: no output.

- [ ] **Step 6: Build**

Run: `go build ./...`
Expected: succeeds.

- [ ] **Step 7: Commit**

```bash
git add cmd/main.go
git -c commit.gpgsign=false commit -m "main: drop legacy Free→Starter re-home and migrateInactiveUsersToFree

Both paths assumed a Starter row existed as the universal default for
no-plan accounts. The free tier is being removed so neither path is
useful anymore — they would silently no-op once Starter is gone."
```

---

## Task 8: Audit feature gating on uuid.Nil

**Files:**
- Read-only: `internal/controllers/web_deploy_controller.go`, `internal/controllers/ai_gateway_controller.go`, `internal/controllers/webhook.go`, `internal/controllers/user_billing.go`

This task is a verification pass; no code changes are expected. If anything is found that would nil-deref on a NULL subscription, fix it in this task and commit.

- [ ] **Step 1: Inspect web_deploy_controller.go ~line 224**

Run: `sed -n '220,235p' internal/controllers/web_deploy_controller.go`

Expected pattern (already nil-safe):
```go
var limits *models.StandardUserLimits
if project.OrganizationID != uuid.Nil {
    limits, _ = c.DB.GetLimitsForOrg(ctx, project.OrganizationID)
}
if limits == nil {
    limits, _ = c.GetUserLimits(user)
}
if limits == nil || !limits.CanRemoveBrandLogo {
    injectWemobBadge(fileMap)
}
```

`limits == nil` is checked before every dereference. **Passes.** No change.

- [ ] **Step 2: Inspect ai_gateway_controller.go ~line 190**

Run: `sed -n '180,200p' internal/controllers/ai_gateway_controller.go`

Expected pattern (already nil-safe):
```go
func (c *Controller) GetUserTierName(user *models.User) string {
    org, err := c.DB.GetPersonalOrgByUserID(user.ID.String())
    if err != nil || org == nil {
        return "free"
    }
    if org.SubscriptionStatus != "active" {
        return "free"
    }
    sub, err := c.DB.GetSubscriptionById(org.SubscriptionID.String())
    if err != nil {
        return "free"
    }
    return strings.ToLower(sub.Name)
}
```

Orgs with `subscription_status = 'inactive'` (the new default) take the second early-return. **Passes.** No change.

The tier `switch` further up the file (`case "starter":`, `case "pro":`, `case "business":` with a default → "free") routes ex-Starter orgs to the `default` "free" branch because `GetUserTierName` returns "free" for non-active orgs. The dead `case "starter":` arm can be removed in a follow-up cleanup PR; do NOT include it in this plan.

- [ ] **Step 3: Inspect webhook.go ~line 708**

Run: `sed -n '703,720p' internal/controllers/webhook.go`

Expected pattern (already nil-safe):
```go
var planName string
if org.SubscriptionID != uuid.Nil {
    if subs, perr := c.DB.GetSubscriptionById(org.SubscriptionID.String()); perr == nil && subs != nil {
        planName = subs.Name
    }
}
```

Explicit `uuid.Nil` guard plus `nil` check on result. **Passes.** No change.

- [ ] **Step 4: Inspect user_billing.go ~line 37**

Run: `sed -n '27,50p' internal/controllers/user_billing.go`

Expected pattern (already nil-safe):
```go
var sub *models.StandardUserSubscription
if org.SubscriptionID != uuid.Nil {
    if s, err := c.DB.GetSubscriptionById(org.SubscriptionID.String()); err == nil {
        sub = s
    }
}
return UserBilling{
    ...
    Subscription: sub,
}
```

`Subscription` returned as nil pointer when no plan; callers must handle nil. **Passes.** No change.

- [ ] **Step 5: Inspect the monthly credit-grant path**

Run: `grep -n "Grant plan's monthly credits\|GrantMonthlyCredits\|grantMonthlyCredits" internal/controllers/webhook.go`

Expected: line ~205 inside a Stripe webhook dedup block that only fires on `customer.subscription.created` / `invoice.payment_succeeded` Stripe events. Orgs with `subscription_id = NULL` never receive those events from Stripe (they have no Stripe subscription), so the grant code is unreachable for them. No change required — write this conclusion as a note in the PR description.

- [ ] **Step 6: Commit (only if changes were made)**

If any of Steps 1-5 surfaced a nil-deref, fix it in this commit. Otherwise skip the commit. Likely no commit here.

---

## Task 9: Write the SQL migration script

**Files:**
- Create: `scripts/migrations/2026-05-13-remove-starter-plan.sql`

- [ ] **Step 1: Verify the scripts/migrations directory exists**

Run: `ls scripts/migrations/ 2>/dev/null | tail -5`

If the directory does not exist, create it:
```bash
mkdir -p scripts/migrations
```

(Check whether the repo already has a different home for one-off migration scripts. Run `find . -type d -name 'migrations' -not -path './.git*' 2>/dev/null` to confirm. If a different location exists, use that.)

- [ ] **Step 2: Write the migration script**

Create `scripts/migrations/2026-05-13-remove-starter-plan.sql` with these contents:

```sql
-- 2026-05-13: remove the Starter (free) plan.
-- Spec: docs/superpowers/specs/2026-05-13-remove-starter-plan-design.md
--
-- Run AFTER the matching code release is live. The new binary no longer
-- depends on the Starter row existing. Running before the release would
-- temporarily NULL-out subscription_id on signups while the old binary
-- still tries to re-home cancellations to Starter.
--
-- Side effects (irreversible):
--   1. Zero out AI credit balances + pending_gateway for the 478 ex-Starter orgs
--   2. NULL out subscription_id and reset subscription_status='inactive' for those orgs
--   3. Soft-delete the Starter subscription row and clear its Stripe price IDs

BEGIN;

WITH starter AS (
  SELECT id FROM standard_user_subscriptions
  WHERE name = 'Starter' AND deleted_at IS NULL
),
target_orgs AS (
  SELECT id FROM organizations
  WHERE subscription_id = (SELECT id FROM starter)
)
UPDATE ai_credits_balances
SET    balance = 0, pending_gateway = 0, updated_at = NOW()
WHERE  organization_id IN (SELECT id FROM target_orgs);

UPDATE organizations
SET    subscription_id = NULL,
       subscription_status = 'inactive',
       updated_at = NOW()
WHERE  subscription_id = (
  SELECT id FROM standard_user_subscriptions
  WHERE name = 'Starter' AND deleted_at IS NULL
);

UPDATE standard_user_subscriptions
SET    deleted_at = NOW(),
       price_id = NULL,
       annual_price_id = NULL
WHERE  name = 'Starter' AND deleted_at IS NULL;

COMMIT;
```

- [ ] **Step 3: Dry-run-check the affected row counts (optional, recommended)**

Before running the migration on prod, an operator should run this read-only query to confirm the row counts match the design (478 orgs expected):

```sql
WITH starter AS (
  SELECT id FROM standard_user_subscriptions
  WHERE name = 'Starter' AND deleted_at IS NULL
)
SELECT
  (SELECT count(*) FROM organizations
     WHERE subscription_id = (SELECT id FROM starter)) AS orgs_to_pause,
  (SELECT count(*) FROM ai_credits_balances
     WHERE organization_id IN (
       SELECT id FROM organizations
       WHERE subscription_id = (SELECT id FROM starter))) AS balances_to_zero;
```

If `orgs_to_pause` is wildly different from ~478 the operator should pause and investigate before running the migration.

- [ ] **Step 4: Commit**

```bash
git add scripts/migrations/2026-05-13-remove-starter-plan.sql
git -c commit.gpgsign=false commit -m "migrations: SQL to soft-delete Starter, null ex-Starter orgs, zero their credits

One-off script to be run by an operator after the code release deploys.
Self-contained transaction with a recommended pre-run sanity query in
the comments."
```

---

## Task 10: Build + vet + tests

Verify the whole tree compiles, the integration build tag still resolves, and existing unit tests still pass.

- [ ] **Step 1: Build the whole tree**

Run: `go build ./...`
Expected: zero errors.

- [ ] **Step 2: Run `go vet` against both build sets**

Run: `go vet ./...`
Expected: no diagnostics.

Run: `go vet -tags=integration ./...`
Expected: no diagnostics. (Per CLAUDE.md, this keeps integration-only testcontainers deps from being pruned by `go mod tidy`.)

- [ ] **Step 3: Run the standard (non-integration) test suite**

Run: `go test ./...`
Expected: all tests pass. Note: `internal/controllers/build_credits_test.go::seedUserOrgAndCredits` creates its own "Starter"-named plan row inside its testcontainer DB — that name is a test-fixture string, not a reference to the production seed. It still works.

- [ ] **Step 4: (Optional, slow) Run the integration tests**

Run: `make test-integration`
Expected: all tests pass. Requires Docker for the testcontainers Postgres.

If anything fails, fix it in this task before moving on.

- [ ] **Step 5: Commit any fixes**

If steps 1-4 needed code changes, commit them with a message describing the fix. If everything passed clean, skip this step.

---

## Task 11: Open the PR

- [ ] **Step 1: Push the branch**

Run: `git push -u origin worktree-remove-starter-plan`

Expected: branch published to origin.

- [ ] **Step 2: Open the PR with `gh`**

```bash
gh pr create --title "Remove the Starter (free) plan" --body "$(cat <<'EOF'
## Summary
- Spec: `docs/superpowers/specs/2026-05-13-remove-starter-plan-design.md`
- Kills the free tier. Hobby / Pro / Max only.
- Soft-deletes the Starter row, NULLs `subscription_id` on the 478 existing Starter orgs, zeros their AI credit balances.
- Strips every "default new org to Starter" / "demote cancellation to Starter" code path.
- New personal orgs are born with `subscription_id = NULL` and `subscription_status = 'inactive'`.
- SQL migration ships as `scripts/migrations/2026-05-13-remove-starter-plan.sql` and is run manually by an operator after the code release.

## Behavior changes
- New signups land with no plan; every gated action returns the existing zero-limits fail-closed error until the frontend renders the plan-picker / paywall.
- Cancelled paid customers transition to `uuid.Nil`, not back to free tier.
- The 478 paused orgs receive no further monthly credit grants (no Stripe subscription → no `invoice.payment_succeeded` event → no grant path).

## Test plan
- [ ] `go build ./...` (passes)
- [ ] `go vet ./...` and `go vet -tags=integration ./...` (passes)
- [ ] `go test ./...` (passes)
- [ ] Optional: `make test-integration` (passes with Docker)
- [ ] Operator dry-run query confirms `orgs_to_pause ≈ 478` before running the migration
- [ ] After deploy + migration: signup → no plan → paywall (verify frontend behavior in coordination)
- [ ] After deploy + migration: cancel a test paid sub → org ends up at `subscription_id = NULL` (verify via Stripe test mode + webhook delivery)

## Out of scope
- Frontend paywall UI lives in the dashboard repo
- AI gateway `case "starter":` dead-code cleanup (follow-up PR)
- Test-fixture rename in `build_credits_test.go` (no functional issue)
EOF
)"
```

Expected: PR URL printed.

- [ ] **Step 3: Coordinate the operator migration step**

The migration is intentionally manual and runs AFTER merge + deploy. Hand the operator the script path (`scripts/migrations/2026-05-13-remove-starter-plan.sql`) and the dry-run sanity query (Task 9, Step 3).
