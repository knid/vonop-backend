# Live Credit Checking During App Generation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop app generation mid-stream when the user's credit budget is exhausted, and always deduct what was consumed (partial deduction to floor) instead of silently deducting nothing.

**Architecture:** Add a `CreditBudget` check that computes max spendable credits (balance + |overdraft floor|). Wire a `ShouldContinue` callback into every `StreamWithTools` call to halt between LLM rounds when budget is exceeded. Replace the all-or-nothing deduction with a partial "deduct to floor" SQL that always collects what it can. Add a budget gate before the expensive single-shot generation call.

**Tech Stack:** Go, PostgreSQL (Bun ORM), PostHog feature flags (remote config), SSE streaming

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/database/ai_credits.go` | Modify | Add `AtomicDeductCreditsToFloor` SQL |
| `internal/controllers/ai_credits.go` | Modify | Add `CreditBudget`, `MakeCreditBudgetChecker`, `DeductAICreditsToFloor` |
| `internal/controllers/app_builder_controller.go` | Modify | Wire `ShouldContinue` into chat `StreamWithTools` calls; change defer to partial deduction |
| `internal/controllers/app_builder_generation.go` | Modify | Add budget gate before single-shot/ecommerce generation; wire `ShouldContinue` into ecommerce setup |
| `internal/controllers/app_builder_tools.go` | Modify | Wire `ShouldContinue` into Phase 1 chat tools `StreamWithTools` |

---

### Task 1: Partial Deduction SQL

**Files:**
- Modify: `internal/database/ai_credits.go`

The current `AtomicDeductCreditsUnchecked` is all-or-nothing — if `balance - cost < floor`, it deducts **zero**. We need a version that deducts as much as possible, clamping at the floor.

- [ ] **Step 1: Add `AtomicDeductCreditsToFloor` method**

In `internal/database/ai_credits.go`, add after the existing `AtomicDeductCreditsUnchecked` method (after line 91):

```go
// AtomicDeductCreditsToFloor atomically deducts credits, clamping at the floor
// instead of failing. If balance is already at or below floor, no deduction happens
// and returns (currentBalance, 0, nil). Otherwise deducts min(cost, balance-floor)
// and returns (newBalance, actualDeducted, nil).
func (db *Database) AtomicDeductCreditsToFloor(tx bun.IDB, balanceID string, cost int, floor int) (newBalance int, deducted int, err error) {
	err = tx.NewRaw(
		`UPDATE ai_credits_balances
		 SET balance = GREATEST(?, balance - ?), updated_at = NOW()
		 WHERE id = ? AND balance > ?
		 RETURNING balance`,
		floor, cost, balanceID, floor,
	).Scan(context.Background(), &newBalance)
	if err != nil {
		if err == sql.ErrNoRows {
			// Balance already at or below floor — nothing to deduct
			return 0, 0, nil
		}
		return 0, 0, err
	}
	// The old balance was (newBalance + actualDeducted), but we don't know it.
	// We know: newBalance = max(floor, oldBalance - cost)
	// actualDeducted = oldBalance - newBalance, but we need oldBalance.
	// Simpler: re-derive from the math. Since we clamped:
	//   if oldBalance - cost >= floor → newBalance = oldBalance - cost → deducted = cost
	//   if oldBalance - cost < floor  → newBalance = floor → deducted = oldBalance - floor
	// We can't get oldBalance from one statement, so just compute:
	// actualDeducted = cost if newBalance == (some_old - cost), else (some_old - floor).
	// Easier approach: do it in one query that returns both.
	return newBalance, 0, nil
}
```

Actually, that approach can't return the actual deducted amount cleanly. Let's use a CTE instead:

```go
// AtomicDeductCreditsToFloor atomically deducts credits, clamping at the floor
// instead of failing. Returns (newBalance, actualDeducted, nil).
// If balance is already at or below floor, returns (currentBalance, 0, nil).
func (db *Database) AtomicDeductCreditsToFloor(tx bun.IDB, balanceID string, cost int, floor int) (newBalance int, deducted int, err error) {
	var result struct {
		NewBalance int `bun:"new_balance"`
		Deducted   int `bun:"deducted"`
	}
	err = tx.NewRaw(
		`WITH old AS (
			SELECT id, balance FROM ai_credits_balances WHERE id = ? FOR UPDATE
		)
		UPDATE ai_credits_balances b
		SET balance = GREATEST(?, old.balance - ?), updated_at = NOW()
		FROM old
		WHERE b.id = old.id AND old.balance > ?
		RETURNING b.balance AS new_balance, (old.balance - b.balance) AS deducted`,
		balanceID, floor, cost, floor,
	).Scan(context.Background(), &result)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	return result.NewBalance, result.Deducted, nil
}
```

- [ ] **Step 2: Verify the file compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./internal/database/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/database/ai_credits.go
git commit -m "feat: add AtomicDeductCreditsToFloor for partial credit deduction"
```

