package gitea

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

type Service struct {
	BaseURL  string // e.g. https://git.wemob.io
	Token    string
	Username string // Gitea username that owns the token
	Org      string // Gitea organization name
	SSHPort  string // SSH port for git clone (e.g. "22222")
}

func NewService(baseURL, token, username, org, sshPort string) *Service {
	if sshPort == "" {
		sshPort = "22"
	}
	return &Service{BaseURL: baseURL, Token: token, Username: username, Org: org, SSHPort: sshPort}
}

type createRepoRequest struct {
	Name          string `json:"name"`
	AutoInit      bool   `json:"auto_init"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

type repoResponse struct {
	ID       int    `json:"id"`
	CloneURL string `json:"clone_url"`
	HTMLURL  string `json:"html_url"`
}

func (s *Service) doRequest(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("gitea: marshal: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, s.BaseURL+"/api/v1"+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("gitea: create request: %w", err)
	}
	req.Header.Set("Authorization", "token "+s.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitea: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitea: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		bodyStr := string(respBody)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		slog.Error("Gitea API error", "status", resp.StatusCode, "path", path, "body", bodyStr)
		return nil, fmt.Errorf("gitea API error: status %d", resp.StatusCode)
	}

	return respBody, nil
}

// CreateRepo creates a new repository under the configured organization.
// Returns the authenticated clone URL for git push.
func (s *Service) CreateRepo(name string) (string, error) {
	body, err := s.doRequest("POST", fmt.Sprintf("/orgs/%s/repos", s.Org), createRepoRequest{
		Name:          name,
		AutoInit:      false,
		DefaultBranch: "main",
		Private:       true,
	})
	if err != nil {
		return "", err
	}

	var resp repoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("gitea: unmarshal create repo: %w", err)
	}

	return s.AuthenticatedCloneURL(name), nil
}

// DeleteRepo deletes a repository.
func (s *Service) DeleteRepo(name string) error {
	_, err := s.doRequest("DELETE", fmt.Sprintf("/repos/%s/%s", s.Org, name), nil)
	return err
}

// AuthenticatedCloneURL returns the clone URL with the API token embedded
// for use with git push. The token is NOT persisted in .git/config because
// PushToURL uses it as a one-shot URL argument.
func (s *Service) AuthenticatedCloneURL(repoName string) string {
	scheme := "https://"
	host := s.BaseURL
	if len(host) > 8 && host[:8] == "https://" {
		host = host[8:]
	} else if len(host) > 7 && host[:7] == "http://" {
		scheme = "http://"
		host = host[7:]
	}
	return fmt.Sprintf("%s%s:%s@%s/%s/%s.git", scheme, s.Username, s.Token, host, s.Org, repoName)
}

// SSHCloneURL returns the SSH clone URL for use with deploy keys.
// Uses ssh:// format to support non-standard ports.
// Format: ssh://git@host:port/org/repo.git
func (s *Service) SSHCloneURL(repoName string) string {
	host := s.BaseURL
	if len(host) > 8 && host[:8] == "https://" {
		host = host[8:]
	} else if len(host) > 7 && host[:7] == "http://" {
		host = host[7:]
	}
	return fmt.Sprintf("ssh://git@%s:%s/%s/%s.git", host, s.SSHPort, s.Org, repoName)
}
