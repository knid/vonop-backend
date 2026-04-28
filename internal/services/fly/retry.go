package fly

import (
	"errors"
	"net"
	"os/exec"
	"strings"
)

// IsManifestUnknown returns true when the Fly Machines API rejects a deploy
// because it can't yet see the image manifest in registry.fly.io. This is a
// propagation lag: Depot pushes the manifest and flyctl exits, but Machines
// API has a separate registry index that lags 5–10s behind. The fix is to
// retry the same request — the manifest will appear shortly.
func IsManifestUnknown(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Status != 400 {
		return false
	}
	return strings.Contains(apiErr.Body, "MANIFEST_UNKNOWN")
}

// IsRetryable returns true if a Fly API or network error should be retried by
// the worker. 4xx (except 408/429) is non-retryable — the caller's request is
// malformed. 5xx is retryable. Network/timeout errors are retryable. flyctl
// exec failures are retryable (transient builder/registry issues are common).
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status >= 500:
			return true
		case apiErr.Status == 408, apiErr.Status == 429:
			return true
		default:
			return false
		}
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return true
	}

	return false
}
