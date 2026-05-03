//go:build integration

package controllers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// keystoreTestFixture wires a User + Organization + OrganizationMember (with
// the requested role) + AppProject for the keystore-download role tests.
// Tests share this so the only varying axis is the member's role.
type keystoreTestFixture struct {
	user    *models.User
	org     *models.Organization
	project *models.AppProject
}

func setupKeystoreFixture(t *testing.T, ctx context.Context, db *database.Database, role string) keystoreTestFixture {
	t.Helper()

	user := &models.User{
		ID: uuid.New(), Name: "U", Email: "u-" + uuid.NewString()[:8] + "@example.com", Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{
		ID: uuid.New(), Name: "Acme", Slug: "acme-" + uuid.NewString()[:8], OwnerID: user.ID,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID, UserID: user.ID, Role: role,
	}).Exec(ctx); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "test", Slug: "test-" + uuid.NewString()[:8],
		Prompt: "n/a", ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return keystoreTestFixture{user: user, org: org, project: project}
}

// keystoreTestModels lists the schema we need for every keystore-download
// integration test. Centralising it keeps the role-axis tests in lockstep
// with the original mints-OTP test.
func keystoreTestModels() []any {
	return []any{
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppProject)(nil),
		(*models.UserActionToken)(nil),
	}
}

// TestRequestKeystoreDownload_MintsOTPAndSendsEvent exercises the Request
// half of the OTP flow end-to-end:
//   - org-membership check passes for an org member
//   - a UserActionToken row of kind keystore_download is persisted
//   - the keystore_download_otp event is emitted via EventService
//
// We do NOT exercise Redeem here because it touches AndroidKeysStorage which
// is intentionally nil in NewTestController. The OTP-format unit tests in
// keystore_download_test.go cover the OTP-generation side; the dual-read
// fallback for the actual download is exercised in the E3 codemagic_keystore
// round-trip test.
func TestRequestKeystoreDownload_MintsOTPAndSendsEvent(t *testing.T) {
	db := database.NewTestDB(t, keystoreTestModels()...)
	c, fakes := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	fx := setupKeystoreFixture(t, ctx, db, "owner")
	user, org, project := fx.user, fx.org, fx.project

	if err := c.RequestKeystoreDownload(ctx, user, org.ID.String(), project.ID.String()); err != nil {
		t.Fatalf("request: %v", err)
	}

	// Verify a UserActionToken row was inserted for this user / kind.
	var token models.UserActionToken
	if err := db.DB.NewSelect().Model(&token).
		Where("user_id = ?", user.ID).
		Where("kind = ?", string(models.UserActionTokenKindKeystoreDownload)).
		Scan(ctx); err != nil {
		t.Fatalf("scan token: %v", err)
	}
	if token.Token == "" {
		t.Fatal("token.Token must not be empty")
	}
	// Stored token format is "{userID}:{projectID}:{otp}".
	if token.UserID != user.ID {
		t.Errorf("token.UserID = %v, want %v", token.UserID, user.ID)
	}
	if token.ExpiredAt.Before(time.Now()) {
		t.Errorf("token already expired: %v", token.ExpiredAt)
	}

	// EventService.SendEvent is fire-and-forget via `go ...`; poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fakes.Events.mu.Lock()
		n := len(fakes.Events.Calls)
		fakes.Events.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	fakes.Events.mu.Lock()
	calls := append([]fakeEventCall(nil), fakes.Events.Calls...)
	fakes.Events.mu.Unlock()

	if len(calls) == 0 {
		t.Fatal("expected EVENT_KEYSTORE_DOWNLOAD_OTP to be sent; got 0 events")
	}
	if calls[0].Event != constants.EVENT_KEYSTORE_DOWNLOAD_OTP {
		t.Errorf("event = %q, want %q", calls[0].Event, constants.EVENT_KEYSTORE_DOWNLOAD_OTP)
	}
	otp, _ := calls[0].Attributes["otp"].(string)
	if otp == "" {
		t.Fatal("expected non-empty 'otp' in event attributes")
	}
	if len(otp) != keystoreOTPLength {
		t.Errorf("OTP length = %d, want %d", len(otp), keystoreOTPLength)
	}
}

