package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	previewSecret := os.Getenv("PREVIEW_SECRET")
	if previewSecret == "" {
		slog.Error("PREVIEW_SECRET env var is required")
		os.Exit(1)
	}

	previewDomain := os.Getenv("PREVIEW_DOMAIN")
	if previewDomain == "" {
		previewDomain = "preview.wemob.io"
	}

	baseDataDir := os.Getenv("PREVIEW_DATA_DIR")
	if baseDataDir == "" {
		baseDataDir = "/data/previews"
	}

	maxContainers := 50

	cm := newContainerManager(maxContainers, baseDataDir, previewDomain)

	// Background cleanup goroutine: every 5 minutes, remove containers idle for >30 min.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			slog.Info("running idle container cleanup")
			cm.cleanupIdle(30 * time.Minute)
		}
	}()

	// API server on :4000
	apiRouter := chi.NewRouter()

	apiRouter.Use(authMiddleware(previewSecret))

	apiRouter.Post("/projects/{id}", handleCreateProject(cm))
	apiRouter.Put("/projects/{id}/files", handleUpdateFiles(cm))
	apiRouter.Delete("/projects/{id}", handleDeleteProject(cm))
	apiRouter.Get("/projects/{id}/status", handleGetStatus(cm))
	apiRouter.Get("/projects/{id}/logs", handleGetLogs(cm))

	// Health check does not require auth.
	apiRouter.Get("/health", handleHealth())

	// Reverse proxy server
	proxyHandler := newReverseProxy(cm)

	apiPort := os.Getenv("API_PORT")
	if apiPort == "" {
		apiPort = "4000"
	}
	proxyPort := os.Getenv("PROXY_PORT")
	if proxyPort == "" {
		proxyPort = "4080"
	}

	apiServer := &http.Server{
		Addr:         ":" + apiPort,
		Handler:      apiRouter,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	proxyServer := &http.Server{
		Addr:         ":" + proxyPort,
		Handler:      proxyHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start both servers concurrently.
	errCh := make(chan error, 2)

	go func() {
		slog.Info("starting API server", "addr", ":"+apiPort)
		errCh <- apiServer.ListenAndServe()
	}()

	go func() {
		slog.Info("starting reverse proxy server", "addr", ":"+proxyPort)
		errCh <- proxyServer.ListenAndServe()
	}()

	// Block until one of the servers returns an error.
	err := <-errCh
	slog.Error("server exited", "error", err)
	os.Exit(1)
}

// authMiddleware validates the X-Preview-Secret header. The /health endpoint
// is excluded from authentication by registering it after the middleware group,
// but since chi applies middleware per-route based on registration order, we
// skip auth for the health path explicitly here.
func authMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow health checks without auth.
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			provided := r.Header.Get("X-Preview-Secret")
			if provided == "" || provided != secret {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
