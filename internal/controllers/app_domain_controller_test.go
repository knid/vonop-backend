package controllers

import "testing"

func TestIsApexDomain(t *testing.T) {
	cases := []struct {
		domain string
		want   bool
	}{
		{"example.com", true},
		{"www.example.com", false},
		{"app.example.com", false},
		{"a.b.example.com", false},
		{"example.co.uk", true},
		{"www.example.co.uk", false},
		{"shop.example.co.uk", false},
		{"example.github.io", true}, // PSL treats github.io as an eTLD
		{"foo.example.github.io", false},
		{"EXAMPLE.COM", true},  // case-insensitive
		{"example.com.", true}, // trailing dot tolerated
	}
	for _, c := range cases {
		if got := isApexDomain(c.domain); got != c.want {
			t.Errorf("isApexDomain(%q) = %v, want %v", c.domain, got, c.want)
		}
	}
}
