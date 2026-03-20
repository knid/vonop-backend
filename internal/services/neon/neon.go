package neon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/knid/wemob/internal/utils"
)

const neonAPIBase = "https://console.neon.tech/api/v2"

var neonHTTPClient = &http.Client{Timeout: 10 * time.Second}

type Service struct {
	APIKey string
}

func NewService(apiKey string) *Service {
	return &Service{APIKey: apiKey}
}

// --- Request / Response types ---

type CreateProjectRequest struct {
	Project CreateProjectSettings `json:"project"`
}

type CreateProjectSettings struct {
	Name     string          `json:"name"`
	RegionID string          `json:"region_id,omitempty"`
	Settings *ProjectQuota   `json:"settings,omitempty"`
}

type ProjectQuota struct {
	Quota *QuotaSettings `json:"quota,omitempty"`
}

type QuotaSettings struct {
	LogicalSizeBytes int64 `json:"logical_size_bytes,omitempty"`
}

type CreateProjectResponse struct {
	Project        ProjectInfo    `json:"project"`
	ConnectionURIs []ConnectionURI `json:"connection_uris"`
	Endpoints      []EndpointInfo  `json:"endpoints"`
}

type ProjectInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ConnectionURI struct {
	ConnectionURI string `json:"connection_uri"`
}

type EndpointInfo struct {
	ID        string `json:"id"`
	BranchID  string `json:"branch_id"`
}

type UpdateEndpointRequest struct {
	Endpoint EndpointSettings `json:"endpoint"`
}

type EndpointSettings struct {
	Settings *EndpointComputeSettings `json:"settings,omitempty"`
}

type EndpointComputeSettings struct {
	AutoscalingLimitMinCU float64 `json:"autoscaling_limit_min_cu"`
	AutoscalingLimitMaxCU float64 `json:"autoscaling_limit_max_cu"`
	SuspendTimeoutSeconds int     `json:"suspend_timeout_seconds,omitempty"`
}

type ExecuteSQLRequest struct {
	Query    string `json:"query"`
	Database string `json:"database,omitempty"`
}

