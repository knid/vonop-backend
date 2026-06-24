package responses

import (
	"time"

	"github.com/google/uuid"
)

type ProjectInformationResponse struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	URL         string    `json:"url"`
	PackageName string    `json:"package_name"`
	Icon        string    `json:"icon"`
	CreatedAt   time.Time `json:"created_at"`
}

type ProjectListResponse struct {
	ProjectList []ProjectInformationResponse `json:"project_list"`
}

type ProjectUpdateResponse struct {
	ProjectInformationResponse
}

type ProjectDeleteResponse struct {
	SuccessResponse
}
