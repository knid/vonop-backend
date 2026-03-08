package utils

import (
	"regexp"
	"strings"
)

var (
	nonAlphanumeric = regexp.MustCompile(`[^a-z0-9-]+`)
	multipleHyphens = regexp.MustCompile(`-{2,}`)
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
