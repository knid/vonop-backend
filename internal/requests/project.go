package requests

type ProjectCreateRequest struct {
	URL               string `json:"url" validator:""`
	PackageName       string `json:"package_name"`
	Title             string `json:"title" validator:"min=3,max=20,required"`
	Description       string `json:"description" validator:"required"`
	SplashScreenColor string `json:"ss_color" validator:"required"`
	AppIcon           string `json:"icon" validator:"required"`
}
