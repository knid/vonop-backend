package requests

import (
	"strings"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
)

func newValidator() *validator.Validate {
	return validator.New(validator.WithRequiredStructEnabled())
}

func validRequest() ComposePushRequest {
	return ComposePushRequest{
		AudienceType: "segments",
		Title:        map[string]string{"en": "Hello"},
		Body:         map[string]string{"en": "World"},
	}
}

func TestComposePush_Validate_Minimal(t *testing.T) {
	r := validRequest()
	if err := r.Validate(newValidator()); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

// <P2#25> Validate folds Localizations entries into Title/Body so downstream
// callers only need to read the maps.
func TestComposePush_Validate_LocalizationsFolded(t *testing.T) {
	r := ComposePushRequest{
		AudienceType: "segments",
		Title:        map[string]string{"en": "Hello"},
		Body:         map[string]string{"en": "World"},
		Localizations: map[string]LocalizedPushContent{
			"tr": {Title: "Merhaba", Body: "Dünya"},
		},
	}
	if err := r.Validate(newValidator()); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if r.Title["tr"] != "Merhaba" || r.Body["tr"] != "Dünya" {
		t.Errorf("expected tr folded into Title/Body, got %+v / %+v", r.Title, r.Body)
	}
	// Original keys preserved.
	if r.Title["en"] != "Hello" {
		t.Errorf("en clobbered: %q", r.Title["en"])
	}
}

// Existing Title/Body keys win over Localizations.
func TestComposePush_Validate_LocalizationsLoseToTitleBody(t *testing.T) {
	r := ComposePushRequest{
		AudienceType: "segments",
		Title:        map[string]string{"tr": "Önce gelen"},
		Body:         map[string]string{"tr": "Önce gelen"},
		Localizations: map[string]LocalizedPushContent{
			"tr": {Title: "Sonra gelen", Body: "Sonra gelen"},
		},
	}
	if err := r.Validate(newValidator()); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if r.Title["tr"] != "Önce gelen" {
		t.Errorf("Title was overwritten: %q", r.Title["tr"])
	}
}

// SendAfter must be RFC 3339 when set.
func TestComposePush_Validate_SendAfter(t *testing.T) {
	r := validRequest()
	r.SendAfter = "not-a-date"
	if err := r.Validate(newValidator()); err == nil || !strings.Contains(err.Error(), "RFC 3339") {
		t.Fatalf("expected RFC 3339 error, got %v", err)
	}

	r2 := validRequest()
	r2.SendAfter = time.Now().Add(time.Hour).Format(time.RFC3339)
	if err := r2.Validate(newValidator()); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

// WebURL: javascript: rejected, http(s) accepted, project URL scheme accepted.
func TestComposePush_Validate_WebURLAndAppURL(t *testing.T) {
	r := validRequest()
	r.WebURL = "javascript:alert(1)"
	if err := r.Validate(newValidator()); err == nil || !strings.Contains(err.Error(), "web_url") {
		t.Fatalf("expected web_url rejection, got %v", err)
	}

	r2 := validRequest()
	r2.WebURL = "https://example.com/path"
	if err := r2.Validate(newValidator()); err != nil {
		t.Fatalf("https: %v", err)
	}

	r3 := validRequest()
	r3.AppURL = "myapp://route/123"
	r3.URLScheme = "myapp"
	if err := r3.Validate(newValidator()); err != nil {
		t.Fatalf("project scheme accepted: %v", err)
	}

	r4 := validRequest()
	r4.AppURL = "myapp://route"
	// URLScheme intentionally empty; should reject.
	if err := r4.Validate(newValidator()); err == nil {
		t.Fatalf("expected reject when URLScheme not declared")
	}

	r5 := validRequest()
	r5.AppURL = "file:///etc/passwd"
	if err := r5.Validate(newValidator()); err == nil {
		t.Fatalf("expected file:// rejection")
	}
}

// BigPicture / IOSAttachmentURL: only http(s) — no project scheme.
func TestComposePush_Validate_MediaURLs(t *testing.T) {
	r := validRequest()
	r.BigPicture = "https://cdn/example.png"
	r.IOSAttachmentURL = "https://cdn/example.png"
	if err := r.Validate(newValidator()); err != nil {
		t.Fatalf("https media: %v", err)
	}

	r2 := validRequest()
	r2.BigPicture = "myapp://image.png"
	r2.URLScheme = "myapp"
	if err := r2.Validate(newValidator()); err == nil {
		t.Fatalf("expected reject — media must be http(s)")
	}
}

// Title length cap: 65 chars; Body cap: 240.
func TestComposePush_Validate_TitleBodyLength(t *testing.T) {
	r := validRequest()
	r.Title = map[string]string{"en": strings.Repeat("a", 66)}
	if err := r.Validate(newValidator()); err == nil {
		t.Fatalf("expected title length error")
	}
	r2 := validRequest()
	r2.Body = map[string]string{"en": strings.Repeat("b", 241)}
	if err := r2.Validate(newValidator()); err == nil {
		t.Fatalf("expected body length error")
	}
}

// IsSafeNotificationURL exhaustive coverage of allow-list logic.
func TestIsSafeNotificationURL(t *testing.T) {
	cases := []struct {
		raw       string
		scheme    string
		want      bool
		desc      string
	}{
		{"http://example.com", "", true, "http"},
		{"https://example.com", "", true, "https"},
		{"javascript:alert(1)", "", false, "js scheme"},
		{"file:///etc/passwd", "", false, "file scheme"},
		{"data:text/html,...", "", false, "data scheme"},
		{"myapp://x", "myapp", true, "project scheme"},
		{"myapp://x", "MYAPP", true, "project scheme case-insensitive"},
		{"otherapp://x", "myapp", false, "wrong scheme"},
		{"ssh://server", "", false, "ssh rejected"},
	}
	for _, tc := range cases {
		got := isSafeNotificationURL(tc.raw, tc.scheme)
		if got != tc.want {
			t.Errorf("%s: isSafeNotificationURL(%q, %q) = %v, want %v",
				tc.desc, tc.raw, tc.scheme, got, tc.want)
		}
	}
}

// IsSafeMediaURL is stricter: project scheme is rejected.
func TestIsSafeMediaURL(t *testing.T) {
	if !isSafeMediaURL("https://x.com/y.png") {
		t.Errorf("https rejected")
	}
	if isSafeMediaURL("myapp://x") {
		t.Errorf("project scheme accepted, should reject")
	}
	if isSafeMediaURL("javascript:alert(1)") {
		t.Errorf("js accepted")
	}
}
