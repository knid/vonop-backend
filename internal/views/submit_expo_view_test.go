package views

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSubmitAppExpo_RejectsMissingToken verifies the auth gate before any
// controller call. Mirrors TestBuildAppExpo_RejectsMissingToken.
func TestSubmitAppExpo_RejectsMissingToken(t *testing.T) {
	v := &HttpViews{} // Controller intentionally nil — auth check rejects before dereferencing.

	req := httptest.NewRequest(http.MethodPost, "/v2/apps/some-app-id/submit/android",
		strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	v.SubmitAppExpo(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d (body: %s)", w.Code, w.Body.String())
	}
}
