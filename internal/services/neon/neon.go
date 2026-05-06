package neon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
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
	Name                    string                   `json:"name"`
	RegionID                string                   `json:"region_id,omitempty"`
	OrgID                   string                   `json:"org_id,omitempty"`
	PgVersion               int                      `json:"pg_version,omitempty"`
	Settings                *ProjectQuota            `json:"settings,omitempty"`
	DefaultEndpointSettings *DefaultEndpointSettings `json:"default_endpoint_settings,omitempty"`
}

type ProjectQuota struct {
	Quota *QuotaSettings `json:"quota,omitempty"`
}

// QuotaSettings carries Neon's per-project consumption limits. ActiveTime and
// DataTransfer are the levers metered on the Free org; storage alone won't
// keep a sponsored project from racking up compute time. ComputeTime is an
// optional secondary cap.
type QuotaSettings struct {
	ActiveTimeSeconds  int   `json:"active_time_seconds,omitempty"`
	ComputeTimeSeconds int   `json:"compute_time_seconds,omitempty"`
	WrittenDataBytes   int64 `json:"written_data_bytes,omitempty"`
	DataTransferBytes  int64 `json:"data_transfer_bytes,omitempty"`
	LogicalSizeBytes   int64 `json:"logical_size_bytes,omitempty"`
}

// DefaultEndpointSettings is applied to the project's default compute endpoint
// at creation time. We set autoscaling and suspend timeout here (instead of a
// follow-up PATCH) to avoid the 423 race the old code hit when patching an
// endpoint mid-init.
type DefaultEndpointSettings struct {
	AutoscalingLimitMinCU float64 `json:"autoscaling_limit_min_cu,omitempty"`
	AutoscalingLimitMaxCU float64 `json:"autoscaling_limit_max_cu,omitempty"`
	SuspendTimeoutSeconds int     `json:"suspend_timeout_seconds,omitempty"`
}

type CreateProjectResponse struct {
	Project        ProjectInfo     `json:"project"`
	ConnectionURIs []ConnectionURI `json:"connection_uris"`
	Endpoints      []EndpointInfo  `json:"endpoints"`
	Branch         BranchInfo      `json:"branch"`
}

type ProjectInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	OrgID string `json:"org_id"`
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

// UpdateProjectRequest patches a project's quota and default-endpoint
// autoscaling. Used after a tier transfer to apply the new plan's limits.
type UpdateProjectRequest struct {
	Project UpdateProjectSettings `json:"project"`
}

type UpdateProjectSettings struct {
	Settings                *ProjectQuota            `json:"settings,omitempty"`
	DefaultEndpointSettings *DefaultEndpointSettings `json:"default_endpoint_settings,omitempty"`
}

type TransferProjectRequest struct {
	TargetOrgID string `json:"target_org_id"`
}

type CreateBranchRequest struct {
	Branch    CreateBranchSettings   `json:"branch"`
	Endpoints []CreateBranchEndpoint `json:"endpoints,omitempty"`
}

type CreateBranchSettings struct {
	Name            string `json:"name,omitempty"`
	ParentID        string `json:"parent_id,omitempty"`
	ParentTimestamp string `json:"parent_timestamp,omitempty"`
}

// CreateBranchEndpoint mirrors the Neon API's compute-endpoint sub-object on
// branch creation. ReadWrite is the typical type for development branches.
type CreateBranchEndpoint struct {
	Type                  string  `json:"type"`
	AutoscalingLimitMinCU float64 `json:"autoscaling_limit_min_cu,omitempty"`
	AutoscalingLimitMaxCU float64 `json:"autoscaling_limit_max_cu,omitempty"`
	SuspendTimeoutSeconds int     `json:"suspend_timeout_seconds,omitempty"`
}

