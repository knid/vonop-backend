package views

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/fal"
	"github.com/knid/wemob/internal/utils"
)

type finalizeDeepAgentsRunRequest struct {
	Successful bool   `json:"successful"`
	Summary    string `json:"summary"`
}

type deepAgentsGenerateImageRequest struct {
	Prompt string `json:"prompt"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Style  string `json:"style"`
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

func (v *HttpViews) ListDeepAgentsRunAssets(w http.ResponseWriter, r *http.Request) {
	run, ok := v.deepAgentsRunFromRequest(w, r)
	if !ok {
		return
	}
	assets, err := v.Controller.DB.GetAppAssetsByProjectID(run.AppProjectID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	result := make([]responses.AssetResponse, 0, len(assets))
	for _, a := range assets {
		url := a.PublicURL
		if a.S3Key != "" && v.Controller.AssetsStorage != nil {
			if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), a.S3Key, 3600); err == nil {
				url = presigned.URL
			}
		}
		result = append(result, responses.AssetResponse{
			ID:          a.ID.String(),
			FileName:    a.FileName,
			ContentType: a.ContentType,
			FileSize:    a.FileSize,
			PublicURL:   url,
			CreatedAt:   a.CreatedAt,
		})
	}
	utils.JSONResponse(w, map[string]any{"assets": result}, http.StatusOK)
}

func (v *HttpViews) GenerateDeepAgentsRunImage(w http.ResponseWriter, r *http.Request) {
	run, ok := v.deepAgentsRunFromRequest(w, r)
	if !ok {
		return
	}
	var req deepAgentsGenerateImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.JSONError(w, "invalid_request", "invalid request body", http.StatusBadRequest, nil)
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		utils.JSONError(w, "invalid_prompt", "prompt is required", http.StatusBadRequest, nil)
		return
	}
	if req.Width <= 0 {
		req.Width = 1024
	}
	if req.Height <= 0 {
		req.Height = 1024
	}
	apiKey := v.Controller.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		utils.JSONError(w, "image_generation_unavailable", "image generation service is not configured", http.StatusServiceUnavailable, nil)
		return
	}
	model := v.Controller.SecretManager.GetSecret("FAL_IMAGE_MODEL")
	if model == "" {
		model = "fal-ai/flux/schnell"
	}
	prompt := req.Prompt
	if req.Style != "" {
		prompt = prompt + "\nStyle: " + req.Style
	}
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	imageURL, err := fal.GenerateImage(ctx, apiKey, model, prompt, falImageSize(req.Width, req.Height))
	if err != nil {
		slog.Error("deepagents image generation failed", "error", err, "runtime_run_id", run.ID, "project_id", run.AppProjectID)
		utils.JSONInternalError(w, err)
		return
	}
	assetID := uuid.New()
	asset := &models.AppAsset{
		ID:           assetID,
		AppProjectID: run.AppProjectID,
		FileName:     fmt.Sprintf("agent-image-%s.jpg", assetID.String()[:8]),
		ContentType:  "image/jpeg",
		FileSize:     0,
		S3Key:        "",
		PublicURL:    imageURL,
	}
	if _, err := v.Controller.DB.CreateAppAsset(asset); err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, map[string]any{
		"asset": responses.AssetResponse{
			ID:          asset.ID.String(),
			FileName:    asset.FileName,
			ContentType: asset.ContentType,
			FileSize:    asset.FileSize,
			PublicURL:   asset.PublicURL,
			CreatedAt:   asset.CreatedAt,
		},
		"url": imageURL,
	}, http.StatusOK)
}

func (v *HttpViews) deepAgentsRunFromRequest(w http.ResponseWriter, r *http.Request) (*models.AgentRuntimeRun, bool) {
	runID, err := uuid.Parse(chi.URLParam(r, "runId"))
	if err != nil {
		utils.JSONError(w, "invalid_runtime_run_id", "invalid runtime run id", http.StatusBadRequest, nil)
		return nil, false
	}
	run, err := v.Controller.DB.GetAgentRuntimeRun(r.Context(), runID)
	if err != nil {
		utils.JSONError(w, "runtime_run_not_found", "runtime run not found", http.StatusNotFound, nil)
		return nil, false
	}
	return run, true
}

func falImageSize(width, height int) string {
	if width == height {
		if width >= 1024 {
			return "square_hd"
		}
		return "square"
	}
	if width > height {
		return "landscape_4_3"
	}
	return "portrait_4_3"
}
