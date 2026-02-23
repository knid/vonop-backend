package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ContainerInfo holds the state of a running preview container.
type ContainerInfo struct {
	ID         string
	Port       int
	Status     string // "starting", "ready", "error", "stopped"
	PreviewURL string
	LastAccess time.Time
	ProjectDir string
}

// containerManager manages the lifecycle of preview containers. It keeps an
// in-memory map of active containers, enforces a capacity limit, and provides
// methods for creation, update, removal, and idle cleanup.
type containerManager struct {
	containers    map[string]*ContainerInfo
	mu            sync.RWMutex
	maxContainers int
	baseDataDir   string
	previewDomain string
}

func newContainerManager(maxContainers int, baseDataDir, previewDomain string) *containerManager {
	return &containerManager{
		containers:    make(map[string]*ContainerInfo),
		maxContainers: maxContainers,
		baseDataDir:   baseDataDir,
		previewDomain: previewDomain,
	}
}

// createContainer provisions a new Docker container for the given project. It
// writes the provided files to disk, starts the container with a volume mount,
// waits for the Next.js dev server to become healthy, and returns the container
// info. If capacity is reached, the oldest idle container is evicted first.
func (cm *containerManager) createContainer(projectID string, files map[string]string, envVars map[string]string) (*ContainerInfo, error) {
	cm.mu.Lock()

	// If a container already exists for this project, remove it first.
	if existing, ok := cm.containers[projectID]; ok {
		cm.mu.Unlock()
		slog.Info("removing existing container before recreate", "project_id", projectID)
		_ = cm.removeContainerLocked(projectID, existing)
		cm.mu.Lock()
	}

	// Evict oldest idle container if at capacity.
	if len(cm.containers) >= cm.maxContainers {
		cm.evictOldestLocked()
	}

	// If still at capacity after eviction, reject.
	if len(cm.containers) >= cm.maxContainers {
		cm.mu.Unlock()
		return nil, fmt.Errorf("max container capacity (%d) reached", cm.maxContainers)
	}

	// Reserve the slot with a starting status.
	info := &ContainerInfo{
		Status:     "starting",
		LastAccess: time.Now(),
	}
	cm.containers[projectID] = info
	cm.mu.Unlock()

	// Create project directory and write files.
	projectDir := filepath.Join(cm.baseDataDir, projectID)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		cm.setError(projectID, err)
		return nil, fmt.Errorf("creating project directory: %w", err)
	}
	info.ProjectDir = projectDir

	if err := writeFilesToDisk(projectDir, files); err != nil {
		cm.setError(projectID, err)
		return nil, fmt.Errorf("writing files: %w", err)
	}

	// Write .env.local so env vars persist across container restarts
	if len(envVars) > 0 {
		var envContent strings.Builder
		for k, v := range envVars {
			envContent.WriteString(fmt.Sprintf("%s=%s\n", k, v))
		}
		if err := os.WriteFile(filepath.Join(projectDir, ".env.local"), []byte(envContent.String()), 0644); err != nil {
			slog.Warn("failed to write .env.local during create", "project_id", projectID, "error", err)
		}
	}

	// Force-remove any leftover container with the same name (e.g. from a previous server run).
	containerName := fmt.Sprintf("wemob-preview-%s", projectID)
	_ = execCommandNoOutput("docker", "rm", "-f", containerName)

	// Build docker run arguments.
	args := []string{
		"run", "-d",
		"--name", containerName,
		"-v", fmt.Sprintf("%s:/app", projectDir),
		"-p", "0:3000",
	}

	// Pass environment variables.
	for k, v := range envVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, "wemob-preview:latest")

	// Start the container.
	out, err := execCommand("docker", args...)
	if err != nil {
		cm.setError(projectID, err)
		return nil, fmt.Errorf("docker run: %w (output: %s)", err, out)
	}
	containerID := strings.TrimSpace(out)
	info.ID = containerID

	// Determine the host port Docker assigned.
	port, err := getAssignedPort(containerName)
	if err != nil {
		// Cleanup on failure.
		_ = execCommandNoOutput("docker", "rm", "-f", containerName)
		cm.setError(projectID, err)
		return nil, fmt.Errorf("getting assigned port: %w", err)
	}
	info.Port = port
	// For local/dev (IP or localhost): return direct port URL.
	// For production (real domain): return subdomain URL.
	if net.ParseIP(cm.previewDomain) != nil || cm.previewDomain == "localhost" {
		info.PreviewURL = fmt.Sprintf("http://%s:%d", cm.previewDomain, port)
	} else {
		info.PreviewURL = fmt.Sprintf("https://%s.%s", projectID, cm.previewDomain)
	}

	// Run health check in background so the API returns immediately with "starting".
	// The frontend polls GET /status until it becomes "ready".
	go func() {
		if err := waitForReady(port, 120*time.Second); err != nil {
			// Capture container logs for debugging.
			logs, _ := execCommand("docker", "logs", "--tail", "50", containerName)
			slog.Warn("container health check failed, marking as error",
				"project_id", projectID, "error", err, "container_logs", logs)
			cm.mu.Lock()
			info.Status = "error"
			cm.mu.Unlock()
			return
		}

		cm.mu.Lock()
		info.Status = "ready"
		info.LastAccess = time.Now()
		cm.mu.Unlock()

		slog.Info("container ready", "project_id", projectID, "port", port, "preview_url", info.PreviewURL)
	}()

	return info, nil
}

