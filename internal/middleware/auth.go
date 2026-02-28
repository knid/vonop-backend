package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

type contextKey string

const userContextKey contextKey = "user"

// UserFromContext retrieves the authenticated user from the request context.
func UserFromContext(ctx context.Context) *models.User {
	user, _ := ctx.Value(userContextKey).(*models.User)
	return user
}

// RequireAuth is middleware that extracts the bearer token, validates it,
// and injects *models.User into the request context.
// Also supports API key authentication with "wemob_" prefixed keys.
func RequireAuth(controller *controllers.Controller) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")

			var user *models.User
			var err error

			// Check for API key authentication
			if strings.HasPrefix(authHeader, "Bearer wemob_") {
				rawKey := strings.TrimPrefix(authHeader, "Bearer ")
				user, _, err = controller.AuthenticateAPIKey(rawKey)
			} else {
				user, err = controller.GetUserFromReq(r)
			}

			if err != nil {
				utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireSubscription checks that the authenticated user has one of the allowed subscription statuses.
// Must be used after RequireAuth.
func RequireSubscription(statuses ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		allowed[s] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				utils.JSONErrorResponse(w, "authentication required", http.StatusUnauthorized)
				return
			}

			if !allowed[user.SubscriptionStatus] {
				utils.JSONCodedErrorResponse(w, "SUBSCRIPTION_REQUIRED", "active subscription required", http.StatusForbidden, nil)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin checks that the authenticated user is an admin.
// Must be used after RequireAuth.
func RequireAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				utils.JSONErrorResponse(w, "authentication required", http.StatusUnauthorized)
				return
			}
			if user.Type != "admin" {
				utils.JSONCodedErrorResponse(w, "ADMIN_REQUIRED", "admin access required", http.StatusForbidden, nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

