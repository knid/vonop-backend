package views

import (
	"net/http"
	"testing"

	"github.com/knid/wemob/internal/services/sandbox"
)

func TestParsePreviewHost(t *testing.T) {
	cases := []struct {
		host     string
		wantSlug string
		wantKind sandbox.PreviewKind
	}{
		{"todo.preview.wemob.app", "todo", sandbox.PreviewApp},
		{"todo-app.preview.wemob.app", "todo-app", sandbox.PreviewApp},
		{"todo-backend.preview.wemob.app", "todo", sandbox.PreviewBackend},
		{"todo-expo.preview.wemob.app", "todo", sandbox.PreviewExpo},
		{"todo-artifacts.preview.wemob.app", "todo", sandbox.PreviewArtifacts},
		{"Todo-Backend.Preview.Wemob.App:443", "todo", sandbox.PreviewBackend},
		{"my-app.preview.wemob.app", "my-app", sandbox.PreviewApp},
		{"id-4bba6fd1-1123-4885-9133-48efd7ce26f9-app.preview.wemob.app", "id-4bba6fd1-1123-4885-9133-48efd7ce26f9", sandbox.PreviewApp},
		{"id-4bba6fd1-1123-4885-9133-48efd7ce26f9-artifacts.preview.wemob.app", "id-4bba6fd1-1123-4885-9133-48efd7ce26f9", sandbox.PreviewArtifacts},
	}

	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			gotSlug, gotKind := parsePreviewHost(tc.host)
			if gotSlug != tc.wantSlug || gotKind != tc.wantKind {
				t.Fatalf("parsePreviewHost(%q) = (%q, %q), want (%q, %q)", tc.host, gotSlug, gotKind, tc.wantSlug, tc.wantKind)
			}
		})
	}
}

func TestParsePreviewHostRejectsInvalidHosts(t *testing.T) {
	for _, host := range []string{
		"",
		"preview.wemob.app",
		"todo.wemob.app",
		"todo.preview.wemob.app.evil.test",
		"nested.todo.preview.wemob.app",
		"3000-sandbox.provider.test",
	} {
		t.Run(host, func(t *testing.T) {
			gotSlug, gotKind := parsePreviewHost(host)
			if gotSlug != "" || gotKind != "" {
				t.Fatalf("parsePreviewHost(%q) = (%q, %q), want empty result", host, gotSlug, gotKind)
			}
		})
	}
}

func TestPreviewURLForKind(t *testing.T) {
	status := &sandbox.Status{PreviewURLs: map[sandbox.PreviewKind]string{
		sandbox.PreviewApp:     "https://app.example.test",
		sandbox.PreviewBackend: "https://backend.example.test",
	}}

	if got := previewURLForKind(status, sandbox.PreviewBackend); got != "https://backend.example.test" {
		t.Fatalf("backend preview URL = %q", got)
	}
	if got := previewURLForKind(status, sandbox.PreviewExpo); got != "" {
		t.Fatalf("expo preview URL = %q, want empty", got)
	}
}

func TestPreviewURLForKindFallsBackForApp(t *testing.T) {
	status := &sandbox.Status{PreviewURLs: map[sandbox.PreviewKind]string{
		sandbox.PreviewBackend: "https://backend.example.test",
	}}

	if got := previewURLForKind(status, sandbox.PreviewApp); got != "https://backend.example.test" {
		t.Fatalf("app fallback preview URL = %q", got)
	}
}

func TestSetPreviewProxyHeadersSkipsDaytonaWarning(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://sandbox.example.test/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	setPreviewProxyHeaders(req, "sandbox.example.test", "todo.preview.wemob.app")

	if req.Host != "sandbox.example.test" {
		t.Fatalf("request host = %q", req.Host)
	}
	if got := req.Header.Get("X-Forwarded-Host"); got != "todo.preview.wemob.app" {
		t.Fatalf("X-Forwarded-Host = %q", got)
	}
	if got := req.Header.Get(daytonaSkipPreviewWarningHeader); got != daytonaSkipPreviewWarningEnabled {
		t.Fatalf("%s = %q", daytonaSkipPreviewWarningHeader, got)
	}
}
