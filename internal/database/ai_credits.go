package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) CreateAICreditsBalance(balance *models.AICreditsBalance) (*models.AICreditsBalance, error) {
	return db.CreateAICreditsBalanceTx(db.DB, balance)
}

// CreateAICreditsBalanceTx is the tx-aware variant of CreateAICreditsBalance.
func (db *Database) CreateAICreditsBalanceTx(tx bun.IDB, balance *models.AICreditsBalance) (*models.AICreditsBalance, error) {
	// The unique constraint on this table is ux_ai_credits_balances_org
	// (organization_id) — user_id is no longer unique since the org-scoped
	// migration on 2026-04-30. ON CONFLICT must match the existing constraint
	// or the INSERT errors out, leaving signups with no balance row.
	_, err := tx.NewInsert().Model(balance).On("CONFLICT (organization_id) DO NOTHING").Exec(context.Background())
	if err != nil {
		return nil, err
	}
	// Re-read to get the actual row (whether we inserted or it already existed)
	return db.GetAICreditsBalanceByOrgTx(tx, balance.OrganizationID.String())
}

func (db *Database) UpdateAICreditsBalance(balance *models.AICreditsBalance) (*models.AICreditsBalance, error) {
	balance.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().
		Model(balance).
		Where("id = ?", balance.ID).
		Exec(context.Background())
	return balance, err
}

func (db *Database) CreateAICreditTransaction(tx *models.AICreditTransaction) (*models.AICreditTransaction, error) {
	_, err := db.DB.NewInsert().Model(tx).Exec(context.Background())
	return tx, err
}

// GetAICreditTransactions returns the most recent transactions for an
// organization. Scoped to organization_id since the 2026-04-30 migration
// — ai_credits_balances is org-keyed and users in multiple orgs would
// otherwise see other orgs' transactions in their credit history.
func (db *Database) GetAICreditTransactions(orgID string, limit int) ([]models.AICreditTransaction, error) {
	var txs []models.AICreditTransaction
	err := db.DB.NewSelect().
		Model(&txs).
		Where("organization_id = ?", orgID).
		OrderExpr("created_at DESC").
		Limit(limit).
		Scan(context.Background())
	return txs, err
}

// GetAllAICreditTransactionsAsc returns every credit transaction for an org in
// chronological (ascending) order. Used for ledger replay — notably by
// ComputeCreditBreakdown, which derives the subscription-vs-purchased bucket
// split by walking the ledger forward. No pagination: we need the full ledger
// to reconcile against the ground-truth balance, and per-org ledgers stay
// small (a year of heavy use is < a few thousand rows).
func (db *Database) GetAllAICreditTransactionsAsc(orgID string) ([]models.AICreditTransaction, error) {
	return db.GetAllAICreditTransactionsAscTx(db.DB, orgID)
}

// GetAllAICreditTransactionsAscTx is the tx-aware variant of GetAllAICreditTransactionsAsc.
func (db *Database) GetAllAICreditTransactionsAscTx(tx bun.IDB, orgID string) ([]models.AICreditTransaction, error) {
	var txs []models.AICreditTransaction
	err := tx.NewSelect().
		Model(&txs).
		Where("organization_id = ?", orgID).
		OrderExpr("created_at ASC").
		Scan(context.Background())
	return txs, err
}

