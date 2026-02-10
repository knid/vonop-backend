package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Request and response types.

type CreateProjectRequest struct {
	Files   map[string]string `json:"files"`
	EnvVars map[string]string `json:"env_vars"`
}

type UpdateFilesRequest struct {
	Files map[string]string `json:"files"`
}

type ProjectStatusResponse struct {
	Status     string `json:"status"`
	PreviewURL string `json:"preview_url,omitempty"`
	Error      string `json:"error,omitempty"`
}

// handleCreateProject creates a new preview container for the given project.
func handleCreateProject(cm *containerManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "id")
		if projectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project id is required"})
			return
		}

		var req CreateProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		if len(req.Files) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "files are required"})
			return
		}

		slog.Info("creating preview container", "project_id", projectID, "file_count", len(req.Files))

		info, err := cm.createContainer(projectID, req.Files, req.EnvVars)
		if err != nil {
			slog.Error("failed to create container", "project_id", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, ProjectStatusResponse{
				Status: "error",
				Error:  err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusCreated, ProjectStatusResponse{
			Status:     info.Status,
			PreviewURL: info.PreviewURL,
		})
	}
}

// handleUpdateFiles writes updated files to the project directory on disk.
// Next.js dev server hot-reloads automatically when files change.
func handleUpdateFiles(cm *containerManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "id")
		if projectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project id is required"})
			return
		}

		var req UpdateFilesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}

		if len(req.Files) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "files are required"})
			return
		}

		slog.Info("updating files", "project_id", projectID, "file_count", len(req.Files))

		if err := cm.updateFiles(projectID, req.Files); err != nil {
			slog.Error("failed to update files", "project_id", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleDeleteProject stops and removes the container and cleans up disk.
func handleDeleteProject(cm *containerManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "id")
		if projectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project id is required"})
			return
		}

		slog.Info("deleting preview container", "project_id", projectID)

		if err := cm.removeContainer(projectID); err != nil {
			slog.Error("failed to remove container", "project_id", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleGetStatus returns the current status and preview URL for a project container.
func handleGetStatus(cm *containerManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "id")
		if projectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project id is required"})
			return
		}

		info := cm.getContainer(projectID)
		if info == nil {
			writeJSON(w, http.StatusNotFound, ProjectStatusResponse{
				Status: "not_found",
				Error:  "container not found",
			})
			return
		}

		writeJSON(w, http.StatusOK, ProjectStatusResponse{
			Status:     info.Status,
			PreviewURL: info.PreviewURL,
		})
	}
}

// handleHealth returns a simple health check response. No auth required.
func handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleGetLogs returns error-only logs from a project's container.
// It fetches the full tail and filters to only lines containing error indicators,
// so startup noise is excluded regardless of when the user clicks "Fix with AI".
func handleGetLogs(cm *containerManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "id")
		if projectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project id is required"})
			return
		}

		info := cm.getContainer(projectID)
		if info == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "container not found"})
			return
		}

		containerName := fmt.Sprintf("wemob-preview-%s", projectID)
		raw, err := execCommand("docker", "logs", "--tail", "200", containerName)
		if err != nil {
			slog.Error("failed to get container logs", "project_id", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get logs"})
			return
		}

		logs := filterErrorLogs(raw)

		writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
	}
}

// filterErrorLogs extracts only user-code error blocks from Next.js container output.
// It skips internal Next.js / node_modules noise and startup messages.
func filterErrorLogs(raw string) string {
	lines := strings.Split(raw, "\n")

	// Lines containing any of these are always ignored (internal noise, not user errors).
	ignorePatterns := []string{
		"node_modules/",
		"patch-incorrect-lockfile",
		"at async Promise.all",
		"at processTicksAndRejections",
		"at async patchIncorrectLockfile",
		"at fetchPkgInfo",
		"Ready in",
		"ready started server",
		"Compiling /",
		"Compiled successfully",
		"Compiled /",
		"▲ Next.js",
		"- Local:",
		"- Network:",
		"GET /",
		"POST /",
		"200 in",
		"304 in",
	}

	// Error markers that indicate the start of a real user-code error block.
	errorMarkers := []string{
		"Failed to compile",
		"Module not found",
		"Module build failed",
		"Unhandled Runtime Error",
		"is not defined",
		"Cannot find module",
		"./app/",
		"./components/",
		"./lib/",
		"./src/",
		"Import trace",
	}

	var result []string
	inBlock := false
	contextRemaining := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if inBlock {
				result = append(result, line)
			}
			continue
		}

		// Skip ignored lines entirely — they break any active error block too.
		ignored := false
		for _, pat := range ignorePatterns {
			if strings.Contains(line, pat) {
				ignored = true
				break
			}
		}
		if ignored {
			continue
		}

		isError := false
		for _, marker := range errorMarkers {
			if strings.Contains(line, marker) {
				isError = true
				break
			}
		}

		if isError {
			inBlock = true
			contextRemaining = 5
			result = append(result, line)
		} else if inBlock && contextRemaining > 0 {
			result = append(result, line)
			contextRemaining--
			if contextRemaining == 0 {
				inBlock = false
			}
		}
	}

	return strings.Join(result, "\n")
}

// writeJSON marshals v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}
