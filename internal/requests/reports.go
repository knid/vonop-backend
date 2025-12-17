package requests

type ReportBuildRequest struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Secret  string `json:"secret"`
	TaskID  string `json:"task_id"`
}

type ReportDeploymentRequest struct {
	Status       string `json:"status"`
	Message      string `json:"message"`
	Secret       string `json:"secret"`
	DeploymentID string `json:"deployment_id"`
}

type ReportCertRequest struct {
	Status        string `json:"status"`
	Message       string `json:"message"`
	Secret        string `json:"secret"`
	CredentialsID string `json:"credentials_id"`
}

type ReportSignIPARequest struct {
	Status        string `json:"status"`
	StatusMessage string `json:"status_message"`
	TaskID        string `json:"task_id"`
}
