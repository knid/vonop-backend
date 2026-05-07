//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils/external"
)

// TestSendComposePush_HappyPath exercises the full compose-push flow:
//  1. Provisioning kicks in lazily (no MobileAppConfig.OneSignalAppID yet).
//  2. OneSignal /apps + /auth/tokens + /notifications fakes return success.
//  3. The encrypted REST key round-trips through CredStore.
//  4. Persists a ComposePushHistory audit-log row.
//  5. Returns the OneSignal notification ID + recipients count.
func TestSendComposePush_HappyPath_PersistsHistory(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
		(*models.ComposePushHistory)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{
		Secrets: map[string]string{
			"ONESIGNAL_ORG_ID":                "x",
			"ONESIGNAL_ORG_API_KEY":           "y",
			"FCM_V1_SERVICE_ACCOUNT_JSON_B64": "",
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/apps" && r.Method == "POST":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "os-app-1", "name": "x"})
		case strings.HasSuffix(r.URL.Path, "/auth/tokens") && r.Method == "POST":
			_ = json.NewEncoder(w).Encode(map[string]string{"formatted_token": "rest-key-1"})
		case r.URL.Path == "/notifications" && r.Method == "POST":
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), `"app_id":"os-app-1"`) {
				t.Errorf("notif body missing app_id: %s", string(b))
			}
			if r.Header.Get("Authorization") != "Basic rest-key-1" {
				t.Errorf("notif auth: got %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "notif-id-99", "recipients": 7})
		default:
			t.Logf("unexpected OS call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	restore := external.SetOneSignalTestEndpoint(server.URL)
	defer restore()

	ctx := context.Background()
	user := &models.User{ID: uuid.New(), Name: "Sender", Email: "sender-" + uuid.NewString()[:8] + "@example.com", Password: "x"}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "O", Slug: "org-cp-" + uuid.NewString()[:8], OwnerID: user.ID, IsPersonal: true}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	mem := &models.OrganizationMember{OrganizationID: org.ID, UserID: user.ID, Role: "owner"}
	if _, err := db.DB.NewInsert().Model(mem).Exec(ctx); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "X", Slug: "x-" + uuid.NewString()[:8], Prompt: "x",
		ProjectType: models.ProjectTypeMobileAppNative,
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	resp, err := c.SendComposePush(ctx, project, user, requests.ComposePushRequest{
		AudienceType: "segments",
		Title:        map[string]string{"en": "Hello"},
		Body:         map[string]string{"en": "World"},
	})
	if err != nil {
		t.Fatalf("SendComposePush: %v", err)
	}
	if resp.NotificationID != "notif-id-99" || resp.RecipientsCount != 7 {
		t.Errorf("response mismatch: %+v", resp)
	}

	hist, total, err := c.DB.ListComposePushHistory(project.ID.String(), 10, 0)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if total != 1 || len(hist) != 1 {
		t.Fatalf("expected 1 history row, got total=%d len=%d", total, len(hist))
	}
	if hist[0].SenderUserID != user.ID {
		t.Errorf("sender id: got %v, want %v", hist[0].SenderUserID, user.ID)
	}
	if hist[0].Title != "Hello" || hist[0].Body != "World" {
		t.Errorf("title/body mismatch: %+v", hist[0])
	}
	if hist[0].OneSignalNotificationID != "notif-id-99" {
		t.Errorf("os notif id: %s", hist[0].OneSignalNotificationID)
	}
	if hist[0].RecipientsCount != 7 {
		t.Errorf("recipients: %d", hist[0].RecipientsCount)
	}
}

// TestSendComposePush_NonOrgMember_Rejected verifies that a user not in the
// project's organization can't send a push (org-scope auth check).
func TestSendComposePush_NonOrgMember_Rejected(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppProject)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})

	ctx := context.Background()
	owner := &models.User{ID: uuid.New(), Name: "Owner", Email: "owner-" + uuid.NewString()[:8] + "@example.com", Password: "x"}
	stranger := &models.User{ID: uuid.New(), Name: "Stranger", Email: "stranger-" + uuid.NewString()[:8] + "@example.com", Password: "x"}
	if _, err := db.DB.NewInsert().Model(owner).Exec(ctx); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(stranger).Exec(ctx); err != nil {
		t.Fatalf("insert stranger: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "O", Slug: "org-deny-" + uuid.NewString()[:8], OwnerID: owner.ID, IsPersonal: true}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{OrganizationID: org.ID, UserID: owner.ID, Role: "owner"}).Exec(ctx); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	project := &models.AppProject{
		ID: uuid.New(), UserID: owner.ID, OrganizationID: org.ID,
		Name: "Y", Slug: "y-" + uuid.NewString()[:8], Prompt: "x",
		ProjectType: models.ProjectTypeMobileAppNative,
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	_, err := c.SendComposePush(ctx, project, stranger, requests.ComposePushRequest{
		AudienceType: "segments",
		Title:        map[string]string{"en": "h"},
		Body:         map[string]string{"en": "b"},
	})
	if err == nil {
		t.Fatalf("expected access-denied error, got nil")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "access") && !strings.Contains(low, "not found") {
		t.Fatalf("expected access-denied/not-found error, got %v", err)
	}
}
