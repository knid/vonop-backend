package views

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) PreviewWebsiteFile(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	if websiteID == "" {
		utils.JSONErrorResponse(w, "website id is required", http.StatusBadRequest)
		return
	}

	// Verify ownership
	website, err := v.Controller.VerifyWebsiteOwnershipPublic(user, websiteID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	// Get file path from URL: /v1/websites/{id}/preview/css/styles.css → /css/styles.css
	filePath := chi.URLParam(r, "*")
	if filePath == "" || filePath == "/" {
		filePath = "/index.html"
	}
	if !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}

	file, err := v.Controller.DB.GetFileByWebsiteIdAndPath(website.ID.String(), filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	content := file.Content

	// Replace integration placeholders for preview
	integrations, _ := v.Controller.DB.GetIntegrationsByWebsiteId(websiteID)
	if len(integrations) > 0 {
		integrationCreds := make(map[string]map[string]any)
		for _, integ := range integrations {
			if integ.Status == "connected" {
				integrationCreds[integ.Provider] = integ.Credentials
			}
		}
		content = utils.ReplaceIntegrationPlaceholders(content, integrationCreds)
	}

	// Set content type based on file extension
	ext := filepath.Ext(filePath)
	contentType := "text/plain"
	switch ext {
	case ".html":
		contentType = "text/html"
	case ".css":
		contentType = "text/css"
	case ".js":
		contentType = "application/javascript"
	case ".json":
		contentType = "application/json"
	}

	w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
}
