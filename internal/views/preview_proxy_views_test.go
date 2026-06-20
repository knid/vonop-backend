package views

import (
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
		{"todo-backend.preview.wemob.app", "todo", sandbox.PreviewBackend},
		{"todo-expo.preview.wemob.app", "todo", sandbox.PreviewExpo},
		{"todo-artifacts.preview.wemob.app", "todo", sandbox.PreviewArtifacts},
		{"Todo-Backend.Preview.Wemob.App:443", "todo", sandbox.PreviewBackend},
		{"my-app.preview.wemob.app", "my-app", sandbox.PreviewApp},
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
