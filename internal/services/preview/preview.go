package preview

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
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
	FlyAppName    string // e.g. "wemob-previews" — for direct machine access
	httpClient    *http.Client
}

func NewService(flyService *fly.Service, db *database.Database, previewDomain string) *Service {
	return &Service{
		Fly:           flyService,
		DB:            db,
		PreviewDomain: previewDomain,
		FlyAppName:    flyService.AppName,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

type PreviewStatus struct {
	Status     string `json:"status"`      // "starting", "syncing", "ready", "error", "stopped"
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

// CreatePreview creates or reuses a Fly Machine for the project, pushes files, and returns status.
// Reuse logic: running → sync files, stopped → start + sync, errored → destroy + recreate, none → create new.
func (s *Service) CreatePreview(projectID string, files map[string]string, envVars map[string]string) (*PreviewStatus, error) {
	previewURL := fmt.Sprintf("https://%s.%s", projectID, s.PreviewDomain)

	// Check for an existing machine record
	existing, err := s.DB.GetPreviewMachineByProjectID(projectID)
	if err == nil && existing.MachineID != "" {
		// Verify actual state with Fly API
		flyMachine, flyErr := s.Fly.GetMachine(existing.MachineID)
		if flyErr != nil {
			// Machine doesn't exist on Fly anymore — clean up DB and create fresh
			slog.Warn("existing machine not found on Fly, will recreate", "project_id", projectID, "machine_id", existing.MachineID, "error", flyErr)
			if err := s.DB.DeletePreviewMachineByProjectID(projectID); err != nil {
				slog.Error("failed to delete stale machine record", "project_id", projectID, "error", err)
			}
		} else {
			switch flyMachine.State {
			case "started":
				// Machine is already running — sync files before reporting ready
				slog.Info("reusing running preview machine", "project_id", projectID, "machine_id", existing.MachineID)
				existing.Status = "syncing"
				existing.LastAccessAt = time.Now()
				if err := s.DB.UpdatePreviewMachine(existing); err != nil {
					slog.Error("failed to update machine status to syncing", "project_id", projectID, "error", err)
				}

				go func() {
					if err := s.waitForHealth(existing.MachineID, 30*time.Second); err != nil {
						slog.Error("running machine health check failed", "project_id", projectID, "error", err)
						existing.Status = "error"
						if err := s.DB.UpdatePreviewMachine(existing); err != nil {
							slog.Error("failed to update machine status", "project_id", projectID, "error", err)
						}
						return
					}
					if err := s.syncFiles(existing.MachineID, existing.SyncToken, files, nil); err != nil {
						slog.Error("file sync to running machine failed", "project_id", projectID, "error", err)
						existing.Status = "error"
						if err := s.DB.UpdatePreviewMachine(existing); err != nil {
							slog.Error("failed to update machine status", "project_id", projectID, "error", err)
						}
						return
					}
					existing.Status = "ready"
					existing.LastAccessAt = time.Now()
					if err := s.DB.UpdatePreviewMachine(existing); err != nil {
						slog.Error("failed to update machine status to ready", "project_id", projectID, "error", err)
					}
					slog.Info("running preview machine synced and ready", "project_id", projectID, "machine_id", existing.MachineID)
				}()

				return &PreviewStatus{
					Status:     "syncing",
					PreviewURL: previewURL,
				}, nil

			case "stopped", "suspended":
				// Machine exists but is stopped — start it and sync
				slog.Info("restarting stopped preview machine", "project_id", projectID, "machine_id", existing.MachineID)
				existing.Status = "starting"
				existing.LastAccessAt = time.Now()
				if err := s.DB.UpdatePreviewMachine(existing); err != nil {
					slog.Error("failed to update machine status to starting", "project_id", projectID, "error", err)
				}

				go func() {
					if err := s.Fly.StartMachine(existing.MachineID); err != nil {
						slog.Error("failed to start stopped machine", "machine_id", existing.MachineID, "error", err)
						existing.Status = "error"
						if err := s.DB.UpdatePreviewMachine(existing); err != nil {
							slog.Error("failed to update machine status", "project_id", projectID, "error", err)
						}
						return
					}

					if err := s.Fly.WaitForState(existing.MachineID, "started", 60*time.Second); err != nil {
						slog.Error("stopped machine did not start", "machine_id", existing.MachineID, "error", err)
						existing.Status = "error"
						if err := s.DB.UpdatePreviewMachine(existing); err != nil {
							slog.Error("failed to update machine status", "project_id", projectID, "error", err)
						}
						return
					}

					if err := s.waitForHealth(existing.MachineID, 60*time.Second); err != nil {
						slog.Error("restarted machine proxy never became healthy", "project_id", projectID, "error", err)
						existing.Status = "error"
						if err := s.DB.UpdatePreviewMachine(existing); err != nil {
							slog.Error("failed to update machine status", "project_id", projectID, "error", err)
						}
						return
					}

					if err := s.syncFiles(existing.MachineID, existing.SyncToken, files, nil); err != nil {
						slog.Error("file sync to restarted machine failed", "project_id", projectID, "error", err)
						existing.Status = "error"
						if err := s.DB.UpdatePreviewMachine(existing); err != nil {
							slog.Error("failed to update machine status", "project_id", projectID, "error", err)
						}
						return
					}

					existing.Status = "ready"
					existing.LastAccessAt = time.Now()
					if err := s.DB.UpdatePreviewMachine(existing); err != nil {
						slog.Error("failed to update machine status to ready", "project_id", projectID, "error", err)
					}
					slog.Info("restarted preview machine ready", "project_id", projectID, "machine_id", existing.MachineID)
				}()

				return &PreviewStatus{
					Status:     "starting",
					PreviewURL: previewURL,
				}, nil

			default:
				// Machine is in an error/unknown state — destroy and recreate
				slog.Info("destroying errored preview machine before recreate", "project_id", projectID, "machine_id", existing.MachineID, "state", flyMachine.State)
				if err := s.Fly.DestroyMachine(existing.MachineID, true); err != nil {
					slog.Error("failed to destroy errored machine", "machine_id", existing.MachineID, "error", err)
				}
				if err := s.DB.DeletePreviewMachineByProjectID(projectID); err != nil {
					slog.Error("failed to delete errored machine record", "project_id", projectID, "error", err)
				}
			}
		}
	}

	// No existing machine (or it was cleaned up above) — create a new one
	return s.createNewMachine(projectID, files, envVars, previewURL)
}

// createNewMachine provisions a brand new Fly Machine for the project.
func (s *Service) createNewMachine(projectID string, files map[string]string, envVars map[string]string, previewURL string) (*PreviewStatus, error) {
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
		// Handle 409 conflict: a machine with this name already exists on Fly
		// but our DB lost track of it. Destroy the orphan and retry.
		if strings.Contains(err.Error(), "already_exists") || strings.Contains(err.Error(), "status 409") {
			slog.Warn("machine name conflict, destroying orphaned machine", "name", machineName, "project_id", projectID)
			machines, listErr := s.Fly.ListMachines()
			if listErr == nil {
				for _, m := range machines {
					if m.Name == machineName {
						if err := s.Fly.DestroyMachine(m.ID, true); err != nil {
							slog.Error("failed to destroy orphaned machine", "machine_id", m.ID, "error", err)
						} else {
							slog.Info("destroyed orphaned machine", "machine_id", m.ID, "name", machineName)
						}
						break
					}
				}
			}
			// Retry creation after destroying the orphan
			machine, err = s.Fly.CreateMachine(machineName, machineEnv, metadata)
		}
		if err != nil {
			return nil, fmt.Errorf("creating fly machine: %w", err)
		}
	}

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
		if destroyErr := s.Fly.DestroyMachine(machine.ID, true); destroyErr != nil {
			slog.Error("failed to cleanup machine after DB error", "machine_id", machine.ID, "error", destroyErr)
		}
		return nil, fmt.Errorf("saving preview machine to DB: %w", err)
	}

	// Background: wait for machine to start, then push files
	go func() {
		if err := s.Fly.WaitForState(machine.ID, "started", 60*time.Second); err != nil {
			slog.Error("machine did not start", "machine_id", machine.ID, "error", err)
			record.Status = "error"
			if err := s.DB.UpdatePreviewMachine(record); err != nil {
				slog.Error("failed to update machine status", "project_id", projectID, "error", err)
			}
			// Destroy orphaned machine that never started
			if err := s.Fly.DestroyMachine(machine.ID, true); err != nil {
				slog.Error("failed to destroy machine that never started", "machine_id", machine.ID, "error", err)
			}
			return
		}

		// Wait for the proxy server inside the machine to be ready
		slog.Info("waiting for preview proxy to become healthy", "project_id", projectID)
		if err := s.waitForHealth(machine.ID, 120*time.Second); err != nil {
			slog.Error("preview proxy never became healthy", "project_id", projectID, "error", err)
			record.Status = "error"
			if err := s.DB.UpdatePreviewMachine(record); err != nil {
				slog.Error("failed to update machine status", "project_id", projectID, "error", err)
			}
			// Destroy orphaned machine that never became healthy
			if err := s.Fly.DestroyMachine(machine.ID, true); err != nil {
				slog.Error("failed to destroy unhealthy machine", "machine_id", machine.ID, "error", err)
			}
			return
		}

		// Push files via sync endpoint
		if err := s.syncFiles(machine.ID, syncToken, files, nil); err != nil {
			slog.Error("initial file sync failed", "project_id", projectID, "error", err)
			record.Status = "error"
			if err := s.DB.UpdatePreviewMachine(record); err != nil {
				slog.Error("failed to update machine status", "project_id", projectID, "error", err)
			}
			return
		}

		record.Status = "ready"
		record.LastAccessAt = time.Now()
		if err := s.DB.UpdatePreviewMachine(record); err != nil {
			slog.Error("failed to update machine status to ready", "project_id", projectID, "error", err)
		}

		slog.Info("preview machine ready", "project_id", projectID, "machine_id", machine.ID, "url", previewURL)
	}()

	return &PreviewStatus{
		Status:     "starting",
		PreviewURL: previewURL,
	}, nil
}

// UpdateFiles pushes file changes to a running preview machine's sync endpoint.
// deletePaths lists files to remove from the preview machine.
func (s *Service) UpdateFiles(projectID string, files map[string]string, deletePaths []string) error {
	record, err := s.DB.GetPreviewMachineByProjectID(projectID)
	if err != nil {
		return fmt.Errorf("preview machine not found: %w", err)
	}

	// Touch last access
	record.LastAccessAt = time.Now()
	if err := s.DB.UpdatePreviewMachine(record); err != nil {
		slog.Error("failed to update last access time", "project_id", projectID, "error", err)
	}

	return s.syncFiles(record.MachineID, record.SyncToken, files, deletePaths)
}

// UpdateEnvVars pushes a .env.local file via sync and restarts the machine if needed.
// Skips the sync+restart entirely when env vars haven't changed (hash comparison).
func (s *Service) UpdateEnvVars(projectID string, envVars map[string]string) error {
	record, err := s.DB.GetPreviewMachineByProjectID(projectID)
	if err != nil {
		return fmt.Errorf("preview machine not found: %w", err)
	}

	// Build .env.local content with sorted keys for deterministic output
	keys := make([]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, envVars[k]))
	}
	envContent := sb.String()

	// Skip if env vars haven't changed
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(envContent)))
	if hash == record.LastEnvHash {
		record.LastAccessAt = time.Now()
		if err := s.DB.UpdatePreviewMachine(record); err != nil {
			slog.Error("failed to update last access time", "project_id", projectID, "error", err)
		}
		return nil
	}

	// Push .env.local via sync
	files := map[string]string{".env.local": envContent}
	if err := s.syncFiles(record.MachineID, record.SyncToken, files, nil); err != nil {
		return fmt.Errorf("syncing env vars: %w", err)
	}

	// Save hash immediately after successful sync — even if the restart or health
	// check below fails, the env file IS on the machine. Without this, a timeout
	// leaves the hash empty and every subsequent call triggers another restart.
	record.LastEnvHash = hash
	record.LastAccessAt = time.Now()
	if err := s.DB.UpdatePreviewMachine(record); err != nil {
		slog.Error("failed to update env hash", "project_id", projectID, "error", err)
	}

	// Check if any non-NEXT_PUBLIC vars changed — these require a restart
	hasServerVars := false
	for k := range envVars {
		if !strings.HasPrefix(k, "NEXT_PUBLIC_") {
			hasServerVars = true
			break
		}
	}

	if hasServerVars {
		// Restart the machine so Next.js picks up server-side env vars.
		// Note: restart wipes the ephemeral filesystem, so .env.local must be
		// re-synced after the machine comes back up.
		if err := s.Fly.StopMachine(record.MachineID); err != nil {
			slog.Warn("failed to stop machine for env restart", "machine_id", record.MachineID, "error", err)
		}
		if err := s.Fly.WaitForState(record.MachineID, "stopped", 15*time.Second); err != nil {
			slog.Warn("machine did not stop in time", "machine_id", record.MachineID, "error", err)
		}
		if err := s.Fly.StartMachine(record.MachineID); err != nil {
			return fmt.Errorf("restarting machine: %w", err)
		}
		if err := s.Fly.WaitForState(record.MachineID, "started", 30*time.Second); err != nil {
			return fmt.Errorf("machine did not restart: %w", err)
		}
		if err := s.waitForHealth(record.MachineID, 120*time.Second); err != nil {
			return fmt.Errorf("health check failed after env restart: %w", err)
		}

		// Re-sync .env.local after restart (filesystem was wiped)
		if err := s.syncFiles(record.MachineID, record.SyncToken, files, nil); err != nil {
			return fmt.Errorf("re-syncing env file after restart: %w", err)
		}
	}

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

