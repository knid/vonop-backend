package responses

// MinVersionResponse is returned by GET /v1/mobile/{slugOrID}/min-version.
//
// The mobile cold-start gate compares MinBuildNumber against the installed
// app's native build number (Application.nativeBuildVersion from
// expo-application). When MinBuildNumber > installed, the gate renders a
// non-skippable "Update Required" screen with a button that opens StoreURL.
// MinBuildNumber=0 disables the gate.
//
// Message is the operator-customizable copy shown above the button. The
// backend fills in a default English string when the customer hasn't set one.
type MinVersionResponse struct {
	MinBuildNumber int64  `json:"min_build_number"`
	StoreURL       string `json:"store_url"`
	Message        string `json:"message"`
}

// AdminMinVersionResponse is returned by PATCH /v2/apps/{appId}/min-version.
// Mirrors the GET shape plus the platform that was updated, so the client
// doesn't have to round-trip the request body to know which platform it
// touched.
type AdminMinVersionResponse struct {
	Platform       string `json:"platform"`
	MinBuildNumber int64  `json:"min_build_number"`
	Message        string `json:"message"`
}
