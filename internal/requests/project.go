package requests

type ProjectCreateRequest struct {
	URL                     string           `json:"url" validate:"required" errormgs:"must be a valid url"`
	Title                   string           `json:"title" validate:"min=3,max=20,required" errormgs:"must be between 3 and 20 characters long"`
	Description             string           `json:"description" validate:"required" errormgs:"is required"`
	SplashScreenColor       string           `json:"ss_color" validate:"required" errormgs:"is required"`
	AppIcon                 string           `json:"icon" validate:"required" errormgs:"is required"`
	BuildForAndroid         bool             `json:"build_for_android"`
	BuildForIOS             bool             `json:"build_for_ios"`
	UserAppleTeamID         string           `json:"user_apple_team_id"`
	P8PrivateKey            string           `json:"p8_key_file"`
	UserAppleIssuerID       string           `json:"apple_issuer_id"`
	UseAutoDeployForIOS     bool             `json:"use_auto_deploy_for_ios"`
	UseAutoDeployForAndroid bool             `json:"use_auto_deploy_for_android"`
	RemoveWemobBranding     bool             `json:"remove_wemob_branding"`
	NavBarItems             []map[string]any `json:"navbar_items"`
	NavBarBackgroundColor   string           `json:"navbar_bg_color"`
	JSInjectionData         string           `json:"js_injection_data"`
}