func (db *Database) GetAICreditTransactionsPaginated(orgID string, page, pageSize int) ([]models.AICreditTransaction, int, error) {
	var txs []models.AICreditTransaction
	offset := (page - 1) * pageSize

	total, err := db.DB.NewSelect().
		Model(&txs).
		Where("organization_id = ?", orgID).
		OrderExpr("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		ScanAndCount(context.Background())
	return txs, total, err
}

// SumRecentTransactionAmounts returns the sum of amounts for the N most recent
// transactions in an organization (by created_at DESC). Used to compute
// running balance_after in paginated history for the org-scoped credit pool.
func (db *Database) SumRecentTransactionAmounts(orgID string, limit int) (int, error) {
	var sum int
	err := db.DB.NewRaw(
		"SELECT COALESCE(SUM(amount), 0) FROM (SELECT amount FROM ai_credit_transactions WHERE organization_id = ? ORDER BY created_at DESC LIMIT ?) sub",
		orgID, limit,
	).Scan(context.Background(), &sum)
	return sum, err
}

// --- Atomic Credit Operations ---

// AtomicDeductCreditsUnchecked atomically deducts credits, allowing negative balance down to floor.
// Returns sql.ErrNoRows if deduction would push balance below floor.
func (db *Database) AtomicDeductCreditsUnchecked(tx bun.IDB, balanceID string, cost int, floor int) (int, error) {
	var newBalance int
	err := tx.NewRaw(
		"UPDATE ai_credits_balances SET balance = balance - ?, updated_at = NOW() WHERE id = ? AND balance - ? >= ? RETURNING balance",
		cost, balanceID, cost, floor,
	).Scan(context.Background(), &newBalance)
	return newBalance, err
}

// AtomicDeductCreditsToFloor atomically deducts credits, clamping at the floor
// instead of failing. Returns (newBalance, actualDeducted, nil).
// If balance is already at or below floor, returns (0, 0, nil).
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

// AtomicAddCredits atomically adds credits. Returns the new balance.
func (db *Database) AtomicAddCredits(tx bun.IDB, balanceID string, amount int) (int, error) {
	var newBalance int
	err := tx.NewRaw(
		"UPDATE ai_credits_balances SET balance = balance + ?, updated_at = NOW() WHERE id = ? RETURNING balance",
		amount, balanceID,
	).Scan(context.Background(), &newBalance)
	return newBalance, err
}

// AtomicAccumulateGatewayPending atomically adds `owed` fractional credits
// to pending_gateway, then (if the accumulator crosses an integer) moves
// the whole part out of pending_gateway and subtracts it from balance in
// the same statement. Returns the number of whole credits that rolled
// over — the caller uses this to emit an ai_credit_transactions row
// matching the existing DeductAICreditsToFloor audit trail. When the
// accumulator doesn't cross an integer, rolled is 0 and balance is
// untouched.
//
// Runs as one UPDATE with a CTE so concurrent gateway requests race
// correctly — each one sees the latest pending_gateway under row lock.
func (db *Database) AtomicAccumulateGatewayPending(organizationID uuid.UUID, owed float64) (rolled int, err error) {
	var result struct {
		Rolled int `bun:"rolled"`
	}
	err = db.DB.NewRaw(
		`WITH locked AS (
			SELECT id, pending_gateway FROM ai_credits_balances
			WHERE organization_id = ? FOR UPDATE
		),
		calc AS (
			SELECT
				id,
				pending_gateway + ?::double precision AS new_pending,
				FLOOR(pending_gateway + ?::double precision)::int AS rolled
			FROM locked
		)
		UPDATE ai_credits_balances b
		SET
			balance = b.balance - calc.rolled,
			pending_gateway = calc.new_pending - calc.rolled,
			updated_at = NOW()
		FROM calc
		WHERE b.id = calc.id
		RETURNING calc.rolled AS rolled`,
		organizationID, owed, owed,
	).Scan(context.Background(), &result)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return result.Rolled, nil
}

// AtomicResetPeriod atomically resets the billing period if it has expired.
// Returns (newBalance, true, nil) if the reset happened, or (0, false, nil) if
// the period was already current (another request won the race).
func (db *Database) AtomicResetPeriod(balanceID string, newBalance int, periodStart, periodEnd time.Time) (int, bool, error) {
	return db.AtomicResetPeriodTx(db.DB, balanceID, newBalance, periodStart, periodEnd)
}

// AtomicResetPeriodTx is the tx-aware variant of AtomicResetPeriod.
func (db *Database) AtomicResetPeriodTx(tx bun.IDB, balanceID string, newBalance int, periodStart, periodEnd time.Time) (int, bool, error) {
	var bal int
	err := tx.NewRaw(
		"UPDATE ai_credits_balances SET balance = ?, period_start = ?, period_end = ?, updated_at = NOW() WHERE id = ? AND period_end < NOW() RETURNING balance",
		newBalance, periodStart, periodEnd, balanceID,
	).Scan(context.Background(), &bal)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil // already reset by another request
		}
		return 0, false, err
	}
	return bal, true, nil
}