---

### Task 2: Controller Credit Budget Methods

**Files:**
- Modify: `internal/controllers/ai_credits.go`

- [ ] **Step 1: Add `CreditBudget` method**

Add after the `TokensToCredits` method (after line 150 in `ai_credits.go`):

```go
// CreditBudget returns the maximum credits the user can spend in a single session.
// Budget = current balance - floor (floor is negative, so this is balance + |floor|).
// For example: balance=1, floor=-20 → budget=21.
func (c *Controller) CreditBudget(user *models.User) (budget int, err error) {
	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return 0, err
	}
	floor := c.RemoteConfig.GetInt(remoteconfig.AICreditsMinBalance, user.ID.String())
	budget = balance.Balance - floor
	if budget < 0 {
		budget = 0
	}
	return budget, nil
}
```

- [ ] **Step 2: Add `MakeCreditBudgetChecker` method**

Add directly after `CreditBudget`:

```go
// MakeCreditBudgetChecker returns a ShouldContinue-compatible function for
// StreamWithTools. It checks whether the accumulated credit cost in the tracker
// has reached the user's budget. When exhausted, it logs a warning and returns false.
func (c *Controller) MakeCreditBudgetChecker(user *models.User, tracker *utils.TokenTracker) func() bool {
	budget, err := c.CreditBudget(user)
	if err != nil {
		// Can't determine budget — allow generation to proceed (deduction will handle it)
		return nil
	}
	return func() bool {
		prompt, completion, _, _ := tracker.Totals()
		spent := c.TokensToCredits(prompt, completion)
		if spent >= budget {
			slog.Warn("credit budget exhausted mid-generation",
				"user_id", user.ID, "spent", spent, "budget", budget)
			return false
		}
		return true
	}
}
```

- [ ] **Step 3: Add `DeductAICreditsToFloor` method**

Add directly after `DeductAICreditsUnchecked` (after line 190):

```go
// DeductAICreditsToFloor deducts credits, clamping at the overdraft floor instead
// of failing. Always deducts something if balance is above floor. Used as the
// primary deduction method for generation sessions where the cost may exceed budget.
func (c *Controller) DeductAICreditsToFloor(user *models.User, cost int, action string, reference string) error {
	if cost <= 0 {
		return nil
	}
	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return err
	}

	var newBalance, deducted int
	txErr := c.DB.RunInTx(func(tx bun.Tx) error {
		floor := c.RemoteConfig.GetInt(remoteconfig.AICreditsMinBalance, user.ID.String())
		var err error
		newBalance, deducted, err = c.DB.AtomicDeductCreditsToFloor(tx, balance.ID.String(), cost, floor)
		if err != nil {
			return err
		}
		if deducted == 0 {
			return nil // already at floor, nothing to log
		}
		_, err = c.DB.CreateAICreditTransactionTx(tx, &models.AICreditTransaction{
			UserID:    user.ID,
			Amount:    -deducted,
			Action:    action,
			Reference: reference,
		})
		return err
	})

	if txErr == nil && deducted > 0 {
		c.TrackUserEvent(user, "ai_credits_deducted", posthog.NewProperties().
			Set("action", action).
			Set("cost", cost).
			Set("deducted", deducted).
			Set("remaining_balance", newBalance).
			Set("reference", reference))
		if deducted < cost {
			slog.Warn("partial credit deduction (clamped at floor)",
				"user_id", user.ID, "requested", cost, "deducted", deducted,
				"remaining", newBalance)
		}
	}

	return txErr
}
```

- [ ] **Step 4: Verify compilation**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./internal/controllers/...`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/ai_credits.go
git commit -m "feat: add CreditBudget, MakeCreditBudgetChecker, DeductAICreditsToFloor"
```

---

### Task 3: Wire Live Checking into Main Chat StreamWithTools

**Files:**
- Modify: `internal/controllers/app_builder_controller.go`

This is the main generation entry point. Two changes:
1. Replace `DeductAICreditsUnchecked` with `DeductAICreditsToFloor` in the defer
2. Add `ShouldContinue` to both chat `StreamWithTools` calls (main + retry)
3. Send an SSE `credits_exhausted` event when budget is hit

- [ ] **Step 1: Add credit checker setup after tracker creation**

In `StreamBuildApp`, after the tracker is created (around line 228, after `ctx = utils.WithTokenTracker(ctx, tracker)`), add:

```go
	// --- Credit budget checker (live mid-generation halt) ---
	creditChecker := c.MakeCreditBudgetChecker(user, tracker)
	creditExhausted := false
	shouldContinueCredits := func() bool {
		if creditChecker != nil && !creditChecker() {
			creditExhausted = true
			sendEvent("credits_exhausted", map[string]any{
				"message": "AI credits exhausted. Generation stopped.",
			})
			return false
		}
		return true
	}
```

