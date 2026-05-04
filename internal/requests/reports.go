package requests

type ReportBuildRequest struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Secret  string `json:"secret"`
	TaskID  string `json:"task_id"`
}

type ReportDeploymentRequest struct {
	Platform     string `json:"platform"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	Secret       string `json:"secret"`
	DeploymentID string `json:"deployment_id"`
}