// CreateAICreditTransactionTx inserts a credit transaction within an existing DB transaction.
func (db *Database) CreateAICreditTransactionTx(tx bun.IDB, t *models.AICreditTransaction) (*models.AICreditTransaction, error) {
	_, err := tx.NewInsert().Model(t).Exec(context.Background())
	return t, err
}

// RunInTx executes fn inside a database transaction. If fn returns an error, the transaction is rolled back.
func (db *Database) RunInTx(fn func(tx bun.Tx) error) error {
	return db.DB.RunInTx(context.Background(), &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		return fn(tx)
	})
}

// HasUsageTransactions returns true if the user has any credit transactions
// other than system grants (initial_grant, monthly_grant, subscription_grant).
// Used to determine if the user has ever actively used their credits.
func (db *Database) HasUsageTransactions(userID string) (bool, error) {
	var count int
	err := db.DB.NewRaw(
		"SELECT COUNT(*) FROM ai_credit_transactions WHERE user_id = ? AND action NOT IN ('initial_grant', 'monthly_grant', 'subscription_grant')",
		userID,
	).Scan(context.Background(), &count)
	return count > 0, err
}

// HasInitialGrant returns true if the user has already received their
// one-time initial credit grant.
func (db *Database) HasInitialGrant(userID string) (bool, error) {
	var count int
	err := db.DB.NewRaw(
		"SELECT COUNT(*) FROM ai_credit_transactions WHERE user_id = ? AND action = 'initial_grant'",
		userID,
	).Scan(context.Background(), &count)
	return count > 0, err
}

// HasCreditPurchase returns true if the user has ever paid for a credit pack
// (any successfully-credited Stripe checkout — see webhook.go's
// `checkout.session.completed` handler, which writes action="purchase"). Used
// to gate first-purchase-only packs (e.g. the $3 onboarding micro-pack) so
// they only show up to users who have never paid before.
func (db *Database) HasCreditPurchase(userID string) (bool, error) {
	var count int
	err := db.DB.NewRaw(
		"SELECT COUNT(*) FROM ai_credit_transactions WHERE user_id = ? AND action = 'purchase'",
		userID,
	).Scan(context.Background(), &count)
	return count > 0, err
}

// HasAICreditTransactionByRefTx returns true if a credit transaction already
// exists for the given (action, reference) pair. Used as an idempotency guard
// for retryable callers (e.g. Stripe webhooks where the same event can fire
// twice). Reads inside the caller's tx so the check + subsequent insert are
// atomic.
func (db *Database) HasAICreditTransactionByRefTx(tx bun.IDB, action, reference string) (bool, error) {
	var count int
	err := tx.NewRaw(
		"SELECT COUNT(*) FROM ai_credit_transactions WHERE action = ? AND reference = ?",
		action, reference,
	).Scan(context.Background(), &count)
	return count > 0, err
}

