package controllers

import (
	"crypto/subtle"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// fakeStaticSecretManager is a tiny secret.SecretManager that returns a single
// canned value for REVENUECAT_WEBHOOK_AUTH_TOKEN; everything else is "".
// Defined here to avoid the //go:build integration constraint on
// fakeSecretManager.
type fakeStaticSecretManager struct{ values map[string]string }

func (f *fakeStaticSecretManager) GetSecret(k string) string { return f.values[k] }
func (f *fakeStaticSecretManager) GetEnv() string            { return "test" }

// TestRCEvent_MobileSubscriptionFromEvent_InitialPurchase verifies the
// event-to-row mapper for a typical INITIAL_PURCHASE shape.
func TestRCEvent_MobileSubscriptionFromEvent_InitialPurchase(t *testing.T) {
	projectID := uuid.New()
	orgID := uuid.New()
	purchasedMs := time.Now().Add(-time.Hour).UnixMilli()
	expirationMs := time.Now().Add(30 * 24 * time.Hour).UnixMilli()
	evt := RCEventV2{
		Event: RCEventV2Event{
			Type:                      RCEventInitialPurchase,
			AppID:                     "rc-proj-1",
			AppUserID:                 "user-uuid-1",
			ProductID:                 "pro_monthly",
			EntitlementID:             "pro",
			Store:                     "APP_STORE",
			PurchasedAtMs:             purchasedMs,
			ExpirationAtMs:            expirationMs,
			CountryCode:               "US",
			Currency:                  "USD",
			PriceInCurrency:           9.99,
			IsTrialPeriod:             false,
			PeriodType:                "NORMAL",
		},
	}
	row := mobileSubscriptionFromEvent(evt, projectID, orgID, models.MobileSubscriptionStatusActive)
	if row == nil {
		t.Fatalf("expected non-nil row")
	}
	if row.AppProjectID != projectID {
		t.Errorf("AppProjectID: got %s want %s", row.AppProjectID, projectID)
	}
	if row.OrganizationID != orgID {
		t.Errorf("OrgID mismatch: %s", row.OrganizationID)
	}
	if row.RCAppUserID != "user-uuid-1" {
		t.Errorf("RCAppUserID: got %q", row.RCAppUserID)
	}
	if row.ProductID != "pro_monthly" {
		t.Errorf("ProductID: got %q", row.ProductID)
	}
	if row.EntitlementID != "pro" {
		t.Errorf("EntitlementID: got %q", row.EntitlementID)
	}
	if row.Status != models.MobileSubscriptionStatusActive {
		t.Errorf("Status: got %q", row.Status)
	}
	if row.Store != "app_store" {
		t.Errorf("Store: got %q", row.Store)
	}
	if row.PriceCents != 999 {
		t.Errorf("PriceCents: got %d, want 999", row.PriceCents)
	}
	if row.Currency != "USD" {
		t.Errorf("Currency: %q", row.Currency)
	}
	if row.IsTrial {
		t.Errorf("IsTrial: got true, want false")
	}
	if row.IsLifetime {
		t.Errorf("IsLifetime: got true, want false")
	}
}

// TestRCEvent_MobileSubscriptionFromEvent_NonRenewing covers
// non-renewing purchases (= IsLifetime=true).
func TestRCEvent_MobileSubscriptionFromEvent_NonRenewing(t *testing.T) {
	evt := RCEventV2{
		Event: RCEventV2Event{
			Type:            RCEventNonRenewingPurchase,
			AppID:           "rc-proj-1",
			AppUserID:       "user-uuid-2",
			ProductID:       "lifetime_unlock",
			Store:           "PLAY_STORE",
			PriceInCurrency: 19.99,
			Currency:        "USD",
		},
	}
	row := mobileSubscriptionFromEvent(evt, uuid.New(), uuid.New(), models.MobileSubscriptionStatusNonRenewing)
	if row == nil {
		t.Fatalf("expected non-nil row")
	}
	if !row.IsLifetime {
		t.Errorf("IsLifetime: got false, want true")
	}
	if row.Status != models.MobileSubscriptionStatusNonRenewing {
		t.Errorf("Status: got %q", row.Status)
	}
	if row.Store != "play_store" {
		t.Errorf("Store: got %q", row.Store)
	}
}

// TestRCEvent_MobileSubscriptionFromEvent_Trial covers trial detection.
func TestRCEvent_MobileSubscriptionFromEvent_Trial(t *testing.T) {
	evt := RCEventV2{Event: RCEventV2Event{
		Type:          RCEventInitialPurchase,
		AppUserID:     "u",
		ProductID:     "p",
		PeriodType:    "TRIAL",
	}}
	row := mobileSubscriptionFromEvent(evt, uuid.New(), uuid.New(), models.MobileSubscriptionStatusActive)
	if row == nil || !row.IsTrial {
		t.Fatalf("expected trial=true row")
	}

	evt2 := RCEventV2{Event: RCEventV2Event{
		Type:          RCEventInitialPurchase,
		AppUserID:     "u",
		ProductID:     "p",
		IsTrialPeriod: true,
	}}
	row2 := mobileSubscriptionFromEvent(evt2, uuid.New(), uuid.New(), models.MobileSubscriptionStatusActive)
	if row2 == nil || !row2.IsTrial {
		t.Fatalf("expected trial=true via IsTrialPeriod flag")
	}
}

// TestRCEvent_MobileSubscriptionFromEvent_MissingFields returns nil for
// events without app_user_id or product_id.
func TestRCEvent_MobileSubscriptionFromEvent_MissingFields(t *testing.T) {
	cases := []RCEventV2{
		{Event: RCEventV2Event{Type: RCEventInitialPurchase, ProductID: "p"}},     // no app_user_id
		{Event: RCEventV2Event{Type: RCEventInitialPurchase, AppUserID: "u"}},     // no product_id
		{Event: RCEventV2Event{Type: RCEventInitialPurchase}},                     // neither
	}
	for i, evt := range cases {
		got := mobileSubscriptionFromEvent(evt, uuid.New(), uuid.New(), "active")
		if got != nil {
			t.Errorf("case %d: expected nil row for missing keys, got %+v", i, got)
		}
	}
}

// TestRCEvent_NormaliseRCStore covers all store mappings.
func TestRCEvent_NormaliseRCStore(t *testing.T) {
	cases := map[string]string{
		"APP_STORE":     "app_store",
		"app_store":     "app_store",
		"PLAY_STORE":    "play_store",
		"MAC_APP_STORE": "play_store",
		"STRIPE":        "stripe",
		"AMAZON":        "amazon",
		"PROMOTIONAL":   "promotional",
		"":              "",
		"weird_thing":   "weird_thing",
	}
	for in, want := range cases {
		if got := normaliseRCStore(in); got != want {
			t.Errorf("normaliseRCStore(%q): got %q, want %q", in, got, want)
		}
	}
}

// TestRCEvent_NormaliseRCProductID picks NewProductID for product_change.
func TestRCEvent_NormaliseRCProductID(t *testing.T) {
	if got := normaliseRCProductID(RCEventV2{Event: RCEventV2Event{ProductID: "a"}}); got != "a" {
		t.Errorf("primary: %q", got)
	}
	if got := normaliseRCProductID(RCEventV2{Event: RCEventV2Event{NewProductID: "b"}}); got != "b" {
		t.Errorf("fallback: %q", got)
	}
	if got := normaliseRCProductID(RCEventV2{Event: RCEventV2Event{ProductID: "a", NewProductID: "b"}}); got != "a" {
		t.Errorf("primary wins: %q", got)
	}
}

// TestRCEvent_PriceCentsFromEvent rounds and falls back across price fields.
func TestRCEvent_PriceCentsFromEvent(t *testing.T) {
	if got := priceCentsFromEvent(RCEventV2{Event: RCEventV2Event{PriceInCurrency: 9.99}}); got != 999 {
		t.Errorf("primary: %d", got)
	}
	if got := priceCentsFromEvent(RCEventV2{Event: RCEventV2Event{PriceUSD: 4.50}}); got != 450 {
		t.Errorf("USD fallback: %d", got)
	}
	if got := priceCentsFromEvent(RCEventV2{Event: RCEventV2Event{Price: 19.95}}); got != 1995 {
		t.Errorf("Price fallback: %d", got)
	}
	if got := priceCentsFromEvent(RCEventV2{Event: RCEventV2Event{}}); got != 0 {
		t.Errorf("empty: %d", got)
	}
}

// TestRCWebhook_VerifyAuth_ConstantTime exercises both happy + sad path of the
// shared-secret bearer verifier.
func TestRCWebhook_VerifyAuth_ConstantTime(t *testing.T) {
	c := &Controller{
		SecretManager: &fakeStaticSecretManager{values: map[string]string{
			"REVENUECAT_WEBHOOK_AUTH_TOKEN": "supersecret123",
		}},
	}
	cases := []struct {
		name    string
		header  string
		wantErr bool
	}{
		{"correct", "Bearer supersecret123", false},
		{"wrong", "Bearer supersecret124", true},
		{"missing bearer prefix", "supersecret123", false}, // trimmed accepts raw value too
		{"empty", "", true},
		{"only bearer", "Bearer ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.VerifyRCWebhookAuth(tc.header)
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestRCWebhook_VerifyAuth_UnsetSecret rejects everything when the secret
// isn't configured (we'd rather 401 in dev than accept unauthenticated POSTs).
func TestRCWebhook_VerifyAuth_UnsetSecret(t *testing.T) {
	c := &Controller{
		SecretManager: &fakeStaticSecretManager{values: map[string]string{}},
	}
	if err := c.VerifyRCWebhookAuth("Bearer anything"); err == nil {
		t.Fatalf("expected unauthenticated when secret unset, got nil")
	}
}

// TestConstantTimeStringsEqual sanity-checks the helper used by
// VerifyRCWebhookAuth — pure correctness, not timing.
func TestConstantTimeStringsEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true},
		{"abc", "abcd", false},
		{"", "", true},
		{"abc", "abd", false},
	}
	for _, tc := range cases {
		if got := constantTimeStringsEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("ct(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
		// And a sanity check that subtle.ConstantTimeCompare would agree.
		want01 := subtle.ConstantTimeCompare([]byte(tc.a), []byte(tc.b))
		if (want01 == 1) != tc.want {
			t.Errorf("subtle disagrees with our ct(%q,%q)", tc.a, tc.b)
		}
	}
}

// TestHandleRCEvent_UnknownTypeAcked verifies HandleRCEvent doesn't error on
// unknown event types — the webhook view 200s those.
func TestHandleRCEvent_UnknownTypeAcked(t *testing.T) {
	// Nil DB is OK because the unknown branch returns before any DB call.
	c := &Controller{}
	err := c.HandleRCEvent(nil, RCEventV2{Event: RCEventV2Event{Type: "SOMETHING_NEW"}})
	if err != nil {
		t.Fatalf("expected nil error for unknown type, got %v", err)
	}
}

// TestRCEvent_PickEntitlementID prefers EntitlementID when set, falls back to first of EntitlementIDs.
func TestRCEvent_PickEntitlementID(t *testing.T) {
	if got := pickEntitlementID(RCEventV2{Event: RCEventV2Event{EntitlementID: "premium"}}); got != "premium" {
		t.Errorf("primary: %q", got)
	}
	if got := pickEntitlementID(RCEventV2{Event: RCEventV2Event{EntitlementIDs: []string{"a", "b"}}}); got != "a" {
		t.Errorf("array fallback: %q", got)
	}
	if got := pickEntitlementID(RCEventV2{Event: RCEventV2Event{}}); got != "" {
		t.Errorf("empty: %q", got)
	}
}

// TestIsNoRowsErr matches the textual "no rows" pattern Bun surfaces.
func TestIsNoRowsErr(t *testing.T) {
	if !isNoRowsErr(simpleErr("sql: no rows in result set")) {
		t.Errorf("expected true for sql: no rows ...")
	}
	if isNoRowsErr(nil) {
		t.Errorf("nil err: false expected")
	}
	if isNoRowsErr(simpleErr("connection refused")) {
		t.Errorf("non-no-rows: false expected")
	}
}

type simpleErr string

func (s simpleErr) Error() string { return string(s) }

// TestRCAnonymousIDPreserved verifies anonymous app_user_ids are not normalised
// — we keep the literal "$RCAnonymousID:..." prefix verbatim. This matters
// for the SUBSCRIBER_ALIAS join later.
func TestRCAnonymousIDPreserved(t *testing.T) {
	anon := "$RCAnonymousID:9c8d-7b6a"
	row := mobileSubscriptionFromEvent(RCEventV2{Event: RCEventV2Event{
		Type: RCEventInitialPurchase, AppUserID: anon, ProductID: "p",
	}}, uuid.New(), uuid.New(), "active")
	if row == nil || row.RCAppUserID != anon {
		t.Fatalf("anon ID: got %q, want %q", row.RCAppUserID, anon)
	}
	if !strings.HasPrefix(row.RCAppUserID, "$RCAnonymousID:") {
		t.Errorf("expected anon prefix preserved")
	}
}
