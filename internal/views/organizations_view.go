package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

func (v *HttpViews) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	var req requests.CreateOrganizationRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.CreateOrganization(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "organization_created", posthog.NewProperties().
		Set("org_name", req.Name))

	utils.JSONResponse(w, result, http.StatusCreated)
}

func (v *HttpViews) GetUserOrganizations(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	result, err := v.Controller.GetUserOrganizations(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) GetOrganization(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorResponse(w, "organization id is required", http.StatusBadRequest)
		return
	}

	result, err := v.Controller.GetOrganization(user, orgID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorResponse(w, "organization id is required", http.StatusBadRequest)
		return
	}

	var req requests.UpdateOrganizationRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.UpdateOrganization(user, orgID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorResponse(w, "organization id is required", http.StatusBadRequest)
		return
	}

	err = v.Controller.DeleteOrganization(user, orgID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, nil, http.StatusNoContent)
}

func (v *HttpViews) GetOrganizationMembers(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorResponse(w, "organization id is required", http.StatusBadRequest)
		return
	}

	result, err := v.Controller.GetOrganizationMembers(user, orgID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) InviteOrganizationMember(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		utils.JSONErrorResponse(w, "organization id is required", http.StatusBadRequest)
		return
	}

	var req requests.InviteMemberRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.InviteOrganizationMember(user, orgID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "organization_member_invited", posthog.NewProperties().
		Set("org_id", orgID))

	utils.JSONResponse(w, result, http.StatusCreated)
}

func (v *HttpViews) AcceptOrganizationInvite(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		utils.JSONErrorResponse(w, "invite token is required", http.StatusBadRequest)
		return
	}

	result, err := v.Controller.AcceptOrganizationInvite(user, token)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) RemoveOrganizationMember(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	memberID := chi.URLParam(r, "memberId")
	if orgID == "" || memberID == "" {
		utils.JSONErrorResponse(w, "organization id and member id are required", http.StatusBadRequest)
		return
	}

	err = v.Controller.RemoveOrganizationMember(user, orgID, memberID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, nil, http.StatusNoContent)
}

func (v *HttpViews) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	orgID := chi.URLParam(r, "orgId")
	memberID := chi.URLParam(r, "memberId")
	if orgID == "" || memberID == "" {
		utils.JSONErrorResponse(w, "organization id and member id are required", http.StatusBadRequest)
		return
	}

	var req requests.UpdateMemberRoleRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.UpdateMemberRole(user, orgID, memberID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
