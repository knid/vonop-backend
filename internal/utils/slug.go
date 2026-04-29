package utils

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	nonAlphanumeric  = regexp.MustCompile(`[^a-z0-9-]+`)
	multipleHyphens  = regexp.MustCompile(`-{2,}`)
	slugStripRegex   = regexp.MustCompile(`[^a-z0-9]+`)
)

// Slugify converts a string into a URL-safe slug.
// "Capsule Pants & Bottoms" → "capsule-pants-bottoms"
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = nonAlphanumeric.ReplaceAllString(s, "")
	s = multipleHyphens.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// SlugifyName converts a free-form name into a URL-safe slug. Empty or
// all-non-alphanumeric input returns "workspace" as a sensible default.
//
//   "James Smith"   → "james-smith"
//   "Acme Co. ✨"    → "acme-co"
//   ""              → "workspace"
func SlugifyName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugStripRegex.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "workspace"
	}
	return s
}

// SlugWithSuffix appends a 6-char random hex suffix to a base slug to defuse
// collisions (e.g. "james-smith-9k2f1a"). Suffix length 6 → 16M values, plenty
// for human-scale ownership.
func SlugWithSuffix(base string) string {
	buf := make([]byte, 3)
	_, _ = rand.Read(buf)
	return base + "-" + hex.EncodeToString(buf)
}
