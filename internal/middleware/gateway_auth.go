package middleware

import (
	"context"
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
