package fly

import (
	"errors"
	"net"
)

// IsRetryable returns true if a Fly API or network error should be retried by
// the worker. 4xx (except 408/429) is non-retryable — the caller's request is
// malformed. 5xx is retryable. Network/timeout errors are retryable.
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

	return false
}
