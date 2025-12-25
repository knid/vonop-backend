package tasks

const (
	TypeBuildUnsignedIpa = "build:ios"
	TypeSignIOS          = "sign:ios"
	TypeDeployIOS        = "deploy:ios"
	TypeUpdateIOSCerts   = "certs:ios"
)

type BuildUnsignedIpaPayload struct {
	ProjectID           string
	Version             int
	BuildID             string
	TaskID              string
	Title               string
	URL                 string
	IconURL             string
	SplashScreenColor   string
	OneSignalAppID      string
	PackageName         string
	RemoveWemobBranding bool
	Secret              string
}

type IOSSignPayload struct {
	BuildID         string
	PackageName     string
	KeyID           string
	TeamID          string
	IssuerID        string
	P8KeyPath       string
	UnsignedIpaPath string
	TaskID          string
	Secret          string
}

type IOSDeployPayload struct {
	BuildID            string
	PackageName        string
	TeamID             string
	KeyID              string
	IssuerID           string
	P8KeyPath          string
	Title              string
	Description        string
	Screenshots        []string
	Icon               string
	NewApp             bool
	ChangedStoreInfo   bool
	ChangedBuild       bool
	TitleChanged       bool
	DescriptionChanged bool
	ScreenshotsChanged bool
	DeploymentID       string
}
