package views

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"fmt"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/utils"
)


// GET /v2/apps/{appId}/assets
func (v *HttpViews) GetAppAssets(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	assets, err := v.Controller.DB.GetAppAssetsByProjectID(appID)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to fetch assets", http.StatusInternalServerError)
		return
	}

	result := make([]responses.AssetResponse, len(assets))
	for i, a := range assets {
		result[i] = responses.AssetResponse{
			ID:          a.ID.String(),
			FileName:    a.FileName,
			ContentType: a.ContentType,
			FileSize:    a.FileSize,
			PublicURL:   a.PublicURL,
			CreatedAt:   a.CreatedAt,
		}
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// POST /v2/apps/{appId}/assets/upload-url
func (v *HttpViews) RequestAssetUploadURL(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.AssetUploadURLRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate file size
	maxSizeMB := v.Controller.RemoteConfig.GetInt(remoteconfig.MaxAssetFileSizeMB, user.ID.String())
	if req.FileSize > int64(maxSizeMB)*1024*1024 {
		utils.JSONErrorResponse(w, fmt.Sprintf("file size exceeds %dMB limit", maxSizeMB), http.StatusBadRequest)
		return
	}

	// Sanitize filename
	filename := filepath.Base(req.Filename)
	if filename == "." || filename == "/" {
		filename = "file"
	}

	id := uuid.New()
	objectKey := "apps/" + appID + "/assets/" + id.String() + "/" + filename

	presignedRequest, err := v.Controller.AssetsStorage.PutObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to generate upload URL: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build public URL by stripping query params from presigned URL
	publicURL := presignedRequest.URL
	if idx := strings.Index(publicURL, "?"); idx != -1 {
		publicURL = publicURL[:idx]
	}

	// Create DB record
	asset := &models.AppAsset{
		ID:           id,
		AppProjectID: uuid.MustParse(appID),
		FileName:     filename,
		ContentType:  req.ContentType,
		FileSize:     req.FileSize,
		S3Key:        objectKey,
		PublicURL:    publicURL,
	}
	if _, err := v.Controller.DB.CreateAppAsset(asset); err != nil {
		utils.JSONErrorResponse(w, "failed to create asset record: "+err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, responses.AssetUploadURLResponse{
		ID:        id.String(),
		UploadURL: presignedRequest.URL,
		PublicURL: publicURL,
	}, http.StatusOK)
}

// POST /v2/apps/{appId}/assets/{assetId}/confirm
func (v *HttpViews) ConfirmAssetUpload(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	assetID := chi.URLParam(r, "assetId")
	asset, err := v.Controller.DB.GetAppAssetByID(assetID)
	if err != nil {
		utils.JSONErrorResponse(w, "asset not found", http.StatusNotFound)
		return
	}

	if asset.AppProjectID.String() != appID {
		utils.JSONErrorResponse(w, "asset does not belong to this app", http.StatusForbidden)
		return
	}

	utils.JSONResponse(w, responses.AssetResponse{
		ID:          asset.ID.String(),
		FileName:    asset.FileName,
		ContentType: asset.ContentType,
		FileSize:    asset.FileSize,
		PublicURL:   asset.PublicURL,
		CreatedAt:   asset.CreatedAt,
	}, http.StatusOK)
}

// PUT /v2/apps/{appId}/assets/{assetId}
func (v *HttpViews) RenameAppAsset(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	assetID := chi.URLParam(r, "assetId")
	asset, err := v.Controller.DB.GetAppAssetByID(assetID)
	if err != nil {
		utils.JSONErrorResponse(w, "asset not found", http.StatusNotFound)
		return
	}

	if asset.AppProjectID.String() != appID {
		utils.JSONErrorResponse(w, "asset does not belong to this app", http.StatusForbidden)
		return
	}

	var req requests.AssetRenameRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	asset.FileName = req.FileName
	asset.UpdatedAt = time.Now()
	if _, err := v.Controller.DB.UpdateAppAsset(asset); err != nil {
		utils.JSONErrorResponse(w, "failed to rename asset: "+err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, responses.AssetResponse{
		ID:          asset.ID.String(),
		FileName:    asset.FileName,
		ContentType: asset.ContentType,
		FileSize:    asset.FileSize,
		PublicURL:   asset.PublicURL,
		CreatedAt:   asset.CreatedAt,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/assets/{assetId}
func (v *HttpViews) DeleteAppAsset(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	assetID := chi.URLParam(r, "assetId")
	asset, err := v.Controller.DB.GetAppAssetByID(assetID)
	if err != nil {
		utils.JSONErrorResponse(w, "asset not found", http.StatusNotFound)
		return
	}

	if asset.AppProjectID.String() != appID {
		utils.JSONErrorResponse(w, "asset does not belong to this app", http.StatusForbidden)
		return
	}

	// Delete from S3
	if err := v.Controller.AssetsStorage.DeleteObjectDirect(r.Context(), asset.S3Key); err != nil {
		utils.JSONErrorResponse(w, "failed to delete file from storage: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Soft delete from DB
	if err := v.Controller.DB.DeleteAppAsset(asset); err != nil {
		utils.JSONErrorResponse(w, "failed to delete asset record: "+err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]bool{"success": true}, http.StatusOK)
}
