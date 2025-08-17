package requests

import "github.com/google/uuid"

type BuildCreateRequest struct {
	ProjectID         uuid.UUID
	Title             string
	Description       string
	URL               string
	Icon              string
	SplashScreenColor string
}