- [ ] **Step 2: Replace the deduction in the defer block**

In `StreamBuildApp`, replace the deduction block in the defer (lines 241-245):

Old:
```go
			credits := c.TokensToCredits(prompt, completion)
			if err := c.DeductAICreditsUnchecked(user, credits, actionLabel, project.ID.String()); err != nil {
				slog.Warn("failed to deduct AI credits", "error", err,
					"user_id", user.ID, "credits", credits, "tokens", total)
			}
```

New:
```go
			credits := c.TokensToCredits(prompt, completion)
			if err := c.DeductAICreditsToFloor(user, credits, actionLabel, project.ID.String()); err != nil {
				slog.Warn("failed to deduct AI credits", "error", err,
					"user_id", user.ID, "credits", credits, "tokens", total)
			}
```

- [ ] **Step 3: Add ShouldContinue to main chat StreamWithTools**

In the main chat `StreamWithTools` call (line ~855 `ToolCallConfig`), add `ShouldContinue`:

```go
			utils.ToolCallConfig{
				MaxRounds:        10,
				NudgeAfter:       3,
				StripAfter:       5,
				MaxTokens:        65536,
				MaxContinuations: 3,
				ShouldContinue:   shouldContinueCredits,
				NudgeMessage:     "You have gathered enough context. Output your code changes now using <file> and <edit> blocks.",
```

- [ ] **Step 4: Add ShouldContinue to retry StreamWithTools**

In the retry `StreamWithTools` call (line ~1004 `ToolCallConfig`), add `ShouldContinue`:

```go
				utils.ToolCallConfig{
					MaxRounds:        3,
					NudgeAfter:       2,
					StripAfter:       3,
					MaxTokens:        65536,
					MaxContinuations: 1,
					ShouldContinue:   shouldContinueCredits,
					NudgeMessage:     "Output your corrected <edit> blocks now. Use the file contents you just read.",
```

- [ ] **Step 5: Verify compilation**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./internal/controllers/...`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/controllers/app_builder_controller.go
git commit -m "feat: wire live credit checking into chat StreamWithTools calls"
```

---

### Task 4: Wire Live Checking into Generation Paths

**Files:**
- Modify: `internal/controllers/app_builder_generation.go`
- Modify: `internal/controllers/app_builder_tools.go`

The generation has three sub-paths that make LLM calls:
1. **Single-shot** (`streamSingleShotGeneration`): `PlanAppFileStructure` + `StreamTextFromLLMWithMessages` — add budget gate between them
2. **E-commerce** (`streamEcommerceGeneration`): `PlanAppFileStructure` + `StreamWithTools` (setup) + `StreamTextFromLLMWithMessages` — add budget gate + ShouldContinue
3. **Pre-gen tools** (`runPreGenWebSearch` in `app_builder_tools.go`): `StreamWithTools` — cheap, but still wire for consistency
4. **Phase 1 chat tools** (`app_builder_tools.go:345`): `StreamWithTools` — wire ShouldContinue

Both `streamSingleShotGeneration` and `streamEcommerceGeneration` need access to the `shouldContinueCredits` function and the tracker. They already receive `user` as a parameter, and the tracker is in the context via `utils.WithTokenTracker`.

- [ ] **Step 1: Add budget gate to `streamSingleShotGeneration`**

In `internal/controllers/app_builder_generation.go`, in `streamSingleShotGeneration` (line ~275), after structure planning succeeds and before the expensive `StreamTextFromLLMWithMessages` call (line ~439), add a budget check. Insert after the `sendEvent("project_created", ...)` call (line ~353) and before the logo generation goroutine (line ~356):

```go
	// Budget gate — check if accumulated credits from planning + setup already
	// exceed the user's budget before starting the expensive code generation call.
	if tracker := utils.TokenTrackerFromContext(ctx); tracker != nil {
		prompt, completion, _, _ := tracker.Totals()
		spent := c.TokensToCredits(prompt, completion)
		budget, budgetErr := c.CreditBudget(user)
		if budgetErr == nil && spent >= budget {
			slog.Warn("credit budget exhausted before single-shot generation",
				"user_id", user.ID, "spent", spent, "budget", budget)
			sendEvent("credits_exhausted", map[string]any{
				"message": "AI credits exhausted. Generation stopped.",
			})
			return "", nil, fmt.Errorf("AI credits exhausted")
		}
	}
```

- [ ] **Step 2: Add budget gate to `streamEcommerceGeneration`**

