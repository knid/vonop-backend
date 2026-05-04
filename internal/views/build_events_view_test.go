package views

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetBuildEvents_RejectsMissingToken — handler short-circuits to 401
// before touching the (nil) Controller when no Authorization header is
// present. Mirrors the pattern from build_expo_view_test.go: cheap,
// hermetic, catches the most common regression (auth gate accidentally
// removed).
func TestGetBuildEvents_RejectsMissingToken(t *testing.T) {
	v := &HttpViews{} // Controller intentionally nil — auth check rejects before any controller call

	req := httptest.NewRequest(http.MethodGet, "/v2/tasks/00000000-0000-0000-0000-000000000000/events", nil)
	w := httptest.NewRecorder()

	v.GetBuildEvents(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestStreamBuildEvents_RejectsMissingToken — same auth-gate guard for the
// SSE endpoint. Doubly important here because SSE's streaming nature can
// otherwise silently swallow 401 responses (we'd write headers, set up
// flush, then realize we have no user).
func TestStreamBuildEvents_RejectsMissingToken(t *testing.T) {
	v := &HttpViews{}

	req := httptest.NewRequest(http.MethodGet, "/v2/tasks/00000000-0000-0000-0000-000000000000/events/stream", nil)
	w := httptest.NewRecorder()

	v.StreamBuildEvents(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d (body: %s)", w.Code, w.Body.String())
	}
}
