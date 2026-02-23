package vercel

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

// ansiRegex strips ANSI escape sequences (colors, cursor movement, etc.)
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x1b]*\x1b\\`)

const vercelAPIBase = "https://api.vercel.com"

type Service struct {
	Token  string
	TeamID string
}

func NewService(token, teamID string) *Service {
	return &Service{Token: token, TeamID: teamID}
}

// File represents a file to deploy
type File struct {
	Path    string `json:"file"`
	Content string `json:"-"`
	SHA     string `json:"sha"`
	Size    int    `json:"size"`
}

// CreateProjectRequest is the request body for creating a Vercel project
type CreateProjectRequest struct {
	Name      string `json:"name"`
	Framework string `json:"framework,omitempty"`
}

// CreateProjectResponse is the response from creating a Vercel project
type CreateProjectResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DeploymentFile is a file in the deployment request
type DeploymentFile struct {
	File string `json:"file"`
	SHA  string `json:"sha"`
	Size int    `json:"size"`
}

// DeployRequest is the request body for creating a deployment
type DeployRequest struct {
	Name            string           `json:"name"`
	Files           []DeploymentFile `json:"files"`
	ProjectID       string           `json:"project,omitempty"`
	Target          string           `json:"target,omitempty"` // "production" or "preview"
	ProjectSettings *ProjectSettings `json:"projectSettings,omitempty"`
}

type ProjectSettings struct {
	Framework       string `json:"framework,omitempty"`
	BuildCommand    string `json:"buildCommand,omitempty"`
	OutputDirectory string `json:"outputDirectory,omitempty"`
	InstallCommand  string `json:"installCommand,omitempty"`
}

// DeployResponse is the response from creating a deployment
type DeployResponse struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	State  string `json:"readyState"`
	Status string `json:"status"`
}

// EnvVar represents a Vercel environment variable
type EnvVar struct {
	Key    string   `json:"key"`
	Value  string   `json:"value"`
	Type   string   `json:"type"`   // "plain", "secret", "encrypted"
	Target []string `json:"target"` // ["production", "preview", "development"]
}

// DomainRequest represents a domain to add to a project
type DomainRequest struct {
	Name string `json:"name"`
}

func (s *Service) teamQuery() string {
	if s.TeamID != "" {
		return "?teamId=" + s.TeamID
	}
	return ""
}

func (s *Service) doRequest(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, vercelAPIBase+path+s.teamQuery(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		slog.Error("Vercel API error", "status", resp.StatusCode, "path", path, "body", string(respBody))
		return nil, fmt.Errorf("vercel API error: status %d", resp.StatusCode)
	}

	return respBody, nil
}

// CreateProject creates a new Vercel project
func (s *Service) CreateProject(name string) (*CreateProjectResponse, error) {
	body, err := s.doRequest("POST", "/v10/projects", CreateProjectRequest{
		Name:      name,
		Framework: "nextjs",
	})
	if err != nil {
		return nil, err
	}

	var resp CreateProjectResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal create project response: %w", err)
	}
	return &resp, nil
}

// UploadFile uploads a single file to Vercel's blob store
func (s *Service) UploadFile(content []byte) error {
	req, err := http.NewRequest("POST", vercelAPIBase+"/v2/files"+s.teamQuery(), bytes.NewReader(content))
	if err != nil {
		return fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/octet-stream")
	h := sha1.New()
	h.Write(content)
	req.Header.Set("x-vercel-digest", hex.EncodeToString(h.Sum(nil)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("Vercel file upload error", "status", resp.StatusCode, "body", string(body))
		return fmt.Errorf("vercel upload error: status %d", resp.StatusCode)
	}

	return nil
}

// DeployFiles creates a deployment with the given files
func (s *Service) DeployFiles(projectID string, files map[string]string, target string) (*DeployResponse, error) {
	var deployFiles []DeploymentFile

	for path, content := range files {
		contentBytes := []byte(content)

		// Upload file content first
		if err := s.UploadFile(contentBytes); err != nil {
			return nil, fmt.Errorf("upload file %s: %w", path, err)
		}

		h := sha1.New()
		h.Write(contentBytes)

		deployFiles = append(deployFiles, DeploymentFile{
			File: path,
			SHA:  hex.EncodeToString(h.Sum(nil)),
			Size: len(contentBytes),
		})
	}

	deployReq := DeployRequest{
		Name:      projectID,
		Files:     deployFiles,
		ProjectID: projectID,
		Target:    target,
		ProjectSettings: &ProjectSettings{
			Framework: "nextjs",
		},
	}

	body, err := s.doRequest("POST", "/v13/deployments", deployReq)
	if err != nil {
		return nil, err
	}

	var resp DeployResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal deploy response: %w", err)
	}
	return &resp, nil
}

// SetEnvVars sets environment variables on a project
func (s *Service) SetEnvVars(projectID string, envVars map[string]string) error {
	for key, value := range envVars {
		ev := EnvVar{
			Key:    key,
			Value:  value,
			Type:   "encrypted",
			Target: []string{"production", "preview", "development"},
		}
		_, err := s.doRequest("POST", fmt.Sprintf("/v10/projects/%s/env", projectID), ev)
		if err != nil {
			slog.Warn("Failed to set env var, may already exist, trying update", "key", key, "error", err)
			// Try PATCH if POST fails (env var may already exist)
			_, err = s.doRequest("PATCH", fmt.Sprintf("/v10/projects/%s/env/%s", projectID, key), map[string]any{
				"value":  value,
				"type":   "encrypted",
				"target": []string{"production", "preview", "development"},
			})
			if err != nil {
				return fmt.Errorf("set env var %s: %w", key, err)
			}
		}
	}
	return nil
}

// AddDomain adds a custom domain to a project
func (s *Service) AddDomain(projectID string, domain string) error {
	_, err := s.doRequest("POST", fmt.Sprintf("/v10/projects/%s/domains", projectID), DomainRequest{Name: domain})
	return err
}

// RemoveDomain removes a custom domain from a project
func (s *Service) RemoveDomain(projectID string, domain string) error {
	_, err := s.doRequest("DELETE", fmt.Sprintf("/v10/projects/%s/domains/%s", projectID, domain), nil)
	return err
}

// GetDeployment gets the current status of a deployment
func (s *Service) GetDeployment(deploymentID string) (*DeployResponse, error) {
	body, err := s.doRequest("GET", fmt.Sprintf("/v13/deployments/%s", deploymentID), nil)
	if err != nil {
		return nil, err
	}

	var resp DeployResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal deployment response: %w", err)
	}
	return &resp, nil
}

// DisableDeploymentProtection disables SSO protection on all deployments
// so that deployed URLs are publicly accessible without Vercel login.
func (s *Service) DisableDeploymentProtection(projectID string) error {
	_, err := s.doRequest("PATCH", fmt.Sprintf("/v9/projects/%s", projectID), map[string]any{
		"ssoProtection": nil,
	})
	if err != nil {
		slog.Warn("Failed to disable deployment protection", "project_id", projectID, "error", err)
	}
	return err
}

// GetDeploymentBuildLog fetches build log events for a deployment and returns build output text.
func (s *Service) GetDeploymentBuildLog(deploymentID string) (string, error) {
	// Build URL with builds=1 to get build output and direction=backward to get latest events
	path := fmt.Sprintf("/v3/deployments/%s/events?builds=1&direction=forward", deploymentID)

	// Use custom request with Accept header to ensure JSON (not streaming) response
	reqURL := vercelAPIBase + path + "&teamId=" + s.TeamID
	if s.TeamID == "" {
		reqURL = vercelAPIBase + path
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		slog.Warn("Vercel events API error", "status", resp.StatusCode, "deployment_id", deploymentID, "body", string(body[:min(len(body), 500)]))
		return "", fmt.Errorf("vercel events API error: status %d", resp.StatusCode)
	}

	// The response can be either a JSON array or newline-delimited JSON objects.
	// Try JSON array first.
	type eventPayload struct {
		Text string `json:"text"`
	}
	type buildEvent struct {
		Type    string       `json:"type"`
		Created int64        `json:"created"`
		Payload eventPayload `json:"payload"`
	}

	var events []buildEvent
	if err := json.Unmarshal(body, &events); err != nil {
		// Fallback: try newline-delimited JSON
		lines := bytes.Split(body, []byte("\n"))
		for _, line := range lines {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var ev buildEvent
			if json.Unmarshal(line, &ev) == nil {
				events = append(events, ev)
			}
		}
		if len(events) == 0 {
			// Last resort: return raw body as text (capped), strip ANSI codes
			raw := ansiRegex.ReplaceAllString(string(body), "")
			if len(raw) > 4000 {
				raw = raw[:4000] + "\n... [truncated]"
			}
			return raw, nil
		}
	}

	// Collect log lines and strip ANSI escape codes
	var result strings.Builder
	for _, ev := range events {
		text := ev.Payload.Text
		if text == "" {
			continue
		}
		text = ansiRegex.ReplaceAllString(text, "")
		if result.Len()+len(text) > 8000 {
			result.WriteString("\n... [truncated]")
			break
		}
		result.WriteString(text)
		result.WriteString("\n")
	}

	return result.String(), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// DeleteProject deletes a Vercel project
func (s *Service) DeleteProject(projectID string) error {
	_, err := s.doRequest("DELETE", fmt.Sprintf("/v10/projects/%s", projectID), nil)
	return err
}
