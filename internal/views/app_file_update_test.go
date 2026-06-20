package views

import (
	"net/http/httptest"
	"testing"

	"github.com/knid/wemob/internal/requests"
)

func TestAppFileUpdatePathPrefersBodyFilePath(t *testing.T) {
	req := httptest.NewRequest("PUT", "/v2/apps/app-1/files", nil)

	got, err := appFileUpdatePath(req, requests.AppFileUpdateRequest{FilePath: "app/page.tsx"})
	if err != nil {
		t.Fatalf("appFileUpdatePath returned error: %v", err)
	}
	if got != "app/page.tsx" {
		t.Fatalf("path = %q, want app/page.tsx", got)
	}
}
