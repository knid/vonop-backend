package views

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/utils"
)

// resolveCreditOrgID extracts an optional ?org_id= from the request and
// resolves it through RequireOrgIDForUser, which verifies membership and
// falls back to the user's personal org when the query param is absent or
// unparseable. All credit endpoints route through this so a user viewing
// a workspace org sees that org's balance/history, not their personal
// org's leftover credits.
func (v *HttpViews) resolveCreditOrgID(r *http.Request, user *models.User) (uuid.UUID, error) {
	var reqOrgID *uuid.UUID
	if raw := r.URL.Query().Get("org_id"); raw != "" {
		if parsed, parseErr := uuid.Parse(raw); parseErr == nil {
			reqOrgID = &parsed
		}
	}
	return v.Controller.RequireOrgIDForUser(user, reqOrgID)
}

// buildCreditCostsMap converts the typed BuildCreditCosts table to a
// JSON-friendly string-keyed shape. Same shape used by the per-user
// /v1/users/subscription endpoint (controllers/subscriptions.go).
func buildCreditCostsMap() map[string]int {
	out := make(map[string]int, len(constants.BuildCreditCosts))
	for k, v := range constants.BuildCreditCosts {
		out[string(k)] = v
	}
	return out
}

func (v *HttpViews) GetBillingPlans(w http.ResponseWriter, r *http.Request) {
	// Best-effort: resolve the caller's personal org so we can compute
	// per-org intro eligibility. Anonymous callers (no auth) get IntroEligible=false.
	var personalOrg *models.Organization
	var authUser *models.User
	if user, err := v.Controller.GetUserFromReq(r); err == nil && user != nil {
		authUser = user
		if org, orgErr := v.Controller.DB.GetPersonalOrgByUserID(user.ID.String()); orgErr == nil {
			personalOrg = org
		}
	}

	// rc-starter-plan-enabled: when false the Starter plan is hidden from the
	// plans list so users must select a paid plan. Per-user evaluation supports
	// A/B testing; anonymous callers fall back to the global default.
	var starterEnabled bool
	if authUser != nil {
		starterEnabled = v.Controller.RemoteConfig.GetBool(remoteconfig.StarterPlanEnabled, authUser.ID.String())
	} else {
		starterEnabled = v.Controller.RemoteConfig.GetBool(remoteconfig.StarterPlanEnabled, remoteconfig.Global)
	}

	subs, err := v.Controller.DB.GetSubscriptions()
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	var plans []responses.PlanResponse
	for _, sub := range *subs {
		if sub.Name == "Starter" && !starterEnabled {
			continue
		}
		annualSavings := 0
		if sub.AnnualPrice > 0 && sub.MonthlyPrice > 0 {
			savings := (sub.MonthlyPrice * 12) - sub.AnnualPrice
			if savings > 0 {
				annualSavings = savings
			}
		}

		var planLimits responses.PlanLimitsResponse
		limits, limitsErr := v.Controller.DB.GetStandardUserLimitsById(sub.LimitsID.String())
		if limitsErr == nil {
			rc := v.Controller.RemoteConfig
			pn := strings.ToLower(sub.Name) // e.g. "starter", "hobby", "pro", "max"

			planLimits = responses.PlanLimitsResponse{
				// Credits
				MonthlyAICredits:     rc.GetInt(remoteconfig.PlanInt(pn, "monthly-ai-credits", limits.MonthlyAICredits), remoteconfig.Global),
				CreditRolloverMonths: rc.GetInt(remoteconfig.PlanInt(pn, "credit-rollover-months", limits.CreditRolloverMonths), remoteconfig.Global),
				BuildCreditCosts:     buildCreditCostsMap(),
				// Org / plan caps
				MaxSeats:               limits.MaxSeats,
				MaximumCredentialCount: limits.MaximumCredentialCount,
				// App project caps
				MaxWebApps:            rc.GetInt(remoteconfig.PlanInt(pn, "max-web-apps", limits.MaxWebApps), remoteconfig.Global),
				MaxMobileApps:         rc.GetInt(remoteconfig.PlanInt(pn, "max-mobile-apps", limits.MaxMobileApps), remoteconfig.Global),
				MaxDeployedWebApps:    rc.GetInt(remoteconfig.PlanInt(pn, "max-deployed-web-apps", limits.MaxDeployedWebApps), remoteconfig.Global),
				MaxDeployedMobileApps: rc.GetInt(remoteconfig.PlanInt(pn, "max-deployed-mobile-apps", limits.MaxDeployedMobileApps), remoteconfig.Global),
				// Build / notification quotas (legacy — credits gate now)
				MontlyBuildCount:        limits.MontlyBuildCount,
				MontlyNotificationCount: limits.MontlyNotificationCount,
				MonthlyOTAPublishCount:  limits.MonthlyOTAPublishCount,
				// Capability flags
				CanBuildToAndroid:  limits.CanBuildToAndroid,
				CanBuildToIOS:      limits.CanBuildToIOS,
				CanBuildMobile:     rc.GetBool(remoteconfig.PlanBool(pn, "can-build-mobile", limits.CanBuildMobile), remoteconfig.Global),
				CanDeployMobile:    rc.GetBool(remoteconfig.PlanBool(pn, "can-deploy-mobile", limits.CanDeployMobile), remoteconfig.Global),
				CanDeployWeb:       rc.GetBool(remoteconfig.PlanBool(pn, "can-deploy-web", limits.CanDeployWeb), remoteconfig.Global),
				CanUseAnalytics:    rc.GetBool(remoteconfig.PlanBool(pn, "can-use-analytics", limits.CanUseAnalytics), remoteconfig.Global),
				CanAccessCode:      rc.GetBool(remoteconfig.PlanBool(pn, "can-access-code", limits.CanAccessCode), remoteconfig.Global),
				CanBuyDomain:       rc.GetBool(remoteconfig.PlanBool(pn, "can-buy-domain", limits.CanBuyDomain), remoteconfig.Global),
				CanUseEmailService: rc.GetBool(remoteconfig.PlanBool(pn, "can-use-email-service", limits.CanUseEmailService), remoteconfig.Global),
				CanUseEcommerce:    rc.GetBool(remoteconfig.PlanBool(pn, "can-use-ecommerce", limits.CanUseEcommerce), remoteconfig.Global),
				CanExportToGitHub:  rc.GetBool(remoteconfig.PlanBool(pn, "can-export-to-github", limits.CanExportToGitHub), remoteconfig.Global),
				CanRemoveBrandLogo: rc.GetBool(remoteconfig.PlanBool(pn, "can-remove-brand-logo", limits.CanRemoveBrandLogo), remoteconfig.Global),
				CanChooseRegion:    limits.CanChooseRegion,
				// Fly machine spec
				DeployMemoryMB:     rc.GetInt(remoteconfig.PlanInt(pn, "deploy-memory-mb", limits.DeployMemoryMB), remoteconfig.Global),
				DeployCPUs:         limits.DeployCPUs,
				MinMachinesRunning: limits.MinMachinesRunning,
				MonthlyBandwidthGB: limits.MonthlyBandwidthGB,
				// Neon database quotas. Active-time + data-transfer override via
				// RC so we can tune sponsored-tier consumption without a re-seed;
				// data-transfer is int64 but RC stores Go int (64-bit on every
				// supported platform), so the cast is lossless for any plausible
				// byte count.
				DeployDBMaxCU:             limits.DeployDBMaxCU,
				DeployDBStorageMB:         rc.GetInt(remoteconfig.PlanInt(pn, "deploy-db-storage-mb", limits.DeployDBStorageMB), remoteconfig.Global),
				DeployDBActiveTimeSec:     rc.GetInt(remoteconfig.PlanInt(pn, "deploy-db-active-time-sec", limits.DeployDBActiveTimeSec), remoteconfig.Global),
				DeployDBDataTransferBytes: int64(rc.GetInt(remoteconfig.PlanInt(pn, "deploy-db-data-transfer-bytes", int(limits.DeployDBDataTransferBytes)), remoteconfig.Global)),
				DeployDBComputeTimeSec:    rc.GetInt(remoteconfig.PlanInt(pn, "deploy-db-compute-time-sec", limits.DeployDBComputeTimeSec), remoteconfig.Global),
				DeployDBSuspendTimeoutSec: rc.GetInt(remoteconfig.PlanInt(pn, "deploy-db-suspend-timeout-sec", limits.DeployDBSuspendTimeoutSec), remoteconfig.Global),
				DeployDBOrgTier:           limits.DeployDBOrgTier,
			}
		}

		planResp := responses.PlanResponse{
			ID:            sub.ID.String(),
			Name:          sub.Name,
			Description:   sub.Description,
			MonthlyPrice:  sub.MonthlyPrice,
			AnnualPrice:   sub.AnnualPrice,
			PriceID:       sub.PriceID,
			AnnualPriceID: sub.AnnualPriceID,
			AnnualSavings: annualSavings,
			Limits:        planLimits,
		}

		// Hobby intro offer: $9 first month for first-time orgs
		if sub.Name == "Hobby" {
			planResp.IntroPrice = 900
			planResp.IntroPriceMonths = 1
			if personalOrg != nil && !personalOrg.IntroDiscountUsed {
				planResp.IntroEligible = true
			}
		}

		plans = append(plans, planResp)
	}

	defaultRegion := ""
	if v.Controller.FlyService != nil {
		defaultRegion = v.Controller.FlyService.Region
	}

	utils.JSONResponse(w, responses.PlansListResponse{
		Plans:            plans,
		AvailableRegions: constants.FlyRegions,
		DefaultRegion:    defaultRegion,
	}, http.StatusOK)
}

