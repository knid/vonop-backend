package requests

import (
	"github.com/go-playground/validator/v10"

	"github.com/knid/wemob/internal/utils"
)

// UpdateMinVersionRequest is the body of PATCH /v2/apps/{appId}/min-version.
//
// MinBuildNumber is the floor — any installed build with a native build
// number strictly less than this triggers the cold-start force-update gate
// in the mobile app. Set to 0 to disable the gate.
//
// Message is shown above the store-link button. Empty string falls back to
// the backend's default English message.
type UpdateMinVersionRequest struct {
	Platform       string `json:"platform" validate:"required,oneof=ios android" errormgs:"platform must be ios or android"`
	MinBuildNumber int64  `json:"min_build_number" validate:"gte=0" errormgs:"min_build_number must be >= 0"`
	Message        string `json:"message" validate:"max=500" errormgs:"message must be at most 500 characters"`
}

func (r *UpdateMinVersionRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateMinVersionRequest](*r, validate)
}
