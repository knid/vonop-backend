package responses

import "github.com/google/uuid"

type ProjectInformationResponse struct {
	ID            uuid.UUID `json:"id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Icon          string    `json:"icon"`
	Version       int       `json:"version"`
	URL           string    `json:"url"`
	PackageName   string    `json:"package_name"`
	ActiveBuildID uuid.UUID `json:"active_build"`
}

type ProjectListResponse struct {
	ProjectList []*ProjectInformationResponse `json:"project_list"`
}

type ProjectCreateResponse struct {
	TaskID uuid.UUID `json:"task_id"`
	SuccessResponse
	// ProjectInformationResponse
}

type ProjectUpdateResponse struct {
	ProjectInformationResponse
}

type ProjectDeleteResponse struct {
	SuccessResponse
}
