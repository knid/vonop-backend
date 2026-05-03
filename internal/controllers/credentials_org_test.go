package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// These tests document the expected behaviour of the org-scoped credential
// methods added in T3. They are skipped until the test DB + org membership
// infra lands in P1.1.
//
// Intended semantics:
//   - GetAppleConnectionForOrg / GetGooglePlayConnectionForOrg must return
//     "organization not found or access denied" when the requesting user is
//     not a member of the given org.
//   - The same methods must succeed and return the credential when the user
//     IS a member of the owning org (even if they are not the creator).
//   - UpdateAppleConnectionForOrg must preserve the original UserID (creator);
//     the requesting user must not become the new owner.

func TestGetAppleConnectionForOrg_RejectsNonMember(t *testing.T) {
	t.Skip("requires test DB + org membership infra; TODO(P1.1)")

	// Silence "imported and not used" for scaffolding imports.
	_ = errors.New("sentinel")
	_ = context.Background()
	_ = uuid.New()
}

func TestGetAppleConnectionForOrg_AllowsOrgMember(t *testing.T) {
	t.Skip("requires test DB + org membership infra; TODO(P1.1)")
}

func TestUpdateAppleConnectionForOrg_PreservesOriginalUserID(t *testing.T) {
	t.Skip("requires test DB + org membership infra; TODO(P1.1)")
}

func TestGetGooglePlayConnectionForOrg_RejectsNonMember(t *testing.T) {
	t.Skip("requires test DB + org membership infra; TODO(P1.1)")
}

func TestGetGooglePlayConnectionForOrg_AllowsOrgMember(t *testing.T) {
	t.Skip("requires test DB + org membership infra; TODO(P1.1)")
}

func TestUpdateGooglePlayConnectionForOrg_PreservesOriginalUserID(t *testing.T) {
	t.Skip("requires test DB + org membership infra; TODO(P1.1)")
}
