package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const flyAPIBase = "https://api.machines.dev/v1"

type machineEntry struct {
	MachineID string
	State     string
	FetchedAt time.Time
}

type router struct {
	appName       string
	apiToken      string
	previewDomain string
	cache         map[string]*machineEntry
	mu            sync.RWMutex
	cacheTTL      time.Duration
	httpClient    *http.Client
}

func newRouter(appName, apiToken, previewDomain string) *router {
	return &router{
		appName:       appName,
		apiToken:      apiToken,
		previewDomain: previewDomain,
		cache:         make(map[string]*machineEntry),
		cacheTTL:      5 * time.Second,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

type flyMachine struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	State    string            `json:"state"`
	Config   flyMachineConfig  `json:"config"`
}

type flyMachineConfig struct {
	Metadata map[string]string `json:"metadata"`
}

func (rt *router) refreshCache() {
	url := fmt.Sprintf("%s/apps/%s/machines", flyAPIBase, rt.appName)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Error("failed to create list machines request", "error", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+rt.apiToken)

	resp, err := rt.httpClient.Do(req)
	if err != nil {
		slog.Error("failed to list machines", "error", err)
		return
	}
	defer resp.Body.Close()

	var machines []flyMachine
	if err := json.NewDecoder(resp.Body).Decode(&machines); err != nil {
		slog.Error("failed to decode machines", "error", err)
		return
	}

	now := time.Now()
	newCache := make(map[string]*machineEntry)

	for _, m := range machines {
		// Skip the router machine itself
		if m.Config.Metadata["role"] == "router" {
			continue
		}

		projectID := m.Config.Metadata["project_id"]
		if projectID == "" {
			continue
		}

		newCache[projectID] = &machineEntry{
			MachineID: m.ID,
			State:     m.State,
			FetchedAt: now,
		}
	}

	rt.mu.Lock()
	rt.cache = newCache
	rt.mu.Unlock()

	slog.Debug("cache refreshed", "machines", len(newCache))
}

func (rt *router) lookupMachine(projectID string) (*machineEntry, bool) {
	rt.mu.RLock()
	entry, ok := rt.cache[projectID]
	rt.mu.RUnlock()

	if ok && time.Since(entry.FetchedAt) < rt.cacheTTL {
		return entry, true
	}

	// Cache miss or stale — refresh
	rt.refreshCache()

	rt.mu.RLock()
	entry, ok = rt.cache[projectID]
	rt.mu.RUnlock()
	return entry, ok
}

func (rt *router) startMachine(machineID string) error {
	url := fmt.Sprintf("%s/apps/%s/machines/%s/start", flyAPIBase, rt.appName, machineID)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+rt.apiToken)

	resp, err := rt.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("start machine returned %d", resp.StatusCode)
	}
	return nil
}

// extractProjectID parses the project ID from a subdomain.
// "abc123.preview.wemob.app" with domain "preview.wemob.app" → "abc123"
func extractProjectID(host, domain string) string {
	suffix := "." + domain
	if !strings.HasSuffix(host, suffix) {
		return ""
	}
	projectID := strings.TrimSuffix(host, suffix)
	if projectID == "" || strings.Contains(projectID, ".") {
		return ""
	}
	return projectID
}

func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	// Strip port if present
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		potentialPort := host[idx+1:]
		isPort := true
		for _, c := range potentialPort {
			if c < '0' || c > '9' {
				isPort = false
				break
			}
		}
		if isPort {
			host = host[:idx]
		}
	}

	projectID := extractProjectID(host, rt.previewDomain)
	if projectID == "" {
		http.Error(w, "invalid host", http.StatusBadGateway)
		return
	}

	entry, ok := rt.lookupMachine(projectID)
	if !ok {
		http.Error(w, "preview not found", http.StatusNotFound)
		return
	}

	// Only replay to machines that are actually running
	if entry.State == "started" {
		w.Header().Set("fly-replay", fmt.Sprintf("instance=%s", entry.MachineID))
		// Fly's proxy intercepts fly-replay and replays the request to the
		// target instance. The response written here is discarded, so we
		// keep it minimal — no Content-Type, no body.
		return
	}

	// Machine exists but isn't running — try to start it if stopped, then show loading page
	if entry.State == "stopped" || entry.State == "suspended" {
		slog.Info("starting stopped machine", "project_id", projectID, "machine_id", entry.MachineID)
		go func() {
			if err := rt.startMachine(entry.MachineID); err != nil {
				slog.Error("failed to start machine", "machine_id", entry.MachineID, "error", err)
			}
		}()
	} else {
		slog.Info("machine not yet running", "project_id", projectID, "machine_id", entry.MachineID, "state", entry.State)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Retry-After", "5")
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprint(w, loadingHTML)
}

const loadingHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Starting preview...</title>
  <meta http-equiv="refresh" content="5">
  <style>
    body { display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; font-family: system-ui, sans-serif; background: #0a0a0a; color: #fafafa; }
    .container { text-align: center; }
    .spinner { width: 40px; height: 40px; border: 3px solid #333; border-top-color: #fff; border-radius: 50%; animation: spin 0.8s linear infinite; margin: 0 auto 16px; }
    @keyframes spin { to { transform: rotate(360deg); } }
    p { color: #888; font-size: 14px; }
  </style>
</head>
<body>
  <div class="container">
    <div class="spinner"></div>
    <h2>Starting preview environment...</h2>
    <p>This page will refresh automatically.</p>
  </div>
</body>
</html>`

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	apiToken := os.Getenv("FLY_API_TOKEN")
	if apiToken == "" {
		slog.Error("FLY_API_TOKEN env var is required")
		os.Exit(1)
	}

	appName := os.Getenv("FLY_APP_NAME")
	if appName == "" {
		appName = "wemob-previews"
	}

	previewDomain := os.Getenv("PREVIEW_DOMAIN")
	if previewDomain == "" {
		previewDomain = "preview.wemob.app"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	rt := newRouter(appName, apiToken, previewDomain)

	// Pre-warm cache
	rt.refreshCache()

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      rt,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	slog.Info("starting preview router", "addr", ":"+port, "domain", previewDomain)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}
