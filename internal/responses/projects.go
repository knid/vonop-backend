package responses

import (
	"time"

	"github.com/google/uuid"
)

type ProjectInformationResponse struct {
	ID                      uuid.UUID `json:"id"`
	Title                   string    `json:"title"`
	Description             string    `json:"description"`
	URL                     string    `json:"url"`
	PackageName             string    `json:"package_name"`
	Icon                    string    `json:"icon"`
	CreatedAt               time.Time `json:"created_at"`
	UserAppleTeamID         string    `json:"user_apple_team_id"`
	UserAppleIssuerID       string    `json:"apple_issuer_id"`
	BuildForAndroid         bool      `json:"build_for_android"`
	BuildForIOS             bool      `json:"build_for_ios"`
	UseAutoDeployForIOS     bool      `json:"use_auto_deploy_for_ios"`
	UseAutoDeployForAndroid bool      `json:"use_auto_deploy_for_android"`
}

type ProjectListResponse struct {
	ProjectList []ProjectInformationResponse `json:"project_list"`
}

type ProjectCreateResponse struct {
	ProjectID uuid.UUID `json:"project_id"`
	TaskID    uuid.UUID `json:"task_id"`
	SuccessResponse
	// ProjectInformationResponse
}

type ProjectUpdateResponse struct {
	ProjectInformationResponse
}

type ProjectDeleteResponse struct {
	SuccessResponse
}
