package utils

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"time"
)

// RetryableHTTPDo executes an HTTP request with exponential backoff retry.
// It retries on 5xx status codes and network errors, up to maxRetries times.
func RetryableHTTPDo(ctx context.Context, client *http.Client, req *http.Request, maxRetries int) (*http.Response, error) {
	var resp *http.Response
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < backoff {
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		resp, err = client.Do(req)
		if err != nil {
			slog.Warn("HTTP request failed, retrying", "attempt", attempt+1, "error", err)
			continue
		}
		if resp.StatusCode < 500 {
			return resp, nil
		}
		slog.Warn("HTTP request returned server error, retrying", "attempt", attempt+1, "status", resp.StatusCode)
		if attempt < maxRetries {
			resp.Body.Close()
		}
	}
	if err != nil {
		return nil, err
	}
	// Final attempt was a 5xx — return with body still open so caller can read the error
	return resp, nil
}
