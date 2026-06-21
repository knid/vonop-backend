package main

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/views"
)

func TestMountAppSandboxRoutesRegistersLegacySandboxAPI(t *testing.T) {
	r := chi.NewRouter()
	mountAppSandboxRoutes(r, views.HttpViews{})

	got := map[string]bool{}
	if err := chi.Walk(r, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, route := range []string{
		"GET /sandbox",
		"PUT /sandbox",
		"POST /sandbox/start",
		"GET /sandbox/status",
		"POST /sandbox/sync",
		"POST /sandbox/open-route",
	} {
		if !got[route] {
			t.Fatalf("route %s was not registered; got %#v", route, got)
		}
	}
}
