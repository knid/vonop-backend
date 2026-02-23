package tasks

const (
	TypeBuildFlutterAndroid = "build:flutter_android"
	TypeBuildFlutterIOS     = "build:flutter_ios"
)

type FlutterBuildPayload struct {
	AppProjectID  string `json:"app_project_id"`
	Platform      string `json:"platform"` // "android" or "ios"
	BuildID       string `json:"build_id"`
	GeneratedCode string `json:"generated_code"` // S3 path to ZIP of generated Flutter code
	PackageName   string `json:"package_name"`
	AppName       string `json:"app_name"`
	Version       int    `json:"version"`
	Secret        string `json:"secret"` // worker auth secret
}
