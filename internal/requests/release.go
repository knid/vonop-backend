package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/utils"
)

// RollbackReleaseRequest is the body for POST /v2/apps/{appId}/releases/rollback.
//
// Both ids must be Releases for the same AppProject (the controller doesn't
// dedupe across projects — caller's responsibility). FromReleaseID is the
// currently-live release we're pulling back; TargetReleaseID is the older
// release we're promoting back into the live slot.
type RollbackReleaseRequest struct {
	FromReleaseID   uuid.UUID `json:"from_release_id" validate:"required" errormgs:"from_release_id is required"`
	TargetReleaseID uuid.UUID `json:"target_release_id" validate:"required" errormgs:"target_release_id is required"`
}

func (r *RollbackReleaseRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[RollbackReleaseRequest](*r, validate)
}
