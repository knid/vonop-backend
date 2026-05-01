package controllers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

// GetAppEnvVars returns all custom env vars for a project
func (c *Controller) GetAppEnvVars(projectID string) ([]models.AppEnvVar, error) {
	return c.DB.GetAppEnvVarsByProjectID(projectID)
}

// SetAppEnvVar creates or updates an env var by key (upsert)
func (c *Controller) SetAppEnvVar(project *models.AppProject, req requests.SetEnvVarRequest) (*models.AppEnvVar, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	// Check if key already exists — upsert
	existing, err := c.DB.GetAppEnvVarByKey(project.ID.String(), req.Key)
	if err == nil && existing != nil {
		existing.Value = req.Value
		existing.IsPublic = req.IsPublic
		existing.UpdatedAt = time.Now()
		return c.DB.UpdateAppEnvVar(existing)
	}

	envVar := &models.AppEnvVar{
		AppProjectID: project.ID,
		Key:          req.Key,
		Value:        req.Value,
		IsPublic:     req.IsPublic,
	}
	return c.DB.CreateAppEnvVar(envVar)
}

// UpdateAppEnvVar partially updates an env var
func (c *Controller) UpdateAppEnvVar(id string, project *models.AppProject, req requests.UpdateEnvVarRequest) (*models.AppEnvVar, error) {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return nil, err
	}

	envVar, err := c.DB.GetAppEnvVarByID(id)
	if err != nil {
		return nil, fmt.Errorf("env var not found")
	}
	if envVar.AppProjectID != project.ID {
		return nil, fmt.Errorf("unauthorized")
	}

	if req.Value != nil {
		envVar.Value = *req.Value
	}
	if req.IsPublic != nil {
		envVar.IsPublic = *req.IsPublic
	}

	envVar.UpdatedAt = time.Now()
	return c.DB.UpdateAppEnvVar(envVar)
}

// DeleteAppEnvVar deletes an env var
func (c *Controller) DeleteAppEnvVar(id string, project *models.AppProject) error {
	envVar, err := c.DB.GetAppEnvVarByID(id)
	if err != nil {
		return fmt.Errorf("env var not found")
	}
	if envVar.AppProjectID != project.ID {
		return fmt.Errorf("unauthorized")
	}
	return c.DB.DeleteAppEnvVar(envVar)
}

// CollectAllEnvVars merges integration env vars, custom env vars, and auto-injected store vars
// into a single map for deployment/preview.
func (c *Controller) CollectAllEnvVars(project *models.AppProject) map[string]string {
	envVars := make(map[string]string)

	// 1. Integration env vars
	appIntegrations, err := c.DB.GetAppIntegrationsByProjectID(project.ID.String())
	if err != nil {
		slog.Warn("Failed to fetch integration env vars", "error", err, "project_id", project.ID)
	}
	for _, integ := range appIntegrations {
		for k, v := range c.DecryptedIntegrationEnvVars(&integ) {
			envVars[k] = v
		}
	}

	// 2. Custom env vars (with NEXT_PUBLIC_ prefix if IsPublic)
	customVars, err := c.DB.GetAppEnvVarsByProjectID(project.ID.String())
	if err != nil {
		slog.Warn("Failed to fetch custom env vars", "error", err, "project_id", project.ID)
	}
	for _, ev := range customVars {
		key := ev.Key
		if ev.IsPublic && !strings.HasPrefix(key, "NEXT_PUBLIC_") {
			key = "NEXT_PUBLIC_" + key
		}
		envVars[key] = ev.Value
	}

	// 3. Auto-inject AI gateway key (server-side) and gateway URL (public)
	if gwKey, err := c.GetDecryptedGatewayKey(project.ID.String()); err == nil && gwKey != "" {
		envVars["WEMOB_AI_KEY"] = gwKey
		envVars["NEXT_PUBLIC_WEMOB_GATEWAY_URL"] = constants.WEMOB_API_BASE_URL + "/v2/gateway"
	}

	return envVars
}

// BuildDotEnvLocal generates .env.local content from an env vars map.
// Values containing special characters are quoted to prevent .env parse issues.
func BuildDotEnvLocal(envVars map[string]string) string {
	if len(envVars) == 0 {
		return ""
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		v := envVars[k]
		// Quote values that contain special characters that can break .env parsers
		if strings.ContainsAny(v, " \t\n\r\"'#=\\$`") {
			// Escape existing double quotes and backslashes, then wrap in double quotes
			v = strings.ReplaceAll(v, `\`, `\\`)
			v = strings.ReplaceAll(v, `"`, `\"`)
			v = strings.ReplaceAll(v, "\n", `\n`)
			v = strings.ReplaceAll(v, "\r", `\r`)
			sb.WriteString(fmt.Sprintf("%s=\"%s\"\n", k, v))
		} else {
			sb.WriteString(fmt.Sprintf("%s=%s\n", k, v))
		}
	}
	return sb.String()
}

// GetMissingEnvVars returns required env vars that are not yet configured for a project.
func (c *Controller) GetMissingEnvVars(project *models.AppProject) ([]RequiredEnvVar, error) {
	var required []RequiredEnvVar
	if raw, ok := project.Settings["required_env_vars"]; ok {
		b, err := json.Marshal(raw)
		if err != nil {
			slog.Warn("failed to marshal required_env_vars setting", "error", err, "project_id", project.ID)
		} else if err := json.Unmarshal(b, &required); err != nil {
			slog.Warn("failed to unmarshal required_env_vars setting", "error", err, "project_id", project.ID)
		}
	}

	fileMap := make(map[string]string)
	if repo, repoErr := c.GitRepoService.Open(project.ID.String()); repoErr == nil {
		fileMap, _ = repo.ReadAllFiles()
	}
	codeKeys := detectEnvVarsFromCode(fileMap)
	return c.computeMissingEnvVars(project, required, codeKeys), nil
}
