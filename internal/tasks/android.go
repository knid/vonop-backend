package tasks

const (
	TypeBuildAndroid  = "build:android"
	TypeDeployAndroid = "deploy:android"
)

type AndroidBuildPayload struct {
	ProjectID           string
	Version             int
	BuildID             string
	TaskID              string
	Title               string
	URL                 string
	IconURL             string
	SplashScreenColor   string
	NavBarStyle         string
	OneSignalAppID      string
	PackageName         string
	RemoveWemobBranding bool
	Secret              string
}

type AndroidDeployPayload struct {
	ServiceAccountPath string
	BuildID            string
	PackageName        string
	IconPath           string
	Title              string
	Description        string
	Screenshots        []string
	Version            int
	ChangedStoreInfo   bool
	ChangedBuild       bool
	IconChanged        bool
	TitleChanged       bool
	DescriptionChanged bool
	ScreenshotsChanged bool
	Secret             string
	DeploymentID       string
}