type CreateBranchResponse struct {
	Branch         BranchInfo      `json:"branch"`
	Endpoints      []EndpointInfo  `json:"endpoints"`
	ConnectionURIs []ConnectionURI `json:"connection_uris"`
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
	ID              string `json:"id"`
	Name            string `json:"name"`
	ParentID        string `json:"parent_id,omitempty"`
	ParentTimestamp string `json:"parent_timestamp,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
}

type RoleResponse struct {
	Roles []RoleInfo `json:"roles"`
}

type RoleInfo struct {
	Name string `json:"name"`
}

// ConsumptionResponse is a thin shape over Neon's per-project consumption
// history endpoint. We surface the most recent rollup; callers compare
// against quotas to alert at 80%/95%.
type ConsumptionResponse struct {
	Projects []ProjectConsumption `json:"projects"`
}

type ProjectConsumption struct {
	ProjectID string                  `json:"project_id"`
	Periods   []ConsumptionPeriodRoll `json:"periods"`
}

type ConsumptionPeriodRoll struct {
	PeriodID    string                `json:"period_id,omitempty"`
	PeriodPlan  string                `json:"period_plan,omitempty"`
	PeriodStart string                `json:"period_start,omitempty"`
	PeriodEnd   string                `json:"period_end,omitempty"`
	Consumption ConsumptionMetricsAgg `json:"consumption,omitempty"`
}

type ConsumptionMetricsAgg struct {
	ActiveTimeSeconds         int   `json:"active_time_seconds,omitempty"`
	ComputeTimeSeconds        int   `json:"compute_time_seconds,omitempty"`
	WrittenDataBytes          int64 `json:"written_data_bytes,omitempty"`
	DataTransferBytes         int64 `json:"data_transfer_bytes,omitempty"`
	SyntheticStorageSizeBytes int64 `json:"synthetic_storage_size_bytes,omitempty"`
}

// --- HTTP helper ---

// maxRetries is the maximum number of times doRequest retries on transient
// failures (5xx, 423, 429). Total attempts = maxRetries + 1.
const maxRetries = 4

// doRequest issues an HTTP request against the Neon API using the service's
// own API key. Most call sites should use this; transfers must use
// doRequestWithKey because cross-org PATCHes need a personal key, not the
// service's org-scoped key.
func (s *Service) doRequest(method, path string, body any) ([]byte, error) {
	return doRequestWithKey(s.APIKey, method, path, body)
}

// doRequestWithKey is the underlying transport. Exposed at package level so
// callers (e.g. transfer flows) can swap in a personal API key without
// constructing a transient *Service.
func doRequestWithKey(apiKey, method, path string, body any) ([]byte, error) {
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
		req.Header.Set("Authorization", "Bearer "+apiKey)
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

// DBLimits holds the full per-plan database resource constraints applied at
// project creation and on every plan-change re-quota. The agent-plan API
// expects active-time + data-transfer + storage caps, plus an
// autoscaling/suspend pair on the default endpoint.
type DBLimits struct {
	MaxCU                 float64 // max compute units (e.g. 0.25, 1, 4)
	MinCU                 float64 // min compute units; defaults to 0.25 when zero
	StorageMB             int     // logical_size_bytes cap, MB
	ActiveTimeSec         int     // active_time_seconds cap (Neon-metered on Free)
	DataTransferBytes     int64   // data_transfer_bytes cap
	ComputeTimeSec        int     // optional compute_time_seconds cap
	SuspendTimeoutSeconds int     // 0 → Neon default (300); 60 minimum on paid org
}

// quotaSettings builds the wire-format quota payload from DBLimits. Returns
// nil when no fields are set, so the Neon API doesn't see an empty object.
func (l *DBLimits) quotaSettings() *QuotaSettings {
	if l == nil {
		return nil
	}
	q := &QuotaSettings{}
	any := false
	if l.StorageMB > 0 {
		q.LogicalSizeBytes = int64(l.StorageMB) * 1024 * 1024
		any = true
	}
	if l.ActiveTimeSec > 0 {
		q.ActiveTimeSeconds = l.ActiveTimeSec
		any = true
	}
	if l.ComputeTimeSec > 0 {
		q.ComputeTimeSeconds = l.ComputeTimeSec
		any = true
	}
	if l.DataTransferBytes > 0 {
		q.DataTransferBytes = l.DataTransferBytes
		any = true
	}
	if !any {
		return nil
	}
	return q
}

// endpointSettings builds the wire-format default-endpoint payload. Returns
// nil when no fields are set.
func (l *DBLimits) endpointSettings() *DefaultEndpointSettings {
	if l == nil {
		return nil
	}
	e := &DefaultEndpointSettings{}
	any := false
	if l.MaxCU > 0 {
		minCU := l.MinCU
		if minCU <= 0 {
			minCU = 0.25
		}
		maxCU := l.MaxCU
		if maxCU < minCU {
			maxCU = minCU
		}
		e.AutoscalingLimitMinCU = minCU
		e.AutoscalingLimitMaxCU = maxCU
		any = true
	}
	if l.SuspendTimeoutSeconds > 0 {
		e.SuspendTimeoutSeconds = l.SuspendTimeoutSeconds
		any = true
	}
	if !any {
		return nil
	}
	return e
}

// CreateProject creates a new Neon project with optional resource limits.
// It returns the project ID and connection string.
func (s *Service) CreateProject(name, region string, limits *DBLimits) (projectID, connectionString string, err error) {
	if region == "" {
		region = "aws-us-east-2"
	}

	settings := CreateProjectSettings{
		Name:                    name,
		RegionID:                region,
		OrgID:                   s.OrgID,
		PgVersion:               16,
		DefaultEndpointSettings: limits.endpointSettings(),
	}
	if q := limits.quotaSettings(); q != nil {
		settings.Settings = &ProjectQuota{Quota: q}
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

// UpdateProjectLimits PATCHes a project's quota and default-endpoint
// autoscaling/suspend. Called after a Free↔Paid transfer to apply the new
// plan's limits in the destination org.
func (s *Service) UpdateProjectLimits(projectID string, limits *DBLimits) error {
	settings := UpdateProjectSettings{
		DefaultEndpointSettings: limits.endpointSettings(),
	}
	if q := limits.quotaSettings(); q != nil {
		settings.Settings = &ProjectQuota{Quota: q}
	}
	if settings.Settings == nil && settings.DefaultEndpointSettings == nil {
		return nil
	}
	_, err := s.doRequest("PATCH", fmt.Sprintf("/projects/%s", projectID), UpdateProjectRequest{
		Project: settings,
	})
	return err
}

// UpdateEndpointLimits sets autoscaling compute limits on a Neon endpoint.
// Kept for callers that target a specific (non-default) endpoint; the project
// PATCH above is preferred for the default endpoint.
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

// TransferProject moves a project to targetOrgID. Cross-org transfers require
// a personal API key — org-scoped keys 403 across orgs.
func TransferProject(personalAPIKey, projectID, targetOrgID string) error {
	if personalAPIKey == "" {
		return fmt.Errorf("transfer requires personal API key")
	}
	if targetOrgID == "" {
		return fmt.Errorf("transfer requires target_org_id")
	}
	_, err := doRequestWithKey(personalAPIKey, "PATCH",
		fmt.Sprintf("/projects/%s/transfer", projectID),
		TransferProjectRequest{TargetOrgID: targetOrgID})
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

// ListBranches returns every branch on a project. Callers use this to
// enforce per-tier branch caps (Free=10, Paid=1000) and to find snapshots.
func (s *Service) ListBranches(projectID string) ([]BranchInfo, error) {
	body, err := s.doRequest("GET", fmt.Sprintf("/projects/%s/branches", projectID), nil)
	if err != nil {
		return nil, err
	}
	var resp BranchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal branches response: %w", err)
	}
	return resp.Branches, nil
}

// CreateBranch creates a new branch off parentBranchID. When parentTimestamp
// is non-nil, Neon performs a point-in-time recovery branch from that
// instant — the agent-plan undo/restore primitive. When endpoint is non-nil,
// the branch is created with its own compute endpoint (development workflow).
func (s *Service) CreateBranch(projectID, name, parentBranchID string, parentTimestamp *time.Time, endpoint *CreateBranchEndpoint) (*CreateBranchResponse, error) {
	branch := CreateBranchSettings{
		Name:     name,
		ParentID: parentBranchID,
	}
	if parentTimestamp != nil {
		branch.ParentTimestamp = parentTimestamp.UTC().Format(time.RFC3339)
	}
	req := CreateBranchRequest{Branch: branch}
	if endpoint != nil {
		req.Endpoints = []CreateBranchEndpoint{*endpoint}
	}
	body, err := s.doRequest("POST", fmt.Sprintf("/projects/%s/branches", projectID), req)
	if err != nil {
		return nil, err
	}
	var resp CreateBranchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal create branch response: %w", err)
	}
	return &resp, nil
}

// DeleteBranch removes a branch. Required for cleanup when free-tier projects
// hit their 10-branch cap or when the user discards a snapshot.
func (s *Service) DeleteBranch(projectID, branchID string) error {
	_, err := s.doRequest("DELETE", fmt.Sprintf("/projects/%s/branches/%s", projectID, branchID), nil)
	return err
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

// GetProjectConsumption fetches recent consumption rollups for a single
// project. Callers compare values against the project's quota to fire 80%/95%
// alerts. Granularity = "monthly"|"daily"|"hourly".
func (s *Service) GetProjectConsumption(projectID, granularity string, from, to time.Time) (*ProjectConsumption, error) {
	if granularity == "" {
		granularity = "daily"
	}
	q := url.Values{}
	q.Set("project_ids", projectID)
	q.Set("granularity", granularity)
	if !from.IsZero() {
		q.Set("from", from.UTC().Format(time.RFC3339))
	}
	if !to.IsZero() {
		q.Set("to", to.UTC().Format(time.RFC3339))
	}
	body, err := s.doRequest("GET", "/consumption_history/projects?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var resp ConsumptionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal consumption response: %w", err)
	}
	for i := range resp.Projects {
		if resp.Projects[i].ProjectID == projectID {
			return &resp.Projects[i], nil
		}
	}
	if len(resp.Projects) > 0 {
		return &resp.Projects[0], nil
	}
	return &ProjectConsumption{ProjectID: projectID}, nil
}
