package views

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/agentruntime"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetRuntimeAccess(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil || user == nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if v.Controller.DeepAgentsClient == nil {
		utils.JSONError(w, "runtime_unavailable", "Deep Agents runtime is not configured", http.StatusServiceUnavailable, nil)
		return
	}
	jobID := chi.URLParam(r, "jobId")
	job, err := v.Controller.GetJob(jobID, user.ID)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	project, err := v.Controller.DB.GetAppProjectByID(job.AppID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	runtimeRun, err := v.Controller.DB.GetAgentRuntimeRunByGenerationJob(r.Context(), job.ID)
	if err != nil {
		utils.JSONError(w, "runtime_not_found", "runtime metadata not found for job", http.StatusNotFound, nil)
		return
	}
	orgID := runtimeRun.OrganizationID
	runtimeAPIURL, _ := runtimeRun.Metadata["runtime_api_url"].(string)
	if runtimeAPIURL == "" && runtimeRun.DeepAgentsRunID != "" {
		if run, getErr := v.Controller.DeepAgentsClient.GetRun(r.Context(), runtimeRun.DeepAgentsRunID); getErr == nil && run.RuntimeAPIURL != "" {
			runtimeAPIURL = run.RuntimeAPIURL
			if runtimeRun.Metadata == nil {
				runtimeRun.Metadata = map[string]any{}
			}
			runtimeRun.Metadata["runtime_api_url"] = runtimeAPIURL
			_ = v.Controller.DB.UpdateAgentRuntimeRun(r.Context(), runtimeRun)
		}
	}
	access, err := v.Controller.DeepAgentsClient.IssueRuntimeAccessToken(agentruntime.RuntimeAccessRequest{
		UserID:        user.ID.String(),
		OrgID:         orgID.String(),
		ProjectID:     project.ID.String(),
		ThreadID:      runtimeRun.DeepAgentsThreadID,
		AssistantID:   agentruntime.DefaultAssistantID,
		RuntimeAPIURL: runtimeAPIURL,
		ExpiresIn:     30 * time.Minute,
	})
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, access, http.StatusOK)
}
