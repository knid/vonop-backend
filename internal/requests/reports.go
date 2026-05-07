package requests

type ReportDeploymentRequest struct {
	Platform     string `json:"platform"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	Secret       string `json:"secret"`
	DeploymentID string `json:"deployment_id"`
}
