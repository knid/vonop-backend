package views

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetMCPTemplates(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	_ = user
	utils.JSONResponse(w, v.Controller.ListMCPTemplates(), http.StatusOK)
}

func (v *HttpViews) GetAppMCPConnections(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	resp, err := v.Controller.GetMCPConnections(project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}

func (v *HttpViews) CreateAppMCPConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if _, err := v.Controller.RequirePermission(user, project.OrganizationID.String(), constants.PermCredentialsManage); err != nil {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
		return
	}
	var req requests.CreateMCPConnectionRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	connection, err := v.Controller.CreateMCPConnection(project, req)
	if err != nil {
		utils.JSONError(w, constants.ErrValidationRequired, err.Error(), http.StatusBadRequest, nil)
		return
	}
	utils.JSONResponse(w, v.Controller.GetSingleMCPConnectionResponse(connection), http.StatusCreated)
}

func (v *HttpViews) StartAppMCPOAuth(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if _, err := v.Controller.RequirePermission(user, project.OrganizationID.String(), constants.PermCredentialsManage); err != nil {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
		return
	}
	var req requests.StartMCPOAuthRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	resp, err := v.Controller.StartMCPOAuth(project, req)
	if err != nil {
		utils.JSONError(w, constants.ErrValidationRequired, err.Error(), http.StatusBadRequest, nil)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}

func (v *HttpViews) CompleteMCPOAuthCallback(w http.ResponseWriter, r *http.Request) {
	returnURL, err := v.Controller.CompleteMCPOAuthCallback(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, fmt.Sprintf("MCP OAuth failed: %v", err), http.StatusBadRequest)
		return
	}
	if returnURL != "" {
		http.Redirect(w, r, returnURL, http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<html><body><p>MCP connection authorized. You can close this window.</p></body></html>"))
}

func (v *HttpViews) DiscoverAppMCPConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if _, err := v.Controller.RequirePermission(user, project.OrganizationID.String(), constants.PermCredentialsManage); err != nil {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
		return
	}
	connection, tools, err := v.Controller.DiscoverMCPConnectionTools(r.Context(), project, chi.URLParam(r, "connectionId"))
	if err != nil {
		utils.JSONError(w, constants.ErrValidationRequired, err.Error(), http.StatusBadRequest, nil)
		return
	}
	utils.JSONResponse(w, v.Controller.GetMCPConnectionResponse(*connection, tools), http.StatusOK)
}

func (v *HttpViews) DeleteAppMCPConnection(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if _, err := v.Controller.RequirePermission(user, project.OrganizationID.String(), constants.PermCredentialsManage); err != nil {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
		return
	}
	connection, err := v.Controller.DB.GetMCPConnectionByID(chi.URLParam(r, "connectionId"))
	if err != nil || connection.AppProjectID != project.ID {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if err := v.Controller.DB.DeleteMCPConnection(connection); err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}

func (v *HttpViews) GetAppMCPGrants(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	resp, err := v.Controller.GetAgentMCPGrantsForProject(project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}

func (v *HttpViews) PutAppMCPGrant(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if _, err := v.Controller.RequirePermission(user, project.OrganizationID.String(), constants.PermCredentialsManage); err != nil {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
		return
	}
	var req requests.PutAgentMCPGrantRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	grant, err := v.Controller.PutAgentMCPGrant(project, chi.URLParam(r, "agentId"), chi.URLParam(r, "connectionId"), req)
	if err != nil {
		utils.JSONError(w, constants.ErrValidationRequired, err.Error(), http.StatusBadRequest, nil)
		return
	}
	utils.JSONResponse(w, responses.AgentMCPGrantResponse{
		ID:            grant.ID,
		AppProjectID:  grant.AppProjectID,
		ConnectionID:  grant.MCPConnectionID,
		AgentID:       grant.AgentID,
		Enabled:       grant.Enabled,
		DefaultPolicy: grant.DefaultPolicy,
		ToolPolicies:  grant.ToolPolicies,
		CreatedAt:     grant.CreatedAt,
		UpdatedAt:     grant.UpdatedAt,
	}, http.StatusOK)
}

func (v *HttpViews) GetOrgAgentMCPGrants(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	orgID := chi.URLParam(r, "orgId")
	if _, err := v.Controller.RequireOrgMembership(user, orgID); err != nil {
		utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
		return
	}
	resp, err := v.Controller.GetAgentMCPGrantsForOrg(orgID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}

func (v *HttpViews) SubmitMCPToolApproval(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	var req requests.SubmitMCPApprovalRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	approval, err := v.Controller.SubmitMCPApproval(user, chi.URLParam(r, "jobId"), chi.URLParam(r, "approvalId"), req.Decision)
	if err != nil {
		utils.JSONError(w, constants.ErrValidationRequired, err.Error(), http.StatusBadRequest, nil)
		return
	}
	utils.JSONResponse(w, responses.MCPApprovalResponse{
		ID:       approval.ID,
		Status:   approval.Status,
		Decision: req.Decision,
	}, http.StatusOK)
}
