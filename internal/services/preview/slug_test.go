package preview

import (
	"strings"
	"testing"
)

func TestNewSlugUsesProjectAndRunHint(t *testing.T) {
	slug, err := NewSlug(SlugOptions{
		ProjectSlug: "CRM Builder!",
		JobID:       "92ksabcd-0000-0000-0000-000000000000",
	})
	if err != nil {
		t.Fatalf("NewSlug: %v", err)
	}
	if !strings.HasPrefix(slug, "crm-builder-r-92ksab-") {
		t.Fatalf("unexpected slug %q", slug)
	}
	if strings.ContainsAny(slug, "_ !") {
		t.Fatalf("slug contains invalid characters: %q", slug)
	}
}

func TestPublicURLDefaultsDomain(t *testing.T) {
	if got, want := PublicURL("crm-x7k2", ""), "https://crm-x7k2.preview.wemob.app"; got != want {
		t.Fatalf("PublicURL = %q want %q", got, want)
	}
}
