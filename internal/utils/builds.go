package utils

import "fmt"

// CreateVersionString converts a build number to a semver-like version string.
// e.g. 1 -> 0.0.1, 14 -> 0.1.4, 105 -> 1.0.5, 999 -> 9.9.9, 1000 -> 10.0.0
func CreateVersionString(buildNumber int) string {
	if buildNumber <= 0 {
		buildNumber = 1
	}
	major := buildNumber / 100
	minor := (buildNumber % 100) / 10
	patch := buildNumber % 10

	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}
