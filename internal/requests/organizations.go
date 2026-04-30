package requests

import "github.com/google/uuid"

type CreateOrganizationRequest struct {
	Name string `json:"name" validate:"required,min=2,max=100" errormgs:"name is required (2-100 characters)"`
	Slug string `json:"slug" validate:"required,min=2,max=50,alphanum" errormgs:"slug is required (2-50 alphanumeric characters)"`
	Logo string `json:"logo,omitempty"`
}

type UpdateOrganizationRequest struct {
	Name string `json:"name" validate:"required,min=2,max=100" errormgs:"name is required"`
	Logo string `json:"logo,omitempty"`
}

type InviteMemberRequest struct {
	Email string `json:"email" validate:"required,email" errormgs:"valid email is required"`
	Role  string `json:"role" validate:"required,oneof=admin member" errormgs:"role must be admin or member"`
}

type UpdateMemberRoleRequest struct {
	Role string `json:"role" validate:"required,oneof=admin member" errormgs:"role must be admin or member"`
}

type TransferOwnershipRequest struct {
	NewOwnerUserID uuid.UUID `json:"new_owner_user_id" validate:"required"`
}
