package middleware

import (
	"context"
	"net/http"
	"strings"
)

type localeKey struct{}

// SupportedLocales lists the locales the API can serve. Default is "en".
var SupportedLocales = []string{"en", "tr"}

// Locale parses the Accept-Language header and stores the resolved locale
// (one of SupportedLocales, default "en") on the request context. Used by
// downstream code that needs the user's language for emails, notifications,
// or log enrichment.
func Locale(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loc := ResolveLocale(r.Header.Get("Accept-Language"))
		ctx := context.WithValue(r.Context(), localeKey{}, loc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ResolveLocale parses an Accept-Language header value and returns the first
// supported base locale (e.g. "tr-TR" → "tr"), defaulting to "en". Exported
// so OAuth signup flows can seed new users' preferred language without
// routing through the middleware.
func ResolveLocale(header string) string {
	if header == "" {
		return "en"
	}
	first := strings.SplitN(header, ",", 2)[0]
	first = strings.SplitN(first, ";", 2)[0]
	first = strings.SplitN(first, "-", 2)[0]
	first = strings.TrimSpace(strings.ToLower(first))
	for _, l := range SupportedLocales {
		if l == first {
			return l
		}
	}
	return "en"
}
