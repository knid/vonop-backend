package controllers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

const previewCodeTTL = 5 * time.Minute

// GeneratePreviewCode creates a one-time code for previewing an app.
// It resolves the preview URL by checking for an active E2B sandbox first,
// then falling back to the latest deployed web URL.
func (c *Controller) GeneratePreviewCode(user *models.User, appProjectID string) (*models.PreviewCode, error) {
	project, err := c.DB.GetAppProjectByID(appProjectID)
	if err != nil {
		return nil, fmt.Errorf("project not found")
	}
	if err := c.AuthorizeProjectAccess(user, project); err != nil {
		return nil, fmt.Errorf("project not found")
	}

	previewURL, sandboxID, err := c.resolvePreviewURL(project)
	if err != nil {
		return nil, err
	}

	// Generate a cryptographically random code
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("failed to generate code")
	}
	hash := sha256.Sum256(randomBytes)
	code := hex.EncodeToString(hash[:])

	pc := &models.PreviewCode{
		Code:         code,
		AppProjectID: uuid.MustParse(appProjectID),
		UserID:       user.ID,
		SandboxID:    sandboxID,
		PreviewURL:   previewURL,
		ExpiresAt:    time.Now().Add(previewCodeTTL),
	}

	pc, err = c.DB.CreatePreviewCode(pc)
	if err != nil {
		return nil, fmt.Errorf("failed to create preview code")
	}

	return pc, nil
}

// RedeemPreviewCode validates and consumes a one-time preview code.
func (c *Controller) RedeemPreviewCode(user *models.User, code string) (*models.PreviewCode, *models.AppProject, error) {
	pc, err := c.DB.GetPreviewCodeByCode(code)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid preview code")
	}

	if time.Now().After(pc.ExpiresAt) {
		return nil, nil, fmt.Errorf("preview code has expired")
	}

	if !pc.RedeemedAt.IsZero() {
		return nil, nil, fmt.Errorf("preview code already used")
	}

	if err := c.DB.MarkPreviewCodeRedeemed(code, user.ID.String()); err != nil {
		return nil, nil, fmt.Errorf("failed to redeem code")
	}

	project, err := c.DB.GetAppProjectByID(pc.AppProjectID.String())
	if err != nil {
		return nil, nil, fmt.Errorf("project not found")
	}

	return pc, project, nil
}

// resolvePreviewURL determines the best preview URL for a project.
// Priority: 1) Active E2B sandbox  2) Latest deployed web URL
func (c *Controller) resolvePreviewURL(project *models.AppProject) (string, string, error) {
	// Check for active E2B sandbox
	if project.SandboxID != "" {
		slog.Info("project has active sandbox", "sandbox_id", project.SandboxID, "project_id", project.ID)
	}

	// Get latest web deployment URL (uses existing DB helper)
	deployment, err := c.DB.GetLatestAppDeployment(project.ID.String(), "web")
	if err == nil && deployment.DeployedURL != "" && deployment.Status == "deployed" {
		return deployment.DeployedURL, project.SandboxID, nil
	}

	// No deployment found — check if sandbox exists as fallback
	if project.SandboxID != "" {
		return "", project.SandboxID, nil
	}

	return "", "", fmt.Errorf("app has no deployment or active preview — deploy your app first")
}