In `internal/controllers/app_builder_generation.go`, in `streamEcommerceGeneration`, add the same budget gate after the Phase 1 setup `StreamWithTools` call completes (after line ~659, before the Phase 2 single-shot generation). Find the spot between Phase 1 completion and Phase 2 start, and add:

```go
	// Budget gate — check before expensive Phase 2 code generation
	if tracker := utils.TokenTrackerFromContext(ctx); tracker != nil {
		prompt, completion, _, _ := tracker.Totals()
		spent := c.TokensToCredits(prompt, completion)
		budget, budgetErr := c.CreditBudget(user)
		if budgetErr == nil && spent >= budget {
			slog.Warn("credit budget exhausted before ecommerce Phase 2",
				"user_id", user.ID, "spent", spent, "budget", budget)
			sendEvent("credits_exhausted", map[string]any{
				"message": "AI credits exhausted. Generation stopped.",
			})
			return "", ops, fmt.Errorf("AI credits exhausted")
		}
	}
```

Also add `ShouldContinue` to the ecommerce setup `StreamWithTools` config (line ~647):

```go
	setupConfig := utils.ToolCallConfig{
		MaxRounds:        c.RemoteConfig.GetInt(remoteconfig.AIGenerationMaxRounds, remoteconfig.Global),
		NudgeAfter:       5,
		StripAfter:       6,
		MaxTokens:        8192,
		MaxContinuations: 1,
		ShouldContinue: func() bool {
			if tracker := utils.TokenTrackerFromContext(ctx); tracker != nil {
				prompt, completion, _, _ := tracker.Totals()
				spent := c.TokensToCredits(prompt, completion)
				budget, budgetErr := c.CreditBudget(user)
				if budgetErr == nil && spent >= budget {
					return false
				}
			}
			return true
		},
		NudgeMessage:     "Finish populating the store and provide a brief summary.",
```

- [ ] **Step 3: Wire ShouldContinue into Phase 1 chat tools**

In `internal/controllers/app_builder_tools.go`, the `buildUnifiedChatTools` function is called from `app_builder_controller.go` which creates the tools and executor. The `StreamWithTools` call at line 345 in `runPreGenerationPhase1` (or similar) and line 744 in `runPreGenWebSearch` — both need `ShouldContinue`.

For the Phase 1 chat tools call at line 345, add ShouldContinue. This function receives `ctx` which has the tracker. Add to its `ToolCallConfig`:

Find the `StreamWithTools` call at `app_builder_tools.go:345` and its config. If it doesn't have a `ToolCallConfig`, add one with `ShouldContinue`. If it already has one, add the field.

Look for the config around line 345. The call is:
```go
result, err := utils.StreamWithTools(
    ctx, fastModel, messages,
    tools, provider,
    nil, nil,
    executor,
    nil,
    func() func(string) { ... }(),
    nil,
    // add config here if missing
)
```

If there's no config, add one as the last argument:
```go
    utils.ToolCallConfig{
        ShouldContinue: func() bool {
            if tracker := utils.TokenTrackerFromContext(ctx); tracker != nil {
                prompt, completion, _, _ := tracker.Totals()
                spent := c.TokensToCredits(prompt, completion)
                budget, budgetErr := c.CreditBudget(user)
                if budgetErr == nil && spent >= budget {
                    return false
                }
            }
            return true
        },
    },
```

- [ ] **Step 4: Verify compilation**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./internal/...`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/app_builder_generation.go internal/controllers/app_builder_tools.go
git commit -m "feat: add credit budget gates and ShouldContinue to all generation paths"
```

---

### Task 5: Manual Verification

- [ ] **Step 1: Verify the full build compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && make build`
Expected: binary created at `bin/app` with no errors

- [ ] **Step 2: Verify the fix addresses the original bug**

Trace the exact scenario from the bug report:
1. User has 1 credit, floor is -20, so budget = 21
2. `checkMinimumCredits`: balance 1 >= 1 → passes (unchanged)
3. Generation starts, `MakeCreditBudgetChecker` captures budget=21
4. After each `StreamWithTools` round, `shouldContinueCredits` checks `spent >= 21` → stops when exceeded
5. For single-shot: budget gate checks before the expensive call
6. Defer runs `DeductAICreditsToFloor(user, 45, ...)` → `GREATEST(-20, 1-45)` = -20 → deducts 21 credits
7. User balance: -20 (instead of still having 1 credit)

- [ ] **Step 3: Commit all changes**

```bash
git add -A
git commit -m "feat: live credit checking during generation + partial deduction to floor

- Add ShouldContinue credit budget checker to all StreamWithTools calls
- Add budget gate before expensive single-shot LLM calls
- Replace all-or-nothing DeductAICreditsUnchecked with DeductAICreditsToFloor
- Send credits_exhausted SSE event when budget hit mid-generation
- Fixes: generation consuming $1+ in LLM costs without deducting any credits"
```
