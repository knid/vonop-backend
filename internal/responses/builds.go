package responses

import (
	"time"

	"github.com/google/uuid"
)

type BuildInformationResponse struct {
	ID                uuid.UUID `json:"id"`
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	Icon              string    `json:"icon"`
	SplashScreenColor string    `json:"splash_screen_color"`
	BuildNumber       int       `json:"build_number"`
	CreatedAt         time.Time `json:"created_at"`
	FinishedAt        time.Time `json:"finished_at"`
}

type BuildListResponse struct {
	BuildList *[]BuildInformationResponse `json:"build_list"`
}

type BuildCreateResponse struct {
	TaskID uuid.UUID `json:"task_id"`
	SuccessResponse
}

type BuildSetReleasedBuildResponse struct {
	SuccessResponse
}
