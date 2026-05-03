//go:build integration

package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

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
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppProject)(nil),
		(*models.UserActionToken)(nil),
	)
	c, fakes := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	user := &models.User{
		ID: uuid.New(), Name: "U", Email: "u@example.com", Password: "x",
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
		OrganizationID: org.ID, UserID: user.ID, Role: "owner",
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