func (v *HttpViews) GetAICreditsBalance(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID, err := v.resolveCreditOrgID(r, user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	balance, err := v.Controller.GetOrCreateCreditsBalance(user, orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, responses.AICreditsBalanceResponse{
		Balance:     balance.Balance,
		PeriodStart: balance.PeriodStart.Format("2006-01-02T15:04:05Z"),
		PeriodEnd:   balance.PeriodEnd.Format("2006-01-02T15:04:05Z"),
	}, http.StatusOK)
}

// GetCreditBreakdown returns the derived subscription vs purchased credit
// split. The backend stores a single balance column; this endpoint replays
// the ledger with a "drain subscription first" policy so the UI can show
// two separate buckets (and stop lying about "0 used" when the user has
// topped up past their monthly limit).
func (v *HttpViews) GetCreditBreakdown(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID, err := v.resolveCreditOrgID(r, user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	// Run the period-reset path first so expired periods roll over before we
	// derive the breakdown — otherwise the "used this period" counter would
	// be computed against a stale period_start.
	if _, err := v.Controller.GetOrCreateCreditsBalance(user, orgID); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	bd, err := v.Controller.ComputeCreditBreakdown(user, orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, responses.CreditBreakdownResponse{
		Balance:           bd.Balance,
		SubBalance:        bd.SubBalance,
		SubLimit:          bd.SubLimit,
		SubUsedThisPeriod: bd.SubUsedThisPeriod,
		PurchasedBalance:  bd.PurchasedBalance,
		PeriodStart:       bd.PeriodStart.Format("2006-01-02T15:04:05Z"),
		PeriodEnd:         bd.PeriodEnd.Format("2006-01-02T15:04:05Z"),
	}, http.StatusOK)
}

func (v *HttpViews) GetAICreditTransactions(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID, err := v.resolveCreditOrgID(r, user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	txs, err := v.Controller.DB.GetAICreditTransactions(orgID.String(), 50)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, txs, http.StatusOK)
}

func (v *HttpViews) GetCreditHistory(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID, err := v.resolveCreditOrgID(r, user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	page := 1
	pageSize := 20
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	orgIDStr := orgID.String()
	txs, total, err := v.Controller.DB.GetAICreditTransactionsPaginated(orgIDStr, page, pageSize)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	// Compute running balance_after for each transaction (newest first).
	// balance_after[i] = current_balance - sum(amounts of transactions newer than i).
	// Balance lookup is by orgID — the org's pool is the credit ledger's
	// ground truth post the 2026-04-30 migration. The deprecated user-id
	// query returned nondeterministic rows for users in multiple orgs.
	currentBalance := 0
	if bal, balErr := v.Controller.DB.GetAICreditsBalanceByOrg(orgIDStr); balErr == nil {
		currentBalance = bal.Balance
	}
	// For pages after 1, subtract the sum of newer transactions not on this page
	offset := (page - 1) * pageSize
	if offset > 0 {
		if newerSum, sumErr := v.Controller.DB.SumRecentTransactionAmounts(orgIDStr, offset); sumErr == nil {
			currentBalance -= newerSum
		}
	}

	transactions := make([]responses.CreditTransactionResponse, 0, len(txs))
	runningBalance := currentBalance
	for _, tx := range txs {
		// gateway_chat_pending records 0-amount placeholder rows for
		// fractional gateway debt (see AtomicAccumulateGatewayPending).
		// They have no display value in the history list and previously
		// rendered as misleading "Usage 0" rows; skip them client-side
		// rather than spend a category on a no-op.
		if tx.Amount == 0 && tx.Action == "gateway_chat_pending" {
			continue
		}
		transactions = append(transactions, responses.CreditTransactionResponse{
			ID:           tx.ID.String(),
			Type:         categorizeTxType(tx.Action, tx.Amount),
			Action:       tx.Action,
			Amount:       tx.Amount,
			BalanceAfter: runningBalance,
			CreatedAt:    tx.CreatedAt.Format("2006-01-02T15:04:05Z"),
			Description:  describeTxAction(tx.Action),
		})
		runningBalance -= tx.Amount
	}

	utils.JSONResponse(w, responses.CreditHistoryResponse{
		Transactions: transactions,
		Total:        total,
		Page:         page,
		PageSize:     pageSize,
	}, http.StatusOK)
}

func categorizeTxType(action string, amount int) string {
	lower := strings.ToLower(action)
	if strings.Contains(lower, "refund") {
		return "refund"
	}
	if strings.Contains(lower, "grant") || strings.Contains(lower, "allocation") || strings.Contains(lower, "bonus") {
		return "allocation"
	}
	if strings.Contains(lower, "purchase") || strings.Contains(lower, "purchased") {
		return "purchase"
	}
	if amount < 0 {
		return "usage"
	}
	if amount > 0 {
		return "allocation"
	}
	return "usage"
}

func describeTxAction(action string) string {
	descriptions := map[string]string{
		"generate_app":       "App generation",
		"generate":           "App generation",
		"app_chat":           "Chat edit",
		"chat":               "Chat edit",
		"screenshot_to_code": "Screenshot to code",
		"element_edit":       "Element edit",
		"generate_image":     "Image generation",
		"purchase":           "Credit purchase",
		"credits_purchased":  "Credit purchase",
		"monthly_grant":      "Monthly credit allocation",
		"initial_grant":      "Initial credit allocation",
		"refund":             "Credit refund",
	}
	if desc, ok := descriptions[action]; ok {
		return desc
	}
	words := strings.Split(action, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func (v *HttpViews) GetTokenUsage(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	entries, err := v.Controller.DB.GetLLMTokenUsageByUser(user.ID.String(), 100)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	since := time.Now().AddDate(0, -1, 0)
	promptTotal, completionTotal, totalTokens, err := v.Controller.DB.GetLLMTokenUsageSummary(user.ID.String(), since)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	var responseEntries []responses.TokenUsageEntry
	for _, e := range entries {
		responseEntries = append(responseEntries, responses.TokenUsageEntry{
			Action:           e.Action,
			Model:            e.Model,
			PromptTokens:     e.PromptTokens,
			CompletionTokens: e.CompletionTokens,
			TotalTokens:      e.TotalTokens,
			CallCount:        e.CallCount,
			Reference:        e.Reference,
			CreatedAt:        e.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	utils.JSONResponse(w, responses.TokenUsageResponse{
		Entries: responseEntries,
		Summary: responses.TokenUsageSummary{
			TotalPromptTokens:     promptTotal,
			TotalCompletionTokens: completionTotal,
			TotalTokens:           totalTokens,
		},
	}, http.StatusOK)
}
