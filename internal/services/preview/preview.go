package preview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Service struct {
	BaseURL    string
	Secret     string
	httpClient *http.Client
}

func NewService(baseURL, secret string) *Service {
	return &Service{
		BaseURL: baseURL,
		Secret:  secret,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type PreviewStatus struct {
	Status     string `json:"status"`      // "starting", "ready", "error", "stopped"
	PreviewURL string `json:"preview_url"`
	Error      string `json:"error,omitempty"`
}

type createRequest struct {
	Files   map[string]string `json:"files"`
	EnvVars map[string]string `json:"env_vars,omitempty"`
}

type updateFilesRequest struct {
	Files map[string]string `json:"files"`
}

func (s *Service) doRequest(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, s.BaseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Preview-Secret", s.Secret)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("preview server returned %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// CreatePreview creates a new preview container with the given files
func (s *Service) CreatePreview(projectID string, files map[string]string, envVars map[string]string) (*PreviewStatus, error) {
	data, err := s.doRequest("POST", "/projects/"+projectID, createRequest{
		Files:   files,
		EnvVars: envVars,
	})
	if err != nil {
		return nil, err
	}

	var status PreviewStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &status, nil
}

// UpdateFiles updates files in a running preview container
func (s *Service) UpdateFiles(projectID string, files map[string]string) error {
	_, err := s.doRequest("PUT", "/projects/"+projectID+"/files", updateFilesRequest{
		Files: files,
	})
	return err
}

// DeletePreview stops and removes a preview container
func (s *Service) DeletePreview(projectID string) error {
	_, err := s.doRequest("DELETE", "/projects/"+projectID, nil)
	return err
}

// GetLogs returns the last 100 lines of container logs for a project
func (s *Service) GetLogs(projectID string) (string, error) {
	data, err := s.doRequest("GET", "/projects/"+projectID+"/logs", nil)
	if err != nil {
		return "", err
	}

	var result struct {
		Logs string `json:"logs"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("failed to parse logs response: %w", err)
	}
	return result.Logs, nil
}

// GetStatus returns the current status of a preview container
func (s *Service) GetStatus(projectID string) (*PreviewStatus, error) {
	data, err := s.doRequest("GET", "/projects/"+projectID+"/status", nil)
	if err != nil {
		return nil, err
	}

	var status PreviewStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &status, nil
}