// requestKeystoreDownloadAccessDenied is the error string surfaced by
// RequireOrgAdminOrOwner for both "not a member" and "wrong role". Tests
// pin to this exact string so a regression that loosens the check (e.g.
// reverting to RequireOrgMembership, which would let a plain member through
// with no error at all) fails loudly.
const requestKeystoreDownloadAccessDenied = "organization not found or access denied"

// TestRequestKeystoreDownload_RejectsNonAdminMember asserts P0.4: a plain
// "member" role MUST NOT be able to mint a keystore-download OTP, even
// though they belong to the org. Pre-fix, RequireOrgMembership accepted any
// role and this exfiltration path was open to every collaborator the org
// owner invited (including agency contractors with read-only intent).
func TestRequestKeystoreDownload_RejectsNonAdminMember(t *testing.T) {
	db := database.NewTestDB(t, keystoreTestModels()...)
	c, fakes := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	fx := setupKeystoreFixture(t, ctx, db, "member")
	err := c.RequestKeystoreDownload(ctx, fx.user, fx.org.ID.String(), fx.project.ID.String())
	if err == nil {
		t.Fatal("RequestKeystoreDownload as plain member: want error, got nil")
	}
	if !strings.Contains(err.Error(), requestKeystoreDownloadAccessDenied) {
		t.Errorf("error = %q, want substring %q", err.Error(), requestKeystoreDownloadAccessDenied)
	}

	// No token row should be persisted on rejection.
	count, err := db.DB.NewSelect().Model((*models.UserActionToken)(nil)).
		Where("user_id = ?", fx.user.ID).
		Count(ctx)
	if err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if count != 0 {
		t.Errorf("UserActionToken rows = %d, want 0 (rejected request must not persist)", count)
	}

	// And no event should fire.
	time.Sleep(50 * time.Millisecond) // fire-and-forget guard
	fakes.Events.mu.Lock()
	calls := len(fakes.Events.Calls)
	fakes.Events.mu.Unlock()
	if calls != 0 {
		t.Errorf("EventService calls = %d, want 0 (rejected request must not emit event)", calls)
	}
}

