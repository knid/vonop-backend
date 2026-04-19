package requests

// DraftListingRequest asks the backend to generate candidate title +
// description + release-notes copy for an app's store listing. The
// Pilot calls this when the user clicks "Draft with AI" on a listing
// field; the user can accept, regenerate, or edit before submission.
type DraftListingRequest struct {
	// Target store — affects tone + length: App Store tends to want
	// richer copy, Play Store accepts plainer text.
	Target string `json:"target"` // "app_store" | "play_store"

	// Kind scopes the generation to a single artefact at a time so
	// users can regenerate one field without disturbing others.
	Kind string `json:"kind"` // "title" | "description" | "release_notes"

	// Optional hints the user may provide to steer the draft.
	Hint string `json:"hint,omitempty"`
}
