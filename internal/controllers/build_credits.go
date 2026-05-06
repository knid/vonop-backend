package controllers

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
)

// ChargeBuildCredits deducts the credit cost of a build action from the
// org's AI credit balance. Returns an error for unknown kinds; otherwise
// delegates to DeductAICreditsUnchecked, which lets balance go negative
// (matches existing AI-credit semantics — gating on insufficiency is the
// caller's job via CreditBudget if needed).
//
// reference is typically the Build.ID or Deployment.ID, used so the credit
// ledger row points back to the action that triggered the charge.
func (c *Controller) ChargeBuildCredits(user *models.User, orgID uuid.UUID, kind constants.BuildCreditKind, reference string) error {
	cost, ok := constants.BuildCreditCosts[kind]
	if !ok {
		return fmt.Errorf("unknown build credit kind: %q", kind)
	}
	return c.DeductAICreditsUnchecked(user, orgID, cost, string(kind), reference)
}
