package utils

import "fmt"

// Create version string like 1.0.1 of build. if each part is bigger than 9, it will be zero and left will be bigger 1 . e.g buildNumber 14 -> 0.1.4 , buildNumber 105 -> 1.0.5 , buildNumber 999 -> 9.9.9 , buildNumber 1000 -> 10.0.0
func CreateVersionString(buildNumber int) string {
	major := buildNumber / 100
	minor := (buildNumber % 100) / 10
	patch := buildNumber % 10

	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}
