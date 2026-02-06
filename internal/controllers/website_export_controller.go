package controllers

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// ExportWebsiteAsZIP creates a ZIP archive of all website files.
func (c *Controller) ExportWebsiteAsZIP(user *models.User, websiteId string) (*bytes.Buffer, string, error) {
	website, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, "", err
	}

	files, err := c.DB.GetFilesByWebsiteId(websiteId)
	if err != nil {
		return nil, "", err
	}

	if len(files) == 0 {
		return nil, "", fmt.Errorf("website has no files to export")
	}

	// Build integration credentials map for placeholder replacement
	integrations, _ := c.DB.GetIntegrationsByWebsiteId(websiteId)
	integrationCreds := make(map[string]map[string]any)
	for _, integ := range integrations {
		if integ.Status == "connected" {
			integrationCreds[integ.Provider] = integ.Credentials
		}
	}

	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	for _, file := range files {
		content := file.Content

		// Replace integration placeholders
		if len(integrationCreds) > 0 {
			content = utils.ReplaceIntegrationPlaceholders(content, integrationCreds)
		}

		// Remove leading slash for zip path
		filePath := strings.TrimPrefix(file.FilePath, "/")

		f, err := w.Create(filePath)
		if err != nil {
			continue
		}
		f.Write([]byte(content))
	}

	w.Close()

	filename := fmt.Sprintf("%s.zip", website.Slug)
	return buf, filename, nil
}
