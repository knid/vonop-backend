package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

func (v *HttpViews) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	var req requests.CreateOrganizationRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	result, err := v.Controller.CreateOrganization(user, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	v.Controller.TrackUserEvent(user, "organization_created", posthog.NewProperties().
		Set("org_name", req.Name))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_ORGANIZATION_CREATED, map[string]any{
		"org_name": req.Name,
	})

	utils.JSONResponse(w, result, http.StatusCreated)
}

func (v *HttpViews) GetUserOrganizations(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	result, err := v.Controller.GetUserOrganizations(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) GetOrganization(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "organization_id"})
		return
	}

	result, err := v.Controller.GetOrganization(user, orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "organization_id"})
		return
	}

	var req requests.UpdateOrganizationRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	result, err := v.Controller.UpdateOrganization(user, orgID, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "organization_id"})
		return
	}

	err = v.Controller.DeleteOrganization(user, orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, nil, http.StatusNoContent)
}

func (v *HttpViews) GetOrganizationMembers(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "organization_id"})
		return
	}

	result, err := v.Controller.GetOrganizationMembers(user, orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) InviteOrganizationMember(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "organization_id"})
		return
	}

	var req requests.InviteMemberRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	result, err := v.Controller.InviteOrganizationMember(user, orgID, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	v.Controller.TrackUserEvent(user, "organization_member_invited", posthog.NewProperties().
		Set("org_id", orgID))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_ORGANIZATION_MEMBER_INVITED, map[string]any{
		"org_id": orgID,
	})

	utils.JSONResponse(w, result, http.StatusCreated)
}

func (v *HttpViews) AcceptOrganizationInvite(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "invite_token"})
		return
	}

	result, err := v.Controller.AcceptOrganizationInvite(user, token)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) RemoveOrganizationMember(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	memberID := chi.URLParam(r, "memberId")
	if orgID == "" || memberID == "" {
		utils.JSONError(w, constants.ErrValidationRequired, "organization_id and member_id are required.", http.StatusBadRequest, map[string]any{"fields": []string{"organization_id", "member_id"}})
		return
	}

	err = v.Controller.RemoveOrganizationMember(user, orgID, memberID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, nil, http.StatusNoContent)
}

func (v *HttpViews) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	memberID := chi.URLParam(r, "memberId")
	if orgID == "" || memberID == "" {
		utils.JSONError(w, constants.ErrValidationRequired, "organization_id and member_id are required.", http.StatusBadRequest, map[string]any{"fields": []string{"organization_id", "member_id"}})
		return
	}

	var req requests.UpdateMemberRoleRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	result, err := v.Controller.UpdateMemberRole(user, orgID, memberID, req)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) ListPendingInvites(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "organization_id"})
		return
	}
	result, err := v.Controller.ListPendingInvites(user, orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) CancelOrganizationInvite(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	orgID := chi.URLParam(r, "orgId")
	inviteID := chi.URLParam(r, "inviteId")
	if orgID == "" || inviteID == "" {
		utils.JSONError(w, constants.ErrValidationRequired, "organization_id and invite_id are required", http.StatusBadRequest, nil)
		return
	}
	if err := v.Controller.CancelOrganizationInvite(user, orgID, inviteID); err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, nil, http.StatusNoContent)
}

func (v *HttpViews) TransferOrganizationOwnership(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "organization_id"})
		return
	}
	var req requests.TransferOwnershipRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	result, err := v.Controller.TransferOrganizationOwnership(user, orgID, req.NewOwnerUserID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) GetOrgCreditsForAdmin(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	orgID := chi.URLParam(r, "orgId")
	result, err := v.Controller.GetOrgCreditsViewForAdmin(user, orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) GetOrgCreditsForMember(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	orgID := chi.URLParam(r, "orgId")
	result, err := v.Controller.GetOrgCreditsViewForMember(user, orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) SetMemberCreditCap(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	orgID := chi.URLParam(r, "orgId")
	memberID := chi.URLParam(r, "memberId")
	var req requests.SetMemberCapRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	result, err := v.Controller.SetMemberCreditCap(user, orgID, memberID, req.MonthlyCreditCap)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}
