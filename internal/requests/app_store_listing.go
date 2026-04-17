package requests

// UpdateAppStoreListingScreenshot matches the client's screenshot tuple.
type UpdateAppStoreListingScreenshot struct {
	ID          string `json:"id"`
	CanvasState string `json:"canvas_state,omitempty"`
}

// UpdateAppStoreListingRequest is a partial update — pointer fields allow
// callers to distinguish "leave alone" (nil) from "clear" (empty string).
// Screenshots, when present, replaces the entire ordered array.
type UpdateAppStoreListingRequest struct {
	IconS3Key        *string                            `json:"icon_s3_key,omitempty"`
	Title            *string                            `json:"title,omitempty"`
	Description      *string                            `json:"description,omitempty"`
	Locale           *string                            `json:"locale,omitempty"`
	Screenshots      *[]UpdateAppStoreListingScreenshot `json:"screenshots,omitempty"`
	PrivacyPolicyURL *string                            `json:"privacy_policy_url,omitempty"`
}