type ExecuteSQLResponse struct {
	Columns []SQLColumn     `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
}

type SQLColumn struct {
	Name string `json:"name"`
	Type int    `json:"type"`
}

type BranchResponse struct {
	Branches []BranchInfo `json:"branches"`
}

type BranchInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RoleResponse struct {
	Roles []RoleInfo `json:"roles"`
}

type RoleInfo struct {
	Name string `json:"name"`
}

// --- HTTP helper ---

func (s *Service) doRequest(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, neonAPIBase+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	ctx := context.Background()
	resp, err := utils.RetryableHTTPDo(ctx, neonHTTPClient, req, 2)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		bodyStr := string(respBody)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500] + "...(truncated)"
		}
		slog.Error("Neon API error", "status", resp.StatusCode, "path", path, "body", bodyStr)
		return nil, fmt.Errorf("neon API error: status %d", resp.StatusCode)
	}

	return respBody, nil
}

// DBLimits holds per-plan database resource constraints.
type DBLimits struct {
	MaxCU       float64 // max compute units (e.g. 0.25, 0.5, 1)
	StorageMB   int     // max logical storage in MB
}

// CreateProject creates a new Neon project with optional resource limits.
// It returns the project ID and connection string.
func (s *Service) CreateProject(name, region string, limits *DBLimits) (projectID, connectionString string, err error) {
	if region == "" {
		region = "aws-us-east-2"
	}

	settings := CreateProjectSettings{
		Name:     name,
		RegionID: region,
	}

	// Apply storage quota if limits are provided
	if limits != nil && limits.StorageMB > 0 {
		settings.Settings = &ProjectQuota{
			Quota: &QuotaSettings{
				LogicalSizeBytes: int64(limits.StorageMB) * 1024 * 1024,
			},
		}
	}

	body, err := s.doRequest("POST", "/projects", CreateProjectRequest{
		Project: settings,
	})
	if err != nil {
		return "", "", err
	}

	var resp CreateProjectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", fmt.Errorf("unmarshal create project response: %w", err)
	}

	connStr := ""
	if len(resp.ConnectionURIs) > 0 {
		connStr = resp.ConnectionURIs[0].ConnectionURI
	}

	// Apply compute limits on the default endpoint
	if limits != nil && limits.MaxCU > 0 && len(resp.Endpoints) > 0 {
		endpointID := resp.Endpoints[0].ID
		if err := s.UpdateEndpointLimits(resp.Project.ID, endpointID, limits.MaxCU); err != nil {
			slog.Warn("Failed to set compute limits on Neon endpoint",
				"error", err, "project_id", resp.Project.ID, "endpoint_id", endpointID, "max_cu", limits.MaxCU)
		}
	}

	return resp.Project.ID, connStr, nil
}

// UpdateEndpointLimits sets autoscaling compute limits on a Neon endpoint.
func (s *Service) UpdateEndpointLimits(projectID, endpointID string, maxCU float64) error {
	minCU := 0.25
	if maxCU < minCU {
		maxCU = minCU
	}

	path := fmt.Sprintf("/projects/%s/endpoints/%s", projectID, endpointID)
	_, err := s.doRequest("PATCH", path, UpdateEndpointRequest{
		Endpoint: EndpointSettings{
			Settings: &EndpointComputeSettings{
				AutoscalingLimitMinCU: minCU,
				AutoscalingLimitMaxCU: maxCU,
				SuspendTimeoutSeconds: 300, // 5 min idle → suspend
			},
		},
	})
	return err
}

// GetBranchID returns the default branch ID for a project.
func (s *Service) GetBranchID(projectID string) (string, error) {
	body, err := s.doRequest("GET", fmt.Sprintf("/projects/%s/branches", projectID), nil)
	if err != nil {
		return "", err
	}

	var resp BranchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("unmarshal branches response: %w", err)
	}

	if len(resp.Branches) == 0 {
		return "", fmt.Errorf("no branches found for project %s", projectID)
	}

	return resp.Branches[0].ID, nil
}

// GetRoleName returns the default role name for a project branch.
func (s *Service) GetRoleName(projectID, branchID string) (string, error) {
	body, err := s.doRequest("GET", fmt.Sprintf("/projects/%s/branches/%s/roles", projectID, branchID), nil)
	if err != nil {
		return "", err
	}

	var resp RoleResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("unmarshal roles response: %w", err)
	}

	if len(resp.Roles) == 0 {
		return "", fmt.Errorf("no roles found for branch %s", branchID)
	}

	return resp.Roles[0].Name, nil
}

// ExecuteSQL runs a SQL query against a Neon project's database.
func (s *Service) ExecuteSQL(projectID, branchID, role, dbName, sql string) error {
	path := fmt.Sprintf("/projects/%s/branches/%s/sql", projectID, branchID)

	reqBody := map[string]string{
		"query":    sql,
		"role":     role,
		"database": dbName,
	}

	_, err := s.doRequest("POST", path, reqBody)
	return err
}

// ExecuteSQLWithResult runs a SQL query and returns the result rows.
func (s *Service) ExecuteSQLWithResult(projectID, branchID, role, dbName, sql string) (*ExecuteSQLResponse, error) {
	path := fmt.Sprintf("/projects/%s/branches/%s/sql", projectID, branchID)

	reqBody := map[string]string{
		"query":    sql,
		"role":     role,
		"database": dbName,
	}

	body, err := s.doRequest("POST", path, reqBody)
	if err != nil {
		return nil, err
	}

	var resp ExecuteSQLResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal sql response: %w", err)
	}

	return &resp, nil
}

// DeleteProject deletes a Neon project.
func (s *Service) DeleteProject(projectID string) error {
	_, err := s.doRequest("DELETE", fmt.Sprintf("/projects/%s", projectID), nil)
	return err
}
