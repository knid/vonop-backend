package middleware

import "net/http"

// InternalOnly gates routes to callers presenting the X-Internal-Token header
// that matches the configured value. Used for Caddy edge → backend internal
// endpoints (cert-allowed, upstream resolution).
func InternalOnly(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" || r.Header.Get("X-Internal-Token") != token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
