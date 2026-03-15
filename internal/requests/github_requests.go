package requests

type ExportToGitHubRequest struct {
	RepoName    string `json:"repo_name" validate:"required,min=1,max=100" errormgs:"repo name is required and must be 1-100 characters"`
	Description string `json:"description" validate:"max=350" errormgs:"description must be at most 350 characters"`
	Private     bool   `json:"private"`
}
