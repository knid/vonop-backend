package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

type gatewayContextKey string

const gatewayUserKey gatewayContextKey = "gateway_user"
const gatewayProjectIDKey gatewayContextKey = "gateway_project_id"

// GatewayUserFromContext retrieves the authenticated user from gateway auth.
func GatewayUserFromContext(ctx context.Context) *models.User {
	user, _ := ctx.Value(gatewayUserKey).(*models.User)
	return user
}

// GatewayProjectIDFromContext retrieves the project ID from gateway auth.
func GatewayProjectIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(gatewayProjectIDKey).(string)
	return id
}

// GatewayError writes an OpenAI-compatible error JSON response.
func GatewayError(w http.ResponseWriter, message, errType, code string, status int) {
	utils.JSONResponse(w, map[string]any{
		"error": map[string]string{
			"message": message,
			"type":    errType,
			"code":    code,
		},
	}, status)
}

// RequireGatewayKey authenticates requests using wemob_gw_ prefixed keys.
func RequireGatewayKey(controller *controllers.Controller) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer "+constants.GatewayKeyPrefix) {
				GatewayError(w, "Invalid or missing API key. Use a wemob_gw_ gateway key.", "authentication_error", "invalid_api_key", http.StatusUnauthorized)
				return
			}

			rawKey := strings.TrimPrefix(authHeader, "Bearer ")
			user, projectID, err := controller.AuthenticateGatewayKey(rawKey)
			if err != nil {
				GatewayError(w, "Invalid API key.", "authentication_error", "invalid_api_key", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), gatewayUserKey, user)
			ctx = context.WithValue(ctx, gatewayProjectIDKey, projectID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GatewayRateLimit returns middleware that enforces per-tier RPM limits.
func GatewayRateLimit(rl *RateLimiter, controller *controllers.Controller) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			user := GatewayUserFromContext(r.Context())
			projectID := GatewayProjectIDFromContext(r.Context())
			if user == nil || projectID == "" {
				next.ServeHTTP(w, r)
				return
			}

			limit := controller.GatewayRPMLimit(user)
			key := fmt.Sprintf("rl:gw:%s", projectID)
			ctx := context.Background()

			result, err := rateLimitScript.Run(ctx, rl.client, []string{key}, 60).Result()
			if err != nil {
				slog.Error("gateway rate limiter redis error", "error", err)
				next.ServeHTTP(w, r)
				return
			}
			count, _ := result.(int64)

			if int(count) > limit {
				ttl, _ := rl.client.TTL(ctx, key).Result()
				retryAfter := int(ttl.Seconds())
				if retryAfter <= 0 {
					retryAfter = 60
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				GatewayError(w, fmt.Sprintf("Rate limit exceeded. Retry after %d seconds.", retryAfter), "rate_limit_error", "rate_limit_exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
