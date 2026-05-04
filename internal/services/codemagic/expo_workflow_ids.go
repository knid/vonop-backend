package codemagic

const (
	WorkflowExpoAndroidBuild  = "expo-android-build"
	WorkflowExpoIOSBuild      = "expo-ios-build"
	WorkflowExpoAndroidDeploy = "expo-android-deploy"
	WorkflowExpoIOSDeploy     = "expo-ios-deploy"
	// P1 #12 (mobile audit): JS-only OTA publish — runs `expo export` only,
	// no native gradle/xcodebuild leg. ~3 min vs 25 min for the full build.
	WorkflowExpoOTAPublish = "expo-ota-publish"
)
