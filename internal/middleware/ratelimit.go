package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/knid/wemob/internal/utils"
	"github.com/redis/go-redis/v9"
)

// RateLimiter provides Redis-backed rate limiting.
type RateLimiter struct {
	client *redis.Client
}

// NewRateLimiter creates a rate limiter backed by the given Redis client.
func NewRateLimiter(client *redis.Client) *RateLimiter {
	return &RateLimiter{client: client}
}

// RateLimit returns middleware that enforces per-key request limits using
// a sliding window counter in Redis. keyFunc extracts the rate-limit key
// from the request (e.g. IP or user ID).
func (rl *RateLimiter) RateLimit(limit int, window time.Duration, keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := fmt.Sprintf("rl:%s", keyFunc(r))
			ctx := context.Background()

			count, err := rl.client.Incr(ctx, key).Result()
			if err != nil {
				slog.Error("rate limiter redis error", "error", err)
				next.ServeHTTP(w, r)
				return
			}

			if count == 1 {
				rl.client.Expire(ctx, key, window)
			}

			ttl, _ := rl.client.TTL(ctx, key).Result()

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(max(0, limit-int(count))))

			if int(count) > limit {
				retryAfter := int(ttl.Seconds())
				if retryAfter <= 0 {
					retryAfter = int(window.Seconds())
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				utils.JSONCodedErrorResponse(w, "RATE_LIMITED", "too many requests, please try again later", http.StatusTooManyRequests, map[string]any{
					"retry_after_seconds": retryAfter,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// IPKey returns the client IP as a rate-limit key.
func IPKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return "ip:" + xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return "ip:" + xri
	}
	return "ip:" + r.RemoteAddr
}

// UserKey returns the authenticated user's ID as a rate-limit key.
// Falls back to IP if no user is in context.
func UserKey(r *http.Request) string {
	user := UserFromContext(r.Context())
	if user != nil {
		return "user:" + user.ID.String()
	}
	return IPKey(r)
}

// AIUserKey returns a rate-limit key specific to AI endpoints for the authenticated user.
func AIUserKey(r *http.Request) string {
	user := UserFromContext(r.Context())
	if user != nil {
		return "ai:" + user.ID.String()
	}
	return "ai:" + IPKey(r)
}
