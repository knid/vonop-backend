package fly

import (
	"fmt"
	"regexp"
)

// imageRefRegex matches lines like:
//
//	"image: registry.fly.io/wemob-acme:deployment-01HXYZ"
//	"==> image registry.fly.io/wemob-acme:deployment-01HXYZ pushed"
var imageRefRegex = regexp.MustCompile(`registry\.fly\.io/[a-z0-9-]+:[a-zA-Z0-9._-]+`)

// parseImageRef extracts the registry image reference from flyctl deploy output.
// Returns the last match in the output, since flyctl prints the same ref multiple times.
func parseImageRef(flyctlOutput string) (string, error) {
	matches := imageRefRegex.FindAllString(flyctlOutput, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("fly: no image ref found in flyctl output")
	}
	return matches[len(matches)-1], nil
}