// GetAICreditTransactionByActionRefsTx returns the first credit transaction
// matching action and any of the given candidate references. Used by the
// charge.refunded webhook to locate the original purchase row when the
// reference convention may be cs.ID, payment_intent.ID, or charge.ID.
// Returns sql.ErrNoRows if no match.
func (db *Database) GetAICreditTransactionByActionRefsTx(tx bun.IDB, action string, references []string) (*models.AICreditTransaction, error) {
	if len(references) == 0 {
		return nil, sql.ErrNoRows
	}
	var t models.AICreditTransaction
	err := tx.NewSelect().
		Model(&t).
		Where("action = ?", action).
		Where("reference IN (?)", bun.In(references)).
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// SumAICreditAmountsByActionRefPrefixTx returns the sum of `amount` across all
// credit transactions matching action with reference matching the given LIKE
// pattern. Used by the charge.refunded handler to compute cumulative prior
// refund debits on a charge (refund rows reference charge.ID:refund.ID, so
// the prefix is charge.ID:%).
func (db *Database) SumAICreditAmountsByActionRefPrefixTx(tx bun.IDB, action, refPrefix string) (int, error) {
	var sum int
	err := tx.NewRaw(
		"SELECT COALESCE(SUM(amount), 0) FROM ai_credit_transactions WHERE action = ? AND reference LIKE ?",
		action, refPrefix,
	).Scan(context.Background(), &sum)
	return sum, err
}

// --- LLM Token Usage ---

func (db *Database) CreateLLMTokenUsage(usage *models.LLMTokenUsage) error {
	_, err := db.DB.NewInsert().Model(usage).Exec(context.Background())
	return err
}

func (db *Database) GetLLMTokenUsageByUser(userID string, limit int) ([]models.LLMTokenUsage, error) {
	var entries []models.LLMTokenUsage
	err := db.DB.NewSelect().
		Model(&entries).
		Where("user_id = ?", userID).
		OrderExpr("created_at DESC").
		Limit(limit).
		Scan(context.Background())
	return entries, err
}

func (db *Database) GetLLMTokenUsageSummary(userID string, since time.Time) (prompt, completion, total, creditCost int, err error) {
	var result struct {
		Prompt     int `bun:"prompt"`
		Completion int `bun:"completion"`
		Total      int `bun:"total"`
		CreditCost int `bun:"credit_cost"`
	}
	err = db.DB.NewSelect().
		TableExpr("llm_token_usages").
		ColumnExpr("COALESCE(SUM(prompt_tokens), 0) AS prompt").
		ColumnExpr("COALESCE(SUM(completion_tokens), 0) AS completion").
		ColumnExpr("COALESCE(SUM(total_tokens), 0) AS total").
		ColumnExpr("COALESCE(SUM(credit_cost), 0) AS credit_cost").
		Where("user_id = ?", userID).
		Where("created_at >= ?", since).
		Scan(context.Background(), &result)
	return result.Prompt, result.Completion, result.Total, result.CreditCost, err
}

// GetLLMTokenUsageByProject returns the most recent LLM token usage
// entries for a given project (now prefers the typed project_id column;
// falls back to legacy reference string for older rows). If actionPrefix
// is non-empty, only entries whose action starts with it are returned —
// pass "gateway_" to exclude codegen traffic from the dashboard's per-app
// AI usage card.
func (db *Database) GetLLMTokenUsageByProject(projectID, actionPrefix string, limit int) ([]models.LLMTokenUsage, error) {
	var entries []models.LLMTokenUsage
	q := db.DB.NewSelect().
		Model(&entries).
		Where("(project_id = ? OR (project_id IS NULL AND reference = ?))", projectID, projectID)
	if actionPrefix != "" {
		q = q.Where("action LIKE ?", actionPrefix+"%")
	}
	err := q.
		OrderExpr("created_at DESC").
		Limit(limit).
		Scan(context.Background())
	return entries, err
}

// GetLLMTokenUsageSummaryByProject returns aggregate token totals for a
// project since the given time. Mirrors GetLLMTokenUsageSummary but
// scopes by project reference instead of user. If actionPrefix is
// non-empty, only entries whose action starts with it are aggregated.
// ListBalancesWithoutOrg returns AICreditsBalance rows where organization_id is NULL.
func (db *Database) ListBalancesWithoutOrg() ([]models.AICreditsBalance, error) {
	var rows []models.AICreditsBalance
	err := db.DB.NewSelect().Model(&rows).
		Where("organization_id IS NULL").
		Where("user_id IS NOT NULL").
		Scan(context.Background())
	return rows, err
}

// AssignBalanceOrg sets organization_id on a single AICreditsBalance row.
func (db *Database) AssignBalanceOrg(balanceID string, orgID uuid.UUID) error {
	_, err := db.DB.NewUpdate().
		Model((*models.AICreditsBalance)(nil)).
		Set("organization_id = ?", orgID).
		Where("id = ?", balanceID).
		Exec(context.Background())
	return err
}

// CountTransactionsWithoutOrg returns the count of ai_credit_transactions
// without organization_id (for backfill progress reporting).
func (db *Database) CountTransactionsWithoutOrg() (int, error) {
	count, err := db.DB.NewSelect().
		Model((*models.AICreditTransaction)(nil)).
		Where("organization_id IS NULL").
		Count(context.Background())
	return count, err
}

// BulkAssignTransactionOrgsFromPersonalOrg sets organization_id on all
// transactions to the personal org of the transaction's user_id, using a
// single SQL statement (faster than per-row updates for very large tables).
func (db *Database) BulkAssignTransactionOrgsFromPersonalOrg() (int, error) {
	res, err := db.DB.ExecContext(context.Background(), `
		UPDATE ai_credit_transactions
		SET organization_id = o.id
		FROM organizations o
		WHERE ai_credit_transactions.organization_id IS NULL
		  AND o.owner_id = ai_credit_transactions.user_id
		  AND o.is_personal = true
		  AND o.deleted_at IS NULL
	`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// GetAICreditsBalanceByOrg returns the balance for an organization. Returns
// sql.ErrNoRows if the org has not been backfilled or has never had a balance.
func (db *Database) GetAICreditsBalanceByOrg(orgID string) (*models.AICreditsBalance, error) {
	return db.GetAICreditsBalanceByOrgTx(db.DB, orgID)
}

// GetAICreditsBalanceByOrgTx is the tx-aware variant of GetAICreditsBalanceByOrg.
func (db *Database) GetAICreditsBalanceByOrgTx(tx bun.IDB, orgID string) (*models.AICreditsBalance, error) {
	var balance models.AICreditsBalance
	err := tx.NewSelect().Model(&balance).
		Where("organization_id = ?", orgID).
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &balance, nil
}

// MemberPeriodSpent returns the absolute value of credits a single member has
// spent within a period (sum of negative-amount transactions). Used for cap
// enforcement.
func (db *Database) MemberPeriodSpent(orgID, userID string, periodStart, periodEnd time.Time) (int, error) {
	var spent int
	err := db.DB.NewSelect().
		Model((*models.AICreditTransaction)(nil)).
		ColumnExpr("COALESCE(SUM(-amount), 0)").
		Where("organization_id = ?", orgID).
		Where("user_id = ?", userID).
		Where("amount < 0").
		Where("created_at >= ?", periodStart).
		Where("created_at < ?", periodEnd).
		Scan(context.Background(), &spent)
	return spent, err
}

// OrgUsageBreakdownRow is one row of "spend per member in this org for the period."
type OrgUsageBreakdownRow struct {
	UserID    uuid.UUID `bun:"user_id"`
	UserEmail string    `bun:"user_email"`
	UserName  string    `bun:"user_name"`
	Spent     int       `bun:"spent"`
}

// OrgUsageBreakdown returns each member's period-spend, sorted descending.
// Members with zero spend are still included (so admin can see caps for everyone).
func (db *Database) OrgUsageBreakdown(orgID string, periodStart, periodEnd time.Time) ([]OrgUsageBreakdownRow, error) {
	var rows []OrgUsageBreakdownRow
	err := db.DB.NewRaw(`
		SELECT
			m.user_id AS user_id,
			COALESCE(u.email, '') AS user_email,
			COALESCE(u.name, '') AS user_name,
			COALESCE((
				SELECT SUM(-t.amount)
				FROM ai_credit_transactions t
				WHERE t.organization_id = ?
				  AND t.user_id = m.user_id
				  AND t.amount < 0
				  AND t.created_at >= ?
				  AND t.created_at < ?
			), 0) AS spent
		FROM organization_members m
		LEFT JOIN users u ON u.id = m.user_id
		WHERE m.organization_id = ?
		  AND m.deleted_at IS NULL
		ORDER BY spent DESC, u.email ASC
	`, orgID, periodStart, periodEnd, orgID).Scan(context.Background(), &rows)
	return rows, err
}

func (db *Database) GetLLMTokenUsageSummaryByProject(projectID, actionPrefix string, since time.Time) (prompt, completion, total, callCount, creditCost int, err error) {
	var result struct {
		Prompt     int `bun:"prompt"`
		Completion int `bun:"completion"`
		Total      int `bun:"total"`
		Calls      int `bun:"calls"`
		CreditCost int `bun:"credit_cost"`
	}
	q := db.DB.NewSelect().
		TableExpr("llm_token_usages").
		ColumnExpr("COALESCE(SUM(prompt_tokens), 0) AS prompt").
		ColumnExpr("COALESCE(SUM(completion_tokens), 0) AS completion").
		ColumnExpr("COALESCE(SUM(total_tokens), 0) AS total").
		ColumnExpr("COALESCE(SUM(call_count), 0) AS calls").
		ColumnExpr("COALESCE(SUM(credit_cost), 0) AS credit_cost").
		Where("(project_id = ? OR (project_id IS NULL AND reference = ?))", projectID, projectID).
		Where("created_at >= ?", since)
	if actionPrefix != "" {
		q = q.Where("action LIKE ?", actionPrefix+"%")
	}
	err = q.Scan(context.Background(), &result)
	return result.Prompt, result.Completion, result.Total, result.Calls, result.CreditCost, err
}

// === 2026-05 Cost Attribution additions ===

// ProjectCreditSummary is the rollup for one project in a billing period.
// Used for agency client billing and "which app cost how much" views.
//
// ProjectName and ProjectSlug are populated via join (empty for deleted
// projects or pre-backfill legacy rows that cannot be resolved).
type ProjectCreditSummary struct {
	ProjectID     uuid.UUID `bun:"project_id"`
	ProjectName   string    `bun:"project_name"`
	ProjectSlug   string    `bun:"project_slug"`
	Spent         int       `bun:"spent"` // absolute value of negative amounts
	TxCount       int       `bun:"tx_count"`
	FirstSpendAt  time.Time `bun:"first_spend_at"`
	LastSpendAt   time.Time `bun:"last_spend_at"`
}

// GetProjectCreditSummary returns aggregate credit spend for a single project
// (prefers the new explicit project_id column; falls back to reference for
// historical rows written before the 2026-05 columns). Now also joins to
// surface project name/slug for more useful reporting payloads.
func (db *Database) GetProjectCreditSummary(orgID, projectID string, periodStart, periodEnd time.Time) (*ProjectCreditSummary, error) {
	var row ProjectCreditSummary
	err := db.DB.NewRaw(`
		WITH spends AS (
			SELECT
				COALESCE(project_id, reference::uuid) AS project_id,
				COALESCE(SUM(-amount), 0) AS spent,
				COUNT(*) AS tx_count,
				MIN(created_at) AS first_spend_at,
				MAX(created_at) AS last_spend_at
			FROM ai_credit_transactions
			WHERE organization_id = ?
			  AND amount < 0
			  AND created_at >= ?
			  AND created_at < ?
			  AND (project_id = ? OR (project_id IS NULL AND reference = ?))
			GROUP BY COALESCE(project_id, reference::uuid)
		)
		SELECT
			s.project_id,
			COALESCE(p.name, '') AS project_name,
			COALESCE(p.slug, '') AS project_slug,
			s.spent,
			s.tx_count,
			s.first_spend_at,
			s.last_spend_at
		FROM spends s
		LEFT JOIN app_projects p ON p.id = s.project_id
	`, orgID, periodStart, periodEnd, projectID, projectID).Scan(context.Background(), &row)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetOrgProjectCreditSummaries returns spend per project for an org in the
// given period (for admin "costs by client" reports). Sorted by spend desc.
// Project names are joined for richer, immediately-usable data (no extra
// roundtrips needed by callers).
func (db *Database) GetOrgProjectCreditSummaries(orgID string, periodStart, periodEnd time.Time, limit int) ([]ProjectCreditSummary, error) {
	var rows []ProjectCreditSummary
	err := db.DB.NewRaw(`
		WITH spends AS (
			SELECT
				COALESCE(project_id, reference::uuid) AS project_id,
				COALESCE(SUM(-amount), 0) AS spent,
				COUNT(*) AS tx_count,
				MIN(created_at) AS first_spend_at,
				MAX(created_at) AS last_spend_at
			FROM ai_credit_transactions
			WHERE organization_id = ?
			  AND amount < 0
			  AND created_at >= ?
			  AND created_at < ?
			GROUP BY COALESCE(project_id, reference::uuid)
			ORDER BY spent DESC
			LIMIT ?
		)
		SELECT
			s.project_id,
			COALESCE(p.name, '') AS project_name,
			COALESCE(p.slug, '') AS project_slug,
			s.spent,
			s.tx_count,
			s.first_spend_at,
			s.last_spend_at
		FROM spends s
		LEFT JOIN app_projects p ON p.id = s.project_id
		ORDER BY s.spent DESC
	`, orgID, periodStart, periodEnd, limit).Scan(context.Background(), &rows)
	return rows, err
}

// === Phase A high-value additions: action breakdown + per-run generation costs ===

// ProjectActionCreditBreakdownRow gives spend for a (project, action) pair.
// Enables "cost by project + action" visibility (e.g. codegen vs gateway vs edits).
type ProjectActionCreditBreakdownRow struct {
	ProjectID   uuid.UUID `bun:"project_id"`
	ProjectName string    `bun:"project_name"`
	ProjectSlug string    `bun:"project_slug"`
	Action      string    `bun:"action"`
	Spent       int       `bun:"spent"`
	TxCount     int       `bun:"tx_count"`
}

// GetOrgProjectActionCreditBreakdowns returns per-project-per-action credit
// spend within the period (for detailed agency cost attribution reports).
func (db *Database) GetOrgProjectActionCreditBreakdowns(orgID string, periodStart, periodEnd time.Time, limit int) ([]ProjectActionCreditBreakdownRow, error) {
	var rows []ProjectActionCreditBreakdownRow
	err := db.DB.NewRaw(`
		WITH spends AS (
			SELECT
				COALESCE(project_id, reference::uuid) AS project_id,
				action,
				COALESCE(SUM(-amount), 0) AS spent,
				COUNT(*) AS tx_count
			FROM ai_credit_transactions
			WHERE organization_id = ?
			  AND amount < 0
			  AND created_at >= ?
			  AND created_at < ?
			GROUP BY COALESCE(project_id, reference::uuid), action
			ORDER BY spent DESC
			LIMIT ?
		)
		SELECT
			s.project_id,
			COALESCE(p.name, '') AS project_name,
			COALESCE(p.slug, '') AS project_slug,
			s.action,
			s.spent,
			s.tx_count
		FROM spends s
		LEFT JOIN app_projects p ON p.id = s.project_id
		ORDER BY s.spent DESC
	`, orgID, periodStart, periodEnd, limit).Scan(context.Background(), &rows)
	return rows, err
}

// ExpensiveGeneration is a lightweight row for "top expensive generations"
// queries against the authoritative AppAgentRun cost records (CostUSD +
// CreditsUsed are written at run completion).
type ExpensiveGeneration struct {
	ID          uuid.UUID `bun:"id"`
	Status      string    `bun:"status"`
	CreditsUsed int       `bun:"credits_used"`
	CostUSD     float64   `bun:"cost_usd"`
	Model       string    `bun:"model"`
	DurationMs  int       `bun:"duration_ms"`
	CreatedAt   time.Time `bun:"created_at"`
}

// GetTopExpensiveGenerationsByProject returns the highest-credit runs for a
// project, ordered by credits_used desc (then recency). Ideal for spotting
// runaway prompts/generations per client app.
func (db *Database) GetTopExpensiveGenerationsByProject(projectID uuid.UUID, limit int) ([]ExpensiveGeneration, error) {
	var rows []ExpensiveGeneration
	err := db.DB.NewSelect().
		TableExpr("app_agent_runs").
		Column("id", "status", "credits_used", "cost_usd", "model", "duration_ms", "created_at").
		Where("app_project_id = ?", projectID).
		OrderExpr("credits_used DESC, created_at DESC").
		Limit(limit).
		Scan(context.Background(), &rows)
	return rows, err
}

// GetAgentRunCostSummaryByProject aggregates run-level costs for quick
// per-project generation cost rollup (distinct from tx ledger, useful for
// "what did the builder actually cost for this app?").
func (db *Database) GetAgentRunCostSummaryByProject(projectID uuid.UUID, since time.Time) (totalCredits int, totalUSD float64, runCount int, err error) {
	var result struct {
		Credits int     `bun:"credits"`
		USD     float64 `bun:"usd"`
		Count   int     `bun:"count"`
	}
	err = db.DB.NewSelect().
		TableExpr("app_agent_runs").
		ColumnExpr("COALESCE(SUM(credits_used), 0) AS credits").
		ColumnExpr("COALESCE(SUM(cost_usd), 0) AS usd").
		ColumnExpr("COUNT(*) AS count").
		Where("app_project_id = ?", projectID).
		Where("created_at >= ?", since).
		Scan(context.Background(), &result)
	return result.Credits, result.USD, result.Count, err
}
