package views

import "testing"

func TestPreviewSlugFromHost(t *testing.T) {
	slug, ok := previewSlugFromHost("crm-x7k2.preview.wemob.app:443", "preview.wemob.app")
	if !ok {
		t.Fatal("expected preview host")
	}
	if slug != "crm-x7k2" {
		t.Fatalf("slug = %q", slug)
	}
	if _, ok := previewSlugFromHost("api.wemob.io", "preview.wemob.app"); ok {
		t.Fatal("expected non-preview host to pass through")
	}
	if _, ok := previewSlugFromHost("nested.bad.preview.wemob.app", "preview.wemob.app"); ok {
		t.Fatal("expected nested preview host to be rejected")
	}
}

func TestSingleJoiningSlash(t *testing.T) {
	cases := []struct {
		a    string
		b    string
		want string
	}{
		{"/", "/src/main.tsx", "/src/main.tsx"},
		{"/base", "/src/main.tsx", "/base/src/main.tsx"},
		{"/base/", "src/main.tsx", "/base/src/main.tsx"},
	}
	for _, tc := range cases {
		if got := singleJoiningSlash(tc.a, tc.b); got != tc.want {
			t.Fatalf("singleJoiningSlash(%q, %q) = %q want %q", tc.a, tc.b, got, tc.want)
		}
	}
}
