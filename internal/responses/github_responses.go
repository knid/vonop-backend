package responses

import "time"

type GitHubExportResponse struct {
	ID           string     `json:"id"`
	RepoFullName string     `json:"repo_full_name"`
	RepoURL      string     `json:"repo_url"`
	Private      bool       `json:"private"`
	LastPushedAt *time.Time `json:"last_pushed_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

type GitHubConnectionResponse struct {
	Connected bool   `json:"connected"`
	Username  string `json:"username,omitempty"`
}
