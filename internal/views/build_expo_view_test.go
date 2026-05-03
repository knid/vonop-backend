package views

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBuildAppExpo_RejectsMissingToken verifies that the route handler
// rejects unauthenticated requests at the auth gate before reaching the
// controller. With no Authorization header, GetUserFromReq returns an
// error from utils.ExtractTokenFromHeader before ever dereferencing the
// (nil) Controller, so the handler short-circuits to 401.
//
// Wider handler-with-controller coverage (wrong project type, happy
// path, partial-failure envelope) is supplied by the controller-level
// integration tests in build_expo_mobile_integration_test.go (P1.1.C C2);
// those exercise the same business logic without the HTTP envelope
// scaffolding cost.
func TestBuildAppExpo_RejectsMissingToken(t *testing.T) {
	v := &HttpViews{} // Controller intentionally nil — auth check rejects before any controller call

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/some-app-id/build/expo",
		strings.NewReader(`{"platforms":["android"],"app_name":"X"}`))
	w := httptest.NewRecorder()

	v.BuildAppExpo(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d (body: %s)", w.Code, w.Body.String())
	}
}
