package utils

import (
	"context"
	"time"
)

// NewStreamingContext creates a context detached from the request's middleware timeout.
// This prevents the global 120s middleware.Timeout from cancelling long-running LLM
// streaming operations. Client disconnect is detected by WriteSSEEvent returning
// a write error, which triggers defer cancel().
func NewStreamingContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Minute)
}
