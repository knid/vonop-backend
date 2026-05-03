package requests

import (
	"github.com/go-playground/validator/v10"

	"github.com/knid/wemob/internal/utils"
)

// SubmitExpoRequest is the body for POST /v2/apps/{appId}/submit/{platform}.
//
// All fields are optional — empty / missing means "use the AppProject's
// persisted default" (the user has already configured these in the dashboard).
//
// Track validation is deferred to the controller because the valid set
// differs per platform (Apple: internal/external/production; Play: internal/
// alpha/beta/production).
type SubmitExpoRequest struct {
	// Track override for this submission. When empty, uses the project's
	// AppStoreTrack (iOS) or PlayStoreTrack (Android).
	Track string `json:"track,omitempty"`
	// PhasedRelease override. nil pointer = use AppProject.PhasedRelease.
	PhasedRelease *bool `json:"phased_release,omitempty"`
	// SubmitAsDraft override. nil pointer = use AppProject.SubmitAsDraft.
	SubmitAsDraft *bool `json:"submit_as_draft,omitempty"`
	// RolloutFraction is the staged-rollout percentage as a fraction of 1.0
	// (e.g. 0.05 = 5% rollout). 0 means no staged rollout. Android-only,
	// production track only.
	RolloutFraction float64 `json:"rollout_fraction,omitempty" validate:"omitempty,min=0,max=1" errormgs:"must be between 0 and 1"`
	// SubmitToAppStore: iOS-only. When true, additionally push to App Store
	// review (production submission). Defaults to false.
	SubmitToAppStore bool `json:"submit_to_app_store,omitempty"`
}

func (r *SubmitExpoRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[SubmitExpoRequest](*r, validate)
}