// TestRequestKeystoreDownload_AllowsAdmin verifies the success path for the
// "admin" role: token persisted, OTP event emitted. Mirrors the owner case
// in MintsOTPAndSendsEvent but exercises the second allowed role.
func TestRequestKeystoreDownload_AllowsAdmin(t *testing.T) {
	db := database.NewTestDB(t, keystoreTestModels()...)
	c, fakes := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	fx := setupKeystoreFixture(t, ctx, db, "admin")
	if err := c.RequestKeystoreDownload(ctx, fx.user, fx.org.ID.String(), fx.project.ID.String()); err != nil {
		t.Fatalf("RequestKeystoreDownload as admin: %v", err)
	}

	var token models.UserActionToken
	if err := db.DB.NewSelect().Model(&token).
		Where("user_id = ?", fx.user.ID).
		Where("kind = ?", string(models.UserActionTokenKindKeystoreDownload)).
		Scan(ctx); err != nil {
		t.Fatalf("scan token: %v", err)
	}
	if token.Token == "" {
		t.Fatal("token.Token must not be empty")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fakes.Events.mu.Lock()
		n := len(fakes.Events.Calls)
		fakes.Events.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	fakes.Events.mu.Lock()
	calls := append([]fakeEventCall(nil), fakes.Events.Calls...)
	fakes.Events.mu.Unlock()
	if len(calls) == 0 || calls[0].Event != constants.EVENT_KEYSTORE_DOWNLOAD_OTP {
		t.Fatalf("expected EVENT_KEYSTORE_DOWNLOAD_OTP for admin caller; got %v", calls)
	}
}

// TestRequestKeystoreDownload_AllowsOwner is the third corner of the role
// matrix. The MintsOTPAndSendsEvent test already covers owner end-to-end
// behaviour; this thinner check just pins owner -> success so a regression
// that flips owner to denied fails fast.
func TestRequestKeystoreDownload_AllowsOwner(t *testing.T) {
	db := database.NewTestDB(t, keystoreTestModels()...)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	fx := setupKeystoreFixture(t, ctx, db, "owner")
	if err := c.RequestKeystoreDownload(ctx, fx.user, fx.org.ID.String(), fx.project.ID.String()); err != nil {
		t.Fatalf("RequestKeystoreDownload as owner: %v", err)
	}
}

// TestRedeemKeystoreDownload_RejectsNonAdminMember asserts the role check
// runs at redeem time too (defence in depth). Even if a plain member
// somehow obtained an OTP, RedeemKeystoreDownload must short-circuit on
// role BEFORE consuming the token or hitting the keystore S3 read.
//
// The fixture seeds a UserActionToken directly (bypassing the also-protected
// Request path) so we can isolate the redeem-side role check from the
// request-side one.
func TestRedeemKeystoreDownload_RejectsNonAdminMember(t *testing.T) {
	db := database.NewTestDB(t, keystoreTestModels()...)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	fx := setupKeystoreFixture(t, ctx, db, "member")

	// Pre-seed an OTP token as if Request had been called (it can't be — we
	// just verified that — but we want to prove redeem is independently
	// gated by role, not by "do you have a valid token").
	otp := "123456"
	tokenValue := fx.user.ID.String() + ":" + fx.project.ID.String() + ":" + otp
	if _, err := db.DB.NewInsert().Model(&models.UserActionToken{
		UserID:    fx.user.ID,
		Kind:      models.UserActionTokenKindKeystoreDownload,
		Token:     tokenValue,
		ExpiredAt: time.Now().Add(10 * time.Minute),
	}).Exec(ctx); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	_, err := c.RedeemKeystoreDownload(ctx, fx.user, fx.org.ID.String(), fx.project.ID.String(), otp)
	if err == nil {
		t.Fatal("RedeemKeystoreDownload as plain member: want error, got nil")
	}
	if !strings.Contains(err.Error(), requestKeystoreDownloadAccessDenied) {
		t.Errorf("error = %q, want substring %q", err.Error(), requestKeystoreDownloadAccessDenied)
	}

	// Token must NOT be consumed: the role check runs before
	// ConsumeUserActionToken, so a later admin redeem is still possible.
	var token models.UserActionToken
	if err := db.DB.NewSelect().Model(&token).
		Where("token = ?", tokenValue).
		Scan(ctx); err != nil {
		t.Fatalf("scan token: %v", err)
	}
	if token.IsConsumed() {
		t.Error("token must not be consumed by a denied redeem; role check should run before consume")
	}
}

// TestRedeemKeystoreDownload_AllowsAdmin verifies the admin role passes the
// redeem-side role check. The call still fails downstream — AndroidKeysStorage
// is nil in NewTestController and the project has no encrypted-credential row
// — but the failure must NOT be the role-denied error. Asserting "not access
// denied AND not nil" is the tightest signal we can get without standing up
// real S3 + KMS in this test.
func TestRedeemKeystoreDownload_AllowsAdmin(t *testing.T) {
	db := database.NewTestDB(t, keystoreTestModels()...)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	fx := setupKeystoreFixture(t, ctx, db, "admin")
	// No OTP minted — redeem will fail with ErrKeystoreOTPInvalid, which is
	// the expected post-RBAC failure mode and proves the role check passed.
	_, err := c.RedeemKeystoreDownload(ctx, fx.user, fx.org.ID.String(), fx.project.ID.String(), "000000")
	if err == nil {
		// Unexpected: with no token + no keystore, this should not succeed.
		t.Fatal("RedeemKeystoreDownload with no OTP unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), requestKeystoreDownloadAccessDenied) {
		t.Errorf("admin caller hit role-denied path: %v", err)
	}
}

// TestRedeemKeystoreDownload_AllowsOwner is the owner-role mirror of
// AllowsAdmin. Same downstream-failure expectation, opposite role.
func TestRedeemKeystoreDownload_AllowsOwner(t *testing.T) {
	db := database.NewTestDB(t, keystoreTestModels()...)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	fx := setupKeystoreFixture(t, ctx, db, "owner")
	_, err := c.RedeemKeystoreDownload(ctx, fx.user, fx.org.ID.String(), fx.project.ID.String(), "000000")
	if err == nil {
		t.Fatal("RedeemKeystoreDownload with no OTP unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), requestKeystoreDownloadAccessDenied) {
		t.Errorf("owner caller hit role-denied path: %v", err)
	}
}
