package views

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

type finalizeDeepAgentsRunRequest struct {
	Successful bool   `json:"successful"`
	Summary    string `json:"summary"`
}

func (v *HttpViews) FinalizeDeepAgentsRunCallback(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "runId"))
	if err != nil {
		utils.JSONError(w, "invalid_runtime_run_id", "invalid runtime run id", http.StatusBadRequest, nil)
		return
	}
	var req finalizeDeepAgentsRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "invalid_request", "invalid request body", http.StatusBadRequest, nil)
		return
	}
	result, err := v.Controller.FinalizeDeepAgentsRun(r.Context(), controllers.DeepAgentsFinalizeRequest{
		AgentRuntimeRunID: runID,
		Successful:        req.Successful,
		Summary:           req.Summary,
	})
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}
