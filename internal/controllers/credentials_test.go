package controllers

import (
	"strings"
	"testing"
)

// TestApplyP8Prefix is a unit test for the helper that turns bare UUIDs into
// prefixed S3 paths. The helper exists to fix a T3 audit bug where
// UpdateAppleConnection wrote bare UUIDs.
func TestApplyP8Prefix(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare uuid", "abc-123", "p8_keys/abc-123.p8"},
		{"empty stays empty", "", ""},
		{"already prefixed pass-through", "p8_keys/abc-123.p8", "p8_keys/abc-123.p8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyP8Prefix(tc.in)
			if got != tc.want {
				t.Errorf("applyP8Prefix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestApplyAPNSP8Prefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc-123", "apns_keys/abc-123.p8"},
		{"", ""},
		{"apns_keys/abc-123.p8", "apns_keys/abc-123.p8"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := applyAPNSP8Prefix(tc.in)
			if got != tc.want {
				t.Errorf("applyAPNSP8Prefix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestApplyServiceAccountPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc-123", "service_accounts/abc-123.json"},
		{"", ""},
		{"service_accounts/abc-123.json", "service_accounts/abc-123.json"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := applyServiceAccountPrefix(tc.in)
			if got != tc.want {
				t.Errorf("applyServiceAccountPrefix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestUpdateAppleConnection_PrefixesNewP8(t *testing.T) {
	// Compile-time check that UpdateAppleConnection does NOT do
	// `p8KeyPath = req.P8Key` (bare assignment) anywhere — should always
	// go through applyP8Prefix.
	//
	// This test is documentary only; the real check is the unit tests on
	// applyP8Prefix above + a careful read of the controller body to make
	// sure the new helper is wired in.
	//
	// To make this test meaningful in the future: write an integration test
	// that actually invokes UpdateAppleConnection with a bare UUID and
	// asserts the resulting P8KeyPath in the DB starts with "p8_keys/".
	// Skipping for now — depends on test DB infra (TODO P1.1).
	t.Skip("integration test — requires DB; TODO(P1.1)")

	_ = strings.HasPrefix // keep import alive
}
