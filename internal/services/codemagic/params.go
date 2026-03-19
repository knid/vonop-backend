package codemagic

// AndroidBuildParams maps to the android-build workflow variables.
type AndroidBuildParams struct {
	ProjectID           string
	BuildID             string
	TaskID              string
	Version             int
	Title               string
	URL                 string
	IconURL             string
	SplashColor         string
	OneSignalAppID      string
	PackageName         string
	RemoveBranding      bool
	Permissions         []string
	URLScheme           string
	UniversalLinkDomain string
}

// IOSBuildParams maps to the ios-build workflow variables.
type IOSBuildParams struct {
	ProjectID           string
	BuildID             string
	TaskID              string
	Version             int
	Title               string
	URL                 string
	IconURL             string
	SplashColor         string
	OneSignalAppID      string
	PackageName         string
	RemoveBranding      bool
	Permissions         []string
	URLScheme           string
	UniversalLinkDomain string
}

// IOSSignParams maps to the ios-sign workflow variables.
type IOSSignParams struct {
	BuildID     string
	TaskID      string
	PackageName string
	TeamID      string
	KeyID       string
	IssuerID    string
	P8KeyS3Path string
}

// AndroidDeployParams maps to the android-deploy workflow variables.
type AndroidDeployParams struct {
	BuildID              string
	DeploymentID         string
	PackageName          string
	Version              int
	Title                string
	Description          string
	Screenshots          []string
	IconPath             string
	Locale               string
	ServiceAccountS3Path string
	NewApp               bool
	ChangedBuild         bool
	TitleChanged         bool
	DescChanged          bool
	ScreenshotsChanged   bool
	IconChanged          bool
}

// IOSDeployParams maps to the ios-deploy workflow variables.
type IOSDeployParams struct {
	BuildID            string
	DeploymentID       string
	PackageName        string
	TeamID             string
	KeyID              string
	IssuerID           string
	P8KeyS3Path        string
	Title              string
	Description        string
	Screenshots        []string
	Icon               string
	Locale             string
	NewApp             bool
	ChangedBuild       bool
	TitleChanged       bool
	DescChanged        bool
	ScreenshotsChanged bool
}
