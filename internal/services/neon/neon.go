package neon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"time"
)

const neonAPIBase = "https://console.neon.tech/api/v2"

var neonHTTPClient = &http.Client{Timeout: 10 * time.Second}

type Service struct {
	APIKey string
	OrgID  string // optional — required for org-scoped Neon accounts
}

func NewService(apiKey string) *Service {
	return &Service{APIKey: apiKey}
}

// NewServiceWithOrg creates a Neon service scoped to a specific organization.
// Required when using an org-scoped API key (personal keys can pass orgID="").
func NewServiceWithOrg(apiKey, orgID string) *Service {
	return &Service{APIKey: apiKey, OrgID: orgID}
}

// --- Request / Response types ---

type CreateProjectRequest struct {
	Project CreateProjectSettings `json:"project"`
}

type CreateProjectSettings struct {
	Name                  string        `json:"name"`
	RegionID              string        `json:"region_id,omitempty"`
	OrgID                 string        `json:"org_id,omitempty"`
	Settings              *ProjectQuota `json:"settings,omitempty"`
	AutoscalingLimitMinCU float64       `json:"autoscaling_limit_min_cu,omitempty"`
	AutoscalingLimitMaxCU float64       `json:"autoscaling_limit_max_cu,omitempty"`
}

type ProjectQuota struct {
	Quota *QuotaSettings `json:"quota,omitempty"`
}

type QuotaSettings struct {
	LogicalSizeBytes int64 `json:"logical_size_bytes,omitempty"`
}

type CreateProjectResponse struct {
	Project        ProjectInfo     `json:"project"`
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
	ID       string `json:"id"`
	BranchID string `json:"branch_id"`
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

// maxRetries is the maximum number of times doRequest retries on transient
// failures (5xx, 423, 429). Total attempts = maxRetries + 1.
const maxRetries = 4

func (s *Service) doRequest(method, path string, body any) ([]byte, error) {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyBytes = b
	}

	var lastStatus int
	var lastBody string
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter: 1s, 2s, 4s, 8s + up to 500ms.
			delay := time.Duration(1<<(attempt-1))*time.Second + time.Duration(rand.Intn(500))*time.Millisecond
			slog.Warn("Neon API request retrying",
				"attempt", attempt, "path", path, "method", method,
				"prev_status", lastStatus, "delay", delay)
			time.Sleep(delay)
		}

		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequest(method, neonAPIBase+path, reqBody)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+s.APIKey)
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")

		resp, err := neonHTTPClient.Do(req)
		if err != nil {
			lastStatus = 0
			lastBody = err.Error()
			if attempt < maxRetries {
				continue
			}
			return nil, fmt.Errorf("do request: %w", err)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastStatus = resp.StatusCode
			lastBody = readErr.Error()
			if attempt < maxRetries {
				continue
			}
			return nil, fmt.Errorf("read response: %w", readErr)
		}

		// Retry on transient errors:
		//   429 — rate limited
		//   5xx — Neon-side failure
		// 423 ("project has running conflicting operations") is NOT retried
		// here on purpose: every 423 we've seen so far traced back to a
		// sequential race in our own code (e.g. a follow-up PATCH firing
		// before the endpoint finished initializing). Retrying papers over
		// the bug; fix the call site to remove the race instead.
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastStatus = resp.StatusCode
			lastBody = string(respBody)
			if attempt < maxRetries {
				continue
			}
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

	if len(lastBody) > 500 {
		lastBody = lastBody[:500] + "...(truncated)"
	}
	slog.Error("Neon API error after retries",
		"status", lastStatus, "path", path, "attempts", maxRetries+1, "body", lastBody)
	return nil, fmt.Errorf("neon API error after %d attempts: status %d", maxRetries+1, lastStatus)
}

// DBLimits holds per-plan database resource constraints.
type DBLimits struct {
	MaxCU     float64 // max compute units (e.g. 0.25, 0.5, 1)
	StorageMB int     // max logical storage in MB
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
		OrgID:    s.OrgID,
	}

	if limits != nil {
		// Apply storage quota
		if limits.StorageMB > 0 {
			settings.Settings = &ProjectQuota{
				Quota: &QuotaSettings{
					LogicalSizeBytes: int64(limits.StorageMB) * 1024 * 1024,
				},
			}
		}
		// Apply autoscaling limits inline so Neon configures the default
		// endpoint atomically with project creation. A follow-up PATCH on
		// /endpoints/{id} would race the endpoint's own initialization and
		// return 423 ("project already has running conflicting operations").
		if limits.MaxCU > 0 {
			minCU := 0.25
			maxCU := limits.MaxCU
			if maxCU < minCU {
				maxCU = minCU
			}
			settings.AutoscalingLimitMinCU = minCU
			settings.AutoscalingLimitMaxCU = maxCU
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
