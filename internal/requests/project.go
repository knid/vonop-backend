package requests

type ProjectCreateRequest struct {
	URL               string `json:"url" validate:""`
	PackageName       string `json:"package_name"`
	Title             string `json:"title" validate:"min=3,max=20,required"`
	Description       string `json:"description" validate:"required"`
	SplashScreenColor string `json:"ss_color" validate:"required"`
	AppIcon           string `json:"icon" validate:"required"`
}
