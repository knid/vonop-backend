package preview

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/fly"
)

type Service struct {
	Fly           *fly.Service
	DB            *database.Database
	PreviewDomain string // e.g. "preview.wemob.app"
	httpClient    *http.Client
}

func NewService(flyService *fly.Service, db *database.Database, previewDomain string) *Service {
	return &Service{
		Fly:           flyService,
		DB:            db,
		PreviewDomain: previewDomain,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type PreviewStatus struct {
	Status     string `json:"status"`      // "starting", "ready", "error", "stopped"
	PreviewURL string `json:"preview_url"`
	Error      string `json:"error,omitempty"`
}

// generateSyncToken creates a random 32-byte hex token for authenticating sync requests.
func generateSyncToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreatePreview creates a new Fly Machine for the project, pushes files, and returns status.
func (s *Service) CreatePreview(projectID string, files map[string]string, envVars map[string]string) (*PreviewStatus, error) {
	// Destroy existing machine if any
	existing, err := s.DB.GetPreviewMachineByProjectID(projectID)
	if err == nil && existing.MachineID != "" {
		slog.Info("destroying existing preview machine before recreate", "project_id", projectID, "machine_id", existing.MachineID)
		_ = s.Fly.DestroyMachine(existing.MachineID, true)
		_ = s.DB.DeletePreviewMachineByProjectID(projectID)
	}

	syncToken, err := generateSyncToken()
	if err != nil {
		return nil, fmt.Errorf("generating sync token: %w", err)
	}

	// Build env vars for the machine: user env vars + sync token
	machineEnv := make(map[string]string)
	for k, v := range envVars {
		machineEnv[k] = v
	}
	machineEnv["SYNC_TOKEN"] = syncToken

	metadata := map[string]string{
		"project_id": projectID,
		"role":       "preview",
	}

	machineName := fmt.Sprintf("preview-%s", projectID)
	machine, err := s.Fly.CreateMachine(machineName, machineEnv, metadata)
	if err != nil {
		return nil, fmt.Errorf("creating fly machine: %w", err)
	}

	previewURL := fmt.Sprintf("https://%s.%s", projectID, s.PreviewDomain)

	// Save to DB
	record := &models.AppPreviewMachine{
		AppProjectID: uuid.MustParse(projectID),
		MachineID:    machine.ID,
		SyncToken:    syncToken,
		Region:       machine.Region,
		Status:       "starting",
		PreviewURL:   previewURL,
		LastAccessAt: time.Now(),
	}
	if _, err := s.DB.CreatePreviewMachine(record); err != nil {
		// Best-effort cleanup
		_ = s.Fly.DestroyMachine(machine.ID, true)
		return nil, fmt.Errorf("saving preview machine to DB: %w", err)
	}

	// Background: wait for machine to start, then push files
	go func() {
		if err := s.Fly.WaitForState(machine.ID, "started", 60*time.Second); err != nil {
			slog.Error("machine did not start", "machine_id", machine.ID, "error", err)
			record.Status = "error"
			_ = s.DB.UpdatePreviewMachine(record)
			return
		}

		// Wait for the proxy server inside the machine to be ready (npm install runs first)
		slog.Info("waiting for preview proxy to become healthy", "project_id", projectID)
		if err := s.waitForHealth(previewURL, 120*time.Second); err != nil {
			slog.Error("preview proxy never became healthy", "project_id", projectID, "error", err)
			record.Status = "error"
			_ = s.DB.UpdatePreviewMachine(record)
			return
		}

		// Push files via sync endpoint
		if err := s.syncFiles(previewURL, syncToken, files); err != nil {
			slog.Error("initial file sync failed", "project_id", projectID, "error", err)
			record.Status = "error"
			_ = s.DB.UpdatePreviewMachine(record)
			return
		}

		record.Status = "ready"
		record.LastAccessAt = time.Now()
		_ = s.DB.UpdatePreviewMachine(record)

		slog.Info("preview machine ready", "project_id", projectID, "machine_id", machine.ID, "url", previewURL)
	}()

	return &PreviewStatus{
		Status:     "starting",
		PreviewURL: previewURL,
	}, nil
}

// UpdateFiles pushes file changes to a running preview machine's sync endpoint.
func (s *Service) UpdateFiles(projectID string, files map[string]string) error {
	record, err := s.DB.GetPreviewMachineByProjectID(projectID)
	if err != nil {
		return fmt.Errorf("preview machine not found: %w", err)
	}

	// Touch last access
	record.LastAccessAt = time.Now()
	_ = s.DB.UpdatePreviewMachine(record)

	return s.syncFiles(record.PreviewURL, record.SyncToken, files)
}

// UpdateEnvVars pushes a .env.local file via sync and restarts the machine if needed.
func (s *Service) UpdateEnvVars(projectID string, envVars map[string]string) error {
	record, err := s.DB.GetPreviewMachineByProjectID(projectID)
	if err != nil {
		return fmt.Errorf("preview machine not found: %w", err)
	}

	// Build .env.local content
	envContent := ""
	for k, v := range envVars {
		envContent += fmt.Sprintf("%s=%s\n", k, v)
	}

	// Push .env.local via sync
	files := map[string]string{".env.local": envContent}
	if err := s.syncFiles(record.PreviewURL, record.SyncToken, files); err != nil {
		return fmt.Errorf("syncing env vars: %w", err)
	}

	// Check if any non-NEXT_PUBLIC vars changed — these require a restart
	hasServerVars := false
	for k := range envVars {
		if len(k) < 12 || k[:12] != "NEXT_PUBLIC_" {
			hasServerVars = true
			break
		}
	}

	if hasServerVars {
		// Restart the machine so Next.js picks up server-side env vars
		if err := s.Fly.StopMachine(record.MachineID); err != nil {
			slog.Warn("failed to stop machine for env restart", "machine_id", record.MachineID, "error", err)
		}
		_ = s.Fly.WaitForState(record.MachineID, "stopped", 15*time.Second)
		if err := s.Fly.StartMachine(record.MachineID); err != nil {
			return fmt.Errorf("restarting machine: %w", err)
		}
	}

	record.LastAccessAt = time.Now()
	_ = s.DB.UpdatePreviewMachine(record)

	return nil
}

// DeletePreview destroys the Fly Machine and removes the DB record.
func (s *Service) DeletePreview(projectID string) error {
	record, err := s.DB.GetPreviewMachineByProjectID(projectID)
	if err != nil {
		// Already gone, nothing to do
		return nil
	}

	if err := s.Fly.DestroyMachine(record.MachineID, true); err != nil {
		slog.Warn("failed to destroy fly machine", "machine_id", record.MachineID, "error", err)
	}

	return s.DB.DeletePreviewMachineByProjectID(projectID)
}

// GetLogs fetches logs from the Fly Machine and filters to error-only output.
func (s *Service) GetLogs(projectID string) (string, error) {
	record, err := s.DB.GetPreviewMachineByProjectID(projectID)
	if err != nil {
		return "", fmt.Errorf("preview machine not found: %w", err)
	}

	// Fly Machines API doesn't have a direct logs endpoint on the machines API.
	// Use the Fly logs API (nats-based). For now, we'll return empty if unavailable.
	_ = record
	// TODO: Integrate with Fly's log endpoint when available
	// For now, return a placeholder indicating logs come from machine output.
	return "", nil
}

// GetStatus returns the current status of a preview machine.
func (s *Service) GetStatus(projectID string) (*PreviewStatus, error) {
	record, err := s.DB.GetPreviewMachineByProjectID(projectID)
	if err != nil {
		return &PreviewStatus{
			Status: "not_found",
			Error:  "preview not found",
		}, nil
	}

	// Optionally verify with Fly API if status is "ready"
	if record.Status == "ready" || record.Status == "starting" {
		machine, err := s.Fly.GetMachine(record.MachineID)
		if err == nil {
			// Map Fly machine states to our status
			switch machine.State {
			case "started":
				if record.Status == "starting" {
					// Still starting, keep checking
				}
			case "stopped":
				record.Status = "stopped"
				_ = s.DB.UpdatePreviewMachine(record)
			case "destroyed":
				_ = s.DB.DeletePreviewMachineByProjectID(projectID)
				return &PreviewStatus{Status: "not_found", Error: "machine was destroyed"}, nil
			}
		}
	}

	return &PreviewStatus{
		Status:     record.Status,
		PreviewURL: record.PreviewURL,
	}, nil
}

// StopIdleMachines stops machines that have been idle for longer than the given duration.
func (s *Service) StopIdleMachines(idleThreshold time.Duration) {
	machines, err := s.DB.ListIdlePreviewMachines(idleThreshold)
	if err != nil {
		slog.Error("failed to list idle preview machines", "error", err)
		return
	}

	for _, m := range machines {
		slog.Info("stopping idle preview machine", "project_id", m.AppProjectID, "machine_id", m.MachineID, "last_access", m.LastAccessAt)
		if err := s.Fly.StopMachine(m.MachineID); err != nil {
			slog.Warn("failed to stop idle machine", "machine_id", m.MachineID, "error", err)
			continue
		}
		m.Status = "stopped"
		_ = s.DB.UpdatePreviewMachine(&m)
	}

	if len(machines) > 0 {
		slog.Info("idle machine cleanup complete", "stopped", len(machines))
	}
}

// DestroyStoppedMachines destroys machines that have been stopped for longer than the given duration.
func (s *Service) DestroyStoppedMachines(stoppedThreshold time.Duration) {
	var machines []models.AppPreviewMachine
	cutoff := time.Now().Add(-stoppedThreshold)

	// Get stopped machines older than threshold
	allIdle, err := s.DB.ListIdlePreviewMachines(stoppedThreshold)
	if err != nil {
		slog.Error("failed to list machines for destroy", "error", err)
		return
	}

	for _, m := range allIdle {
		if m.Status == "stopped" && m.LastAccessAt.Before(cutoff) {
			machines = append(machines, m)
		}
	}

	for _, m := range machines {
		slog.Info("destroying stopped preview machine", "project_id", m.AppProjectID, "machine_id", m.MachineID)
		if err := s.Fly.DestroyMachine(m.MachineID, true); err != nil {
			slog.Warn("failed to destroy machine", "machine_id", m.MachineID, "error", err)
		}
		_ = s.DB.DeletePreviewMachineByProjectID(m.AppProjectID.String())
	}

	if len(machines) > 0 {
		slog.Info("stopped machine destroy complete", "destroyed", len(machines))
	}
}

// syncFiles pushes files to the machine's /__wemob/sync endpoint.
func (s *Service) syncFiles(previewURL, syncToken string, files map[string]string) error {
	body := map[string]interface{}{
		"files": files,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling sync payload: %w", err)
	}

	syncURL := previewURL + "/__wemob/sync"
	req, err := http.NewRequest("POST", syncURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating sync request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+syncToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sync request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("sync returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// waitForHealth polls the /__wemob/health endpoint until it returns 200.
func (s *Service) waitForHealth(previewURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	healthURL := previewURL + "/__wemob/health"
	client := &http.Client{Timeout: 5 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("health check timed out after %s", timeout)
}