// updateFiles writes files to the project directory on disk. The Next.js dev
// server inside the container picks up changes via hot module reload.
// If package.json is among the updated files, npm install is re-run inside the
// container so newly added dependencies are available immediately.
func (cm *containerManager) updateFiles(projectID string, files map[string]string) error {
	cm.mu.RLock()
	info, ok := cm.containers[projectID]
	cm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("container not found for project %s", projectID)
	}

	if err := writeFilesToDisk(info.ProjectDir, files); err != nil {
		return fmt.Errorf("writing files: %w", err)
	}

	// If package.json changed, install new deps inside the container.
	if _, hasPackageJSON := files["package.json"]; hasPackageJSON {
		containerName := fmt.Sprintf("wemob-preview-%s", projectID)
		go func() {
			out, err := execCommand("docker", "exec", containerName, "npm", "install", "--prefer-offline")
			if err != nil {
				slog.Warn("npm install after file sync failed", "project_id", projectID, "error", err, "output", out)
			} else {
				slog.Info("npm install after file sync completed", "project_id", projectID)
			}
		}()
	}

	cm.touchContainer(projectID)
	return nil
}

// updateEnvVars writes a .env.local file to the project directory and restarts
// the container so the Next.js dev server reads the updated variables.
func (cm *containerManager) updateEnvVars(projectID string, envVars map[string]string) error {
	cm.mu.RLock()
	info, ok := cm.containers[projectID]
	cm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("container not found for project %s", projectID)
	}

	// Build .env.local content
	var envContent strings.Builder
	for k, v := range envVars {
		envContent.WriteString(fmt.Sprintf("%s=%s\n", k, v))
	}

	envPath := filepath.Join(info.ProjectDir, ".env.local")
	if err := os.WriteFile(envPath, []byte(envContent.String()), 0644); err != nil {
		return fmt.Errorf("writing .env.local: %w", err)
	}

	// Restart the container so Next.js picks up the new env vars from .env.local
	containerName := fmt.Sprintf("wemob-preview-%s", projectID)
	go func() {
		out, err := execCommand("docker", "restart", "-t", "2", containerName)
		if err != nil {
			slog.Warn("failed to restart container after env update", "project_id", projectID, "error", err, "output", out)
		} else {
			slog.Info("container restarted for env var update", "project_id", projectID)
		}
	}()

	cm.touchContainer(projectID)
	return nil
}

// removeContainer stops and removes the Docker container, deletes the project
// directory, and removes the entry from the in-memory map.
func (cm *containerManager) removeContainer(projectID string) error {
	cm.mu.Lock()
	info, ok := cm.containers[projectID]
	if !ok {
		cm.mu.Unlock()
		return fmt.Errorf("container not found for project %s", projectID)
	}
	delete(cm.containers, projectID)
	cm.mu.Unlock()

	return cm.removeContainerLocked(projectID, info)
}

// removeContainerLocked performs the actual Docker stop/rm and disk cleanup.
// The caller must have already removed the entry from the map (or will do so).
func (cm *containerManager) removeContainerLocked(projectID string, info *ContainerInfo) error {
	containerName := fmt.Sprintf("wemob-preview-%s", projectID)

	// Stop the container (5s grace period).
	if _, err := execCommand("docker", "stop", "-t", "5", containerName); err != nil {
		slog.Warn("docker stop failed", "project_id", projectID, "error", err)
	}

	// Remove the container.
	if _, err := execCommand("docker", "rm", "-f", containerName); err != nil {
		slog.Warn("docker rm failed", "project_id", projectID, "error", err)
	}

	// Remove project files from disk.
	if info.ProjectDir != "" {
		if err := os.RemoveAll(info.ProjectDir); err != nil {
			slog.Warn("failed to remove project directory", "project_id", projectID, "dir", info.ProjectDir, "error", err)
		}
	}

	slog.Info("container removed", "project_id", projectID)
	return nil
}

