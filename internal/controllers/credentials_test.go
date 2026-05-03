package controllers

import (
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

// TestUpdateAppleConnection_PrefixesNewP8 lives in
// credentials_integration_test.go — it needs a real Postgres container via
// testcontainers-go and is tagged //go:build integration.
