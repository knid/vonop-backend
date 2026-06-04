package views

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestShouldRedirectStaticAsset(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/assets/app.js", true},
		{"/assets/style.css", true},
		{"/favicon.ico", true},
		{"/", false},
		{"/dashboard", false},
		{"/index.html", false},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := shouldRedirectStaticAsset(tc.path); got != tc.want {
				t.Fatalf("shouldRedirectStaticAsset(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestStaticCDNBaseURL(t *testing.T) {
	deployment := &models.AppDeployment{
		ProviderData: map[string]any{
			"static_cdn_base_url": "https://cdn.example.com/",
		},
	}

	if got := staticCDNBaseURL(deployment); got != "https://cdn.example.com" {
		t.Fatalf("staticCDNBaseURL() = %q", got)
	}
}
