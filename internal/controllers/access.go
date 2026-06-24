package controllers

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
)

// ErrOrgInDunning is returned by credit-deduction paths when the org's
// subscription is in a payment-failed state. Views surface this as a 402 with
// a billing-portal pointer so the user can resolve their payment method.
var ErrOrgInDunning = errors.New("organization subscription is in dunning")

// ErrAICreditsFloorReached is returned by credit-deduction paths when the
// deduction would push the org's balance below the configured overdraft
// floor. Wrapped errors carry the floor value in the format "AI credits
// overdraft limit reached (floor: N)".
var ErrAICreditsFloorReached = errors.New("AI credits overdraft limit reached")

// MemberCreditCapError signals the actor has exceeded their per-member
// monthly credit cap. Carries the cap, period spend, and requested cost so
// views can render an actionable message and surface the gap to the UI.
type MemberCreditCapError struct {
	Cap       int
	Spent     int
	Requested int
}

func (e *MemberCreditCapError) Error() string {
	return fmt.Sprintf("member credit cap reached (cap=%d, spent=%d, requested=%d)", e.Cap, e.Spent, e.Requested)
}

// IsOrgInDunning reports whether an org's billing state should block
// credit-spend operations. Money-failed states (past_due, unpaid,
// incomplete_expired) and operator-paused (paused) all qualify; transient
// pre-confirmation (incomplete) does NOT — Stripe uses it briefly while a
// PaymentIntent confirms and the user has not actually failed to pay yet.
// Active/free/trial flow through unchanged.
func IsOrgInDunning(org *models.Organization) bool {
	if org == nil {
		return false
	}
	switch org.SubscriptionStatus {
	case "past_due", "unpaid", "incomplete_expired", "paused":
		return true
	}
	return false
}

// AuthorizeProjectAccess returns nil if the user is allowed to read or modify
// the given AppProject. Access is granted when either the legacy UserID match
// holds (personal-org / pre-org-billing rows) or the user is an active member
// of project.OrganizationID. Otherwise returns an "unauthorized" sentinel —
// callers wrap or surface their own error string but should not change the
// resulting HTTP status code.
func (c *Controller) AuthorizeProjectAccess(user *models.User, project *models.AppProject) error {
	if user == nil || project == nil {
		return errors.New("unauthorized")
	}
	if project.UserID == user.ID {
		return nil
	}
	if project.OrganizationID != uuid.Nil {
		if _, err := c.DB.GetOrganizationMember(project.OrganizationID.String(), user.ID.String()); err == nil {
			return nil
		}
	}
	return errors.New("unauthorized")
}

// -----------------------------------------------------------------------------
// Phase B: Granular organization permission system
// -----------------------------------------------------------------------------

// HasPermission reports whether the given member has the capability.
// If the member's Permissions slice is non-empty, it is treated as the
// source of truth (custom override). Otherwise we fall back to the
// role-derived defaults. This provides seamless backward compatibility
// for pre-Phase-B rows.
func HasPermission(member *models.OrganizationMember, perm string) bool {
	if member == nil {
		return false
	}
	perms := member.Permissions
	if len(perms) == 0 {
		perms = constants.DefaultPermissionsForRole(member.Role)
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// UserHasPermission is the high-level helper controllers/views use.
// It looks up membership (hiding existence on error) and checks the perm.
func (c *Controller) UserHasPermission(user *models.User, orgID, perm string) bool {
	if user == nil || orgID == "" || perm == "" {
		return false
	}
	member, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return false
	}
	return HasPermission(member, perm)
}

// RequirePermission is the org-scoped analog of RequireOrgAdminOrOwner.
// It verifies membership + that the caller holds the specific permission.
// It deliberately returns the same opaque error as RequireOrgMembership so
// existence is not leaked. Use for any action now governed by a capability.
func (c *Controller) RequirePermission(user *models.User, orgIDStr, perm string) (uuid.UUID, error) {
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		return uuid.Nil, errors.New("invalid organization id")
	}
	member, err := c.DB.GetOrganizationMember(orgIDStr, user.ID.String())
	if err != nil {
		return uuid.Nil, errors.New("organization not found or access denied")
	}
	if !HasPermission(member, perm) {
		return uuid.Nil, errors.New("organization not found or access denied")
	}
	return orgID, nil
}
