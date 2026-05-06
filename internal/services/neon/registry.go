package neon

import (
	"fmt"
	"log/slog"
)

// Tier names used as map keys and stored on AppIntegration.Config["org_tier"].
// Keep these in sync with StandardUserLimits.DeployDBOrgTier values.
const (
	TierFree = "free"
	TierPaid = "paid"
)

// Registry routes Neon API calls to the right org-scoped service based on a
// project's tier. The agent plan runs with two orgs — Free (Neon-sponsored)
// and Paid (metered) — plus a personal API key for cross-org transfers.
//
// In single-org mode (only NEON_API_KEY set, no FREE/PAID split) the same
// service is registered under both tier keys and TransferKey is empty so
// transfers no-op gracefully.
type Registry struct {
	services    map[string]*Service
	orgIDs      map[string]string
	TransferKey string // personal API key used for cross-org transfers
}

// NewRegistry builds a Registry. Pass nil services to omit a tier; at least
// one tier must be present or the registry returns an error.
func NewRegistry(free, paid *Service, transferKey string) (*Registry, error) {
	if free == nil && paid == nil {
		return nil, fmt.Errorf("registry requires at least one Neon service")
	}
	r := &Registry{
		services:    make(map[string]*Service, 2),
		orgIDs:      make(map[string]string, 2),
		TransferKey: transferKey,
	}
	if free != nil {
		r.services[TierFree] = free
		r.orgIDs[TierFree] = free.OrgID
	}
	if paid != nil {
		r.services[TierPaid] = paid
		r.orgIDs[TierPaid] = paid.OrgID
	}
	// Single-org fallback: if only one tier is wired, route the other
	// tier to the same service so callers don't need to special-case
	// missing keys.
	if free == nil && paid != nil {
		r.services[TierFree] = paid
		r.orgIDs[TierFree] = paid.OrgID
		slog.Warn("Neon registry: free tier falls back to paid org (no NEON_FREE_ORG_API_KEY set)")
	}
	if paid == nil && free != nil {
		r.services[TierPaid] = free
		r.orgIDs[TierPaid] = free.OrgID
		slog.Warn("Neon registry: paid tier falls back to free org (no NEON_PAID_ORG_API_KEY set)")
	}
	return r, nil
}

// For returns the org-scoped service for a tier. Unknown tiers fall back to
// the paid tier so a misconfigured DB row doesn't break provisioning.
func (r *Registry) For(tier string) *Service {
	if r == nil {
		return nil
	}
	if svc, ok := r.services[tier]; ok {
		return svc
	}
	if svc, ok := r.services[TierPaid]; ok {
		return svc
	}
	return r.services[TierFree]
}

// OrgID returns the Neon organization ID for a tier. Empty when the tier
// is unknown or the service was constructed without an org ID (personal key).
func (r *Registry) OrgID(tier string) string {
	if r == nil {
		return ""
	}
	return r.orgIDs[tier]
}

// CanTransfer reports whether cross-org transfers are configured. False when
// the personal API key is missing — callers should skip transfer attempts.
func (r *Registry) CanTransfer() bool {
	return r != nil && r.TransferKey != ""
}

// Transfer moves a project to the destination tier's org. Returns nil with
// no API call when source == dest.
func (r *Registry) Transfer(projectID, fromTier, toTier string) error {
	if r == nil {
		return fmt.Errorf("nil registry")
	}
	if fromTier == toTier {
		return nil
	}
	if !r.CanTransfer() {
		return fmt.Errorf("transfer not configured: NEON_PERSONAL_API_KEY missing")
	}
	targetOrg := r.OrgID(toTier)
	if targetOrg == "" {
		return fmt.Errorf("transfer target tier %q has no org id", toTier)
	}
	return TransferProject(r.TransferKey, projectID, targetOrg)
}
