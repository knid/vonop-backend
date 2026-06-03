package utils

import "testing"

func TestSanitizeFilePathRejectsMalformedLLMPaths(t *testing.T) {
	cases := []string{
		`app/landing/page.tsx"`,
		`app/landing/"page.tsx`,
		"app/landing/page.tsx'",
		"app/landing/page.tsx`",
		`app\landing\page.tsx`,
		" app/landing/page.tsx",
		"app/landing/page.tsx ",
		"app/landing/page.tsx\n",
		".",
	}

	for _, tc := range cases {
		if cleaned, ok := SanitizeFilePath(tc); ok {
			t.Fatalf("SanitizeFilePath(%q) = %q, true; want rejected", tc, cleaned)
		}
	}
}

func TestSanitizeFilePathAllowsNextRouteSyntax(t *testing.T) {
	cases := []string{
		"app/(marketing)/page.tsx",
		"app/blog/[slug]/page.tsx",
		"app/@modal/default.tsx",
		"components/ui/button.tsx",
	}

	for _, tc := range cases {
		cleaned, ok := SanitizeFilePath(tc)
		if !ok {
			t.Fatalf("SanitizeFilePath(%q) rejected valid path", tc)
		}
		if cleaned != tc {
			t.Fatalf("SanitizeFilePath(%q) = %q, want unchanged", tc, cleaned)
		}
	}
}