// getContainer returns the container info for the given project, or nil.
func (cm *containerManager) getContainer(projectID string) *ContainerInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.containers[projectID]
}

// touchContainer updates the LastAccess timestamp for the given project.
func (cm *containerManager) touchContainer(projectID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if info, ok := cm.containers[projectID]; ok {
		info.LastAccess = time.Now()
	}
}

// cleanupIdle stops and removes all containers that have been idle for longer
// than maxIdle. This is called periodically from a background goroutine.
func (cm *containerManager) cleanupIdle(maxIdle time.Duration) {
	cm.mu.RLock()
	var toRemove []string
	cutoff := time.Now().Add(-maxIdle)
	for id, info := range cm.containers {
		if info.LastAccess.Before(cutoff) {
			toRemove = append(toRemove, id)
		}
	}
	cm.mu.RUnlock()

	for _, id := range toRemove {
		slog.Info("cleaning up idle container", "project_id", id)
		if err := cm.removeContainer(id); err != nil {
			slog.Error("failed to cleanup idle container", "project_id", id, "error", err)
		}
	}

	if len(toRemove) > 0 {
		slog.Info("idle cleanup complete", "removed", len(toRemove))
	}
}

// setError marks the container status as "error" and logs the error.
func (cm *containerManager) setError(projectID string, err error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if info, ok := cm.containers[projectID]; ok {
		info.Status = "error"
	}
	slog.Error("container error", "project_id", projectID, "error", err)
}

// evictOldestLocked removes the container with the oldest LastAccess time.
// The caller must hold cm.mu in write mode.
func (cm *containerManager) evictOldestLocked() {
	var oldestID string
	var oldestTime time.Time

	for id, info := range cm.containers {
		if oldestID == "" || info.LastAccess.Before(oldestTime) {
			oldestID = id
			oldestTime = info.LastAccess
		}
	}

	if oldestID == "" {
		return
	}

	info := cm.containers[oldestID]
	delete(cm.containers, oldestID)

	// Release the lock for the actual Docker cleanup, then re-acquire.
	cm.mu.Unlock()
	slog.Info("evicting oldest idle container", "project_id", oldestID)
	_ = cm.removeContainerLocked(oldestID, info)
	cm.mu.Lock()
}

// writeFilesToDisk writes each file in the map to the given base directory,
// creating any intermediate directories as needed.
func writeFilesToDisk(baseDir string, files map[string]string) error {
	for path, content := range files {
		fullPath := filepath.Join(baseDir, filepath.Clean(path))

		// Prevent path traversal.
		if !strings.HasPrefix(fullPath, baseDir) {
			return fmt.Errorf("invalid file path: %s", path)
		}

		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}

		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing file %s: %w", path, err)
		}
	}
	return nil
}

// getAssignedPort queries Docker for the host port mapped to container port
// 3000 using `docker port`.
func getAssignedPort(containerName string) (int, error) {
	out, err := execCommand("docker", "port", containerName, "3000")
	if err != nil {
		return 0, fmt.Errorf("docker port: %w (output: %s)", err, out)
	}

	// Output format: "0.0.0.0:XXXXX\n" or ":::XXXXX\n"
	// We need the port number after the last colon.
	line := strings.TrimSpace(out)
	parts := strings.Split(line, "\n")
	// Use the first line (IPv4 binding).
	binding := strings.TrimSpace(parts[0])
	lastColon := strings.LastIndex(binding, ":")
	if lastColon == -1 {
		return 0, fmt.Errorf("unexpected docker port output: %s", line)
	}

	portStr := binding[lastColon+1:]
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return 0, fmt.Errorf("parsing port from %q: %w", portStr, err)
	}

	return port, nil
}

// waitForReady polls the container's HTTP endpoint until it returns a 200
// status code or the timeout elapses.
func waitForReady(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://localhost:%d", port)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			// Any HTTP response means the server is up (may return 404/500 while compiling).
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("container on port %d not ready after %s", port, timeout)
}

// execCommand runs a command and returns its combined stdout/stderr output.
func execCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// execCommandNoOutput runs a command and discards its output.
func execCommandNoOutput(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}