// GetLogs fetches Next.js logs from the preview machine.
func (s *Service) GetLogs(projectID string) (string, error) {
	record, err := s.DB.GetPreviewMachineByProjectID(projectID)
	if err != nil {
		return "", fmt.Errorf("preview machine not found: %w", err)
	}

	return s.fetchLogs(record.MachineID, record.SyncToken)
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

	// Verify actual state with Fly API
	if record.Status == "ready" || record.Status == "starting" || record.Status == "syncing" || record.Status == "error" {
		machine, err := s.Fly.GetMachine(record.MachineID)
		if err == nil {
			switch machine.State {
			case "started":
				// Fly says it's running — if our DB is stuck on "starting" or "error",
				// promote to "ready" so the user isn't stuck in a loading state forever.
				// Don't promote "syncing" — that means files are being pushed right now.
				if record.Status == "starting" || record.Status == "error" {
					slog.Info("promoting stale status to ready", "project_id", projectID, "old_status", record.Status)
					record.Status = "ready"
					if err := s.DB.UpdatePreviewMachine(record); err != nil {
						slog.Error("failed to update machine status", "project_id", projectID, "error", err)
					}
				}
			case "stopped", "suspended":
				record.Status = "stopped"
				if err := s.DB.UpdatePreviewMachine(record); err != nil {
					slog.Error("failed to update machine status", "project_id", projectID, "error", err)
				}
			case "destroyed":
				if err := s.DB.DeletePreviewMachineByProjectID(projectID); err != nil {
					slog.Error("failed to delete destroyed machine record", "project_id", projectID, "error", err)
				}
				return &PreviewStatus{Status: "not_found", Error: "machine was destroyed"}, nil
			}
		} else {
			// Machine doesn't exist on Fly anymore
			slog.Warn("machine not found on Fly during status check", "project_id", projectID, "machine_id", record.MachineID, "error", err)
			if err := s.DB.DeletePreviewMachineByProjectID(projectID); err != nil {
				slog.Error("failed to delete stale machine record", "project_id", projectID, "error", err)
			}
			return &PreviewStatus{Status: "not_found", Error: "machine no longer exists"}, nil
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
		if err := s.DB.UpdatePreviewMachine(&m); err != nil {
			slog.Error("failed to update idle machine status", "machine_id", m.MachineID, "error", err)
		}
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
		if err := s.DB.DeletePreviewMachineByProjectID(m.AppProjectID.String()); err != nil {
			slog.Error("failed to delete destroyed machine record", "project_id", m.AppProjectID, "error", err)
		}
	}

	if len(machines) > 0 {
		slog.Info("stopped machine destroy complete", "destroyed", len(machines))
	}
}

// machineBaseURL returns the direct Fly URL for a machine, bypassing the router.
// Uses the app's .fly.dev domain with Fly-Force-Instance-Id header.
func (s *Service) machineBaseURL() string {
	return fmt.Sprintf("https://%s.fly.dev", s.FlyAppName)
}

// syncFiles pushes files directly to a machine's /__wemob/sync endpoint.
// deletePaths lists files to remove from the preview machine.
func (s *Service) syncFiles(machineID, syncToken string, files map[string]string, deletePaths []string) error {
	body := map[string]interface{}{
		"files": files,
	}
	if len(deletePaths) > 0 {
		body["delete"] = deletePaths
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling sync payload: %w", err)
	}

	syncURL := s.machineBaseURL() + "/__wemob/sync"
	req, err := http.NewRequest("POST", syncURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating sync request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+syncToken)
	req.Header.Set("Fly-Force-Instance-Id", machineID)

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

// fetchLogs retrieves Next.js logs from a machine's /__wemob/logs endpoint.
func (s *Service) fetchLogs(machineID, syncToken string) (string, error) {
	logsURL := s.machineBaseURL() + "/__wemob/logs"
	req, err := http.NewRequest("GET", logsURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating logs request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+syncToken)
	req.Header.Set("Fly-Force-Instance-Id", machineID)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("logs request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("logs returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Logs string `json:"logs"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parsing logs response: %w", err)
	}

	return result.Logs, nil
}

// waitForHealth polls a machine's /__wemob/health endpoint directly until it returns 200.
func (s *Service) waitForHealth(machineID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	healthURL := s.machineBaseURL() + "/__wemob/health"
	client := &http.Client{Timeout: 5 * time.Second}

	for time.Now().Before(deadline) {
		req, err := http.NewRequest("GET", healthURL, nil)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		req.Header.Set("Fly-Force-Instance-Id", machineID)

		resp, err := client.Do(req)
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
