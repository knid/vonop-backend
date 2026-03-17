package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/utils"
	"github.com/redis/go-redis/v9"
)

// rateLimitScript atomically increments the counter and sets the expiry
// on the first increment, preventing a TOCTOU race between INCR and EXPIRE.
var rateLimitScript = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
    redis.call("EXPIRE", KEYS[1], ARGV[1])
end
return count
`)

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
			// Never rate-limit CORS preflight requests — they are automatic
			// browser overhead and blocking them breaks the actual request.
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			key := fmt.Sprintf("rl:%s", keyFunc(r))
			ctx := context.Background()

			result, err := rateLimitScript.Run(ctx, rl.client, []string{key}, int(window.Seconds())).Result()
			if err != nil {
				slog.Error("rate limiter redis error", "error", err)
				utils.JSONErrorResponse(w, "service temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			count, _ := result.(int64)

			ttl, _ := rl.client.TTL(ctx, key).Result()

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(max(0, limit-int(count))))
			if ttl > 0 {
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(ttl).Unix()))
			}

			if int(count) > limit {
				slog.Warn("request rate limited",
					"key", key,
					"count", count,
					"limit", limit,
					"from", "ratelimit",
					"path", r.URL.Path,
					"method", r.Method,
				)
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
// Extracts the first (leftmost/client) IP from X-Forwarded-For to prevent spoofing.
func IPKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs: "client, proxy1, proxy2"
		// The leftmost IP is the original client. Only use that.
		if idx := strings.Index(xff, ","); idx != -1 {
			xff = xff[:idx]
		}
		return "ip:" + strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return "ip:" + strings.TrimSpace(xri)
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

// StorefrontIPKey returns a per-store-per-IP rate-limit key using the storeId URL param.
func StorefrontIPKey(r *http.Request) string {
	storeID := chi.URLParam(r, "storeId")
	ip := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			xff = xff[:idx]
		}
		ip = strings.TrimSpace(xff)
	} else if xri := r.Header.Get("X-Real-IP"); xri != "" {
		ip = strings.TrimSpace(xri)
	}
	return "sf:" + storeID + ":" + ip
}
