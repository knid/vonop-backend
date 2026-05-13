# Remove the Starter (free) plan

Status: draft (2026-05-13)
Supersedes: free-tier portion of `2026-05-06-subscription-plan-revamp-design.md`

## Goal

Kill the free tier. The product offers Hobby ($19/mo), Pro ($39/mo), and Max ($69/mo) only. New orgs sign up with no plan and hit a paywall on every gated action until they pick a paid plan. The 478 organizations currently on Starter are paused in place — `subscription_id` is set to NULL and their AI credit balances are zeroed.

No advance notice to the 478 orgs; the change ships as a single deploy + migration.

## Non-goals

- Trial flows, grace windows, or grandfathering.
- Frontend paywall UI. The dashboard already needs to render a plan picker when `subscription_id` is null; that work lives in the frontend repo.
- Email notifications to paused orgs.
- Rollback snapshot. We accept irreversibility for the credit zeroing.

## End state

| Surface | Before | After |
|---|---|---|
| `standard_user_subscriptions` rows where `deleted_at IS NULL` | Starter, Hobby, Pro, Max | Hobby, Pro, Max |
| Starter row | active | soft-deleted (`deleted_at = NOW()`), `price_id = NULL`, `annual_price_id = NULL` |
| 478 existing Starter orgs | `subscription_id = <starter>`, `subscription_status = 'free'` | `subscription_id = NULL`, `subscription_status = 'inactive'` |
| AI credit balances for those 478 orgs | varying balances, monthly grant of 50 | `balance = 0`, `pending_gateway = 0`, no future grants |
| New personal org creation | auto-assigned Starter | created with `subscription_id = NULL` |
| Cancelled / dunning paid orgs | re-homed to Starter | left at `subscription_id = uuid.Nil` (paywall) |

`organizations.subscription_id` is already nullable; no DDL is required. Go code reads the column into `uuid.UUID`, so SQL NULL deserializes as `uuid.Nil`, which is already the in-code "no plan" sentinel.

## Database migration

Single transaction. Run as part of the deploy of the code changes below.

```sql
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

Clearing `price_id` releases the `free_plan` sentinel from the unique index (mirrors the precedent in `cmd/main.go` lines 883-895). The Bun `soft_delete` tag on `StandardUserSubscription` keeps the row queryable via `GetSubscriptionByNameIncludeDeleted` but invisible to default queries.

## Code changes

All changes ship in one PR with the SQL migration above.

### `cmd/main.go`

| Lines | Action |
|---|---|
| 883-895 | Delete: "clear legacy Free price_id" cleanup is no longer protecting anything. |
| 898-946 (Starter entry in `seedPlans`'s `plans` slice) | Delete. |
| 1110-1116 (the "restoring soft-deleted subscription plan" path) | Keep. It still applies to Hobby/Pro/Max if they ever get tombstoned by mistake; harmless for Starter once Starter is no longer in the seed slice. |
| 1178-1213 (Free→Starter re-home, then soft-delete of "Free" / "Professional") | Delete. The legacy "Free" row is long gone; the re-home target no longer exists either. |
| 1216-1229 (`migrateInactiveUsersToFree` body + its call site) | Delete the function and call. |

### `internal/controllers/users.go`

| Lines | Action |
|---|---|
| 262 (`c.assignFreePlan(personalOrg)` in Google login flow) | Delete the call. The personal org is created without a plan. |
| 446 (`c.assignFreePlan(personalOrg)` in email signup flow) | Delete the call. |
| 503-522 (`assignFreePlan` function) | Delete. |
| 524-536 (`freePlanIDOrNil` function) | Delete. |

### `internal/controllers/oauth_mobile_controller.go`

| Lines | Action |
|---|---|
| 107 (`c.assignFreePlan(personalOrg)`) | Delete the call. |

### `internal/controllers/organizations.go`

`EnsurePersonalOrg` (lines 494-513): drop the `GetSubscriptionByName("Starter")` lookup and the related error log. Create the personal org with `SubscriptionID` zero-valued (which serializes as SQL NULL via Bun).

### `internal/controllers/webhook.go`

Three sites (lines 342, 357, 717) currently call `c.freePlanIDOrNil()` to demote a cancelled or delinquent org back to the free tier. Replace each with `org.SubscriptionID = uuid.Nil`. Cancelled paid customers transition to "no plan" → paywall, not back to free tier.

The credit-grant block at line 205 ("Grant plan's monthly credits…") should already short-circuit when the org's subscription lookup fails (the only place it's called from has a paid `subscription_id`), but verify during execution that there is no code path that grants credits to an org with NULL `subscription_id`.

## Behavior changes worth flagging

- **New signups land with no plan.** Every gated action (generate, build, deploy, ecommerce, etc.) must show the paywall. Until the frontend ships the paywall UI, these actions fail-closed with whatever error the backend currently returns when limits resolve to zero.
- **Cancellation no longer demotes to free.** A customer who cancels at end-of-billing-period stops at the paywall on their next gated action. This is a real product behavior change versus "they can keep using the free tier after cancelling".
- **No more monthly credit grants for the 478 paused orgs.** Their balances stay at zero until they pick a plan.

## Verification

Run before merging:

1. **Feature gating fails closed on `uuid.Nil`/NULL `subscription_id`.** Audit the four call sites that fetch the org's subscription:
   - `internal/controllers/web_deploy_controller.go:224`
   - `internal/controllers/ai_gateway_controller.go:190`
   - `internal/controllers/webhook.go:708`
   - `internal/controllers/user_billing.go:37`
   Today these return `sql.ErrNoRows` on a `uuid.Nil` lookup. Some callers ignore the error and operate on a zero-value `*StandardUserLimits` (effectively fail-closed); others may nil-deref. Convert any nil-deref hazard into an explicit early-return that returns the paywall error.
2. **`/plans`-style API returns only Hobby/Pro/Max.** The Bun `soft_delete` tag on the model already filters; sanity-check the response payload.
3. **Monthly credit-grant cron** does not top up orgs with `subscription_id IS NULL`. Trace the cron entry point and confirm.

## Rollback

- Restoring the Starter plan row: `UPDATE standard_user_subscriptions SET deleted_at = NULL WHERE name = 'Starter';` (the limits row is untouched).
- Re-homing the 478 orgs back to Starter is **not possible without a snapshot**, which we explicitly chose not to take. Once they have `subscription_id = NULL`, they are indistinguishable from genuinely new signups.
- Restoring zeroed credit balances is **not possible without a snapshot**, same reason.

The deliberate decision is: this is a one-way change. If we need to back out, we re-launch a free tier as a new product decision, not as a rollback.
