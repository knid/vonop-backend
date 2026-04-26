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
		loc := resolveLocale(r.Header.Get("Accept-Language"))
		ctx := context.WithValue(r.Context(), localeKey{}, loc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LocaleFrom returns the resolved locale on the context, or "en" if none.
func LocaleFrom(ctx context.Context) string {
	if v, ok := ctx.Value(localeKey{}).(string); ok && v != "" {
		return v
	}
	return "en"
}

func resolveLocale(header string) string {
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
