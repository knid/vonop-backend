package agent

import "testing"

func TestSlashCommandIsReadOnly(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"plan with args", "/plan build a CRM", true},
		{"explain with args", "/explain auth flow", true},
		{"review no args", "/review", true},
		{"fix mutating", "/fix broken nav", false},
		{"seo mutating", "/seo", false},
		{"unknown", "/whatever", false},
		{"plan missing args falls through", "/plan", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SlashCommandIsReadOnly(tc.text); got != tc.want {
				t.Fatalf("SlashCommandIsReadOnly(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
