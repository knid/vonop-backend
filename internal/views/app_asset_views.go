package views

import (
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"fmt"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// GET /v2/apps/{appId}/assets
func (v *HttpViews) GetAppAssets(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	assets, err := v.Controller.DB.GetAppAssetsByProjectID(appID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	result := make([]responses.AssetResponse, len(assets))
	for i, a := range assets {
		url := a.PublicURL
		if a.S3Key != "" && v.Controller.AssetsStorage != nil {
			if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), a.S3Key, 3600); err == nil {
				url = presigned.URL
			}
		}
		result[i] = responses.AssetResponse{
			ID:          a.ID.String(),
			FileName:    a.FileName,
			ContentType: a.ContentType,
			FileSize:    a.FileSize,
			PublicURL:   url,
			CreatedAt:   a.CreatedAt,
		}
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// POST /v2/apps/{appId}/assets/upload-url
func (v *HttpViews) RequestAssetUploadURL(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	var req requests.AssetUploadURLRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	// Validate file size
	kind := constants.ClassifyAttachment(req.ContentType)
	maxBytes := constants.MaxAttachmentSize(kind)
	if req.FileSize > maxBytes {
		utils.JSONError(w, constants.ErrAssetTooLarge,
			fmt.Sprintf("File size exceeds %d MB limit for this file type.", constants.MaxAttachmentSizeMB(kind)),
			http.StatusBadRequest, map[string]any{
				"limit_bytes": maxBytes,
				"size_bytes":  req.FileSize,
			})
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
		utils.JSONInternalError(w, err)
		return
	}

	// Build public URL via the proxy endpoint (bucket is private, direct S3 URLs won't work)
	publicURL := fmt.Sprintf("%s/v2/apps/%s/assets/%s/file", constants.WEMOB_API_BASE_URL, appID, id.String())

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
		utils.JSONInternalError(w, err)
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
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	assetID := chi.URLParam(r, "assetId")
	asset, err := v.Controller.DB.GetAppAssetByID(assetID)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAssetNotFound, http.StatusNotFound, nil)
		return
	}

	if asset.AppProjectID.String() != appID {
		utils.JSONError(w, constants.ErrForbidden, "Asset does not belong to this app.", http.StatusForbidden, nil)
		return
	}

	url := asset.PublicURL
	if asset.S3Key != "" && v.Controller.AssetsStorage != nil {
		if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), asset.S3Key, 3600); err == nil {
			url = presigned.URL
		}
	}

	utils.JSONResponse(w, responses.AssetResponse{
		ID:          asset.ID.String(),
		FileName:    asset.FileName,
		ContentType: asset.ContentType,
		FileSize:    asset.FileSize,
		PublicURL:   url,
		CreatedAt:   asset.CreatedAt,
	}, http.StatusOK)
}

// PUT /v2/apps/{appId}/assets/{assetId}
func (v *HttpViews) RenameAppAsset(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	assetID := chi.URLParam(r, "assetId")
	asset, err := v.Controller.DB.GetAppAssetByID(assetID)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAssetNotFound, http.StatusNotFound, nil)
		return
	}

	if asset.AppProjectID.String() != appID {
		utils.JSONError(w, constants.ErrForbidden, "Asset does not belong to this app.", http.StatusForbidden, nil)
		return
	}

	var req requests.AssetRenameRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	asset.FileName = req.FileName
	asset.UpdatedAt = time.Now()
	if _, err := v.Controller.DB.UpdateAppAsset(asset); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	url := asset.PublicURL
	if asset.S3Key != "" && v.Controller.AssetsStorage != nil {
		if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), asset.S3Key, 3600); err == nil {
			url = presigned.URL
		}
	}

	utils.JSONResponse(w, responses.AssetResponse{
		ID:          asset.ID.String(),
		FileName:    asset.FileName,
		ContentType: asset.ContentType,
		FileSize:    asset.FileSize,
		PublicURL:   url,
		CreatedAt:   asset.CreatedAt,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/assets/{assetId}
func (v *HttpViews) DeleteAppAsset(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	assetID := chi.URLParam(r, "assetId")
	asset, err := v.Controller.DB.GetAppAssetByID(assetID)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAssetNotFound, http.StatusNotFound, nil)
		return
	}

	if asset.AppProjectID.String() != appID {
		utils.JSONError(w, constants.ErrForbidden, "Asset does not belong to this app.", http.StatusForbidden, nil)
		return
	}

	// Delete from S3
	if err := v.Controller.AssetsStorage.DeleteObjectDirect(r.Context(), asset.S3Key); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	// Soft delete from DB
	if err := v.Controller.DB.DeleteAppAsset(asset); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]bool{"success": true}, http.StatusOK)
}

// GET /v2/apps/{appId}/assets/{assetId}/file — public endpoint that redirects to a fresh presigned S3 URL.
// No auth required — assets are embedded in generated app code.
func (v *HttpViews) GetAssetFile(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	assetID := chi.URLParam(r, "assetId")
	if appID == "" || assetID == "" {
		http.NotFound(w, r)
		return
	}

	asset, err := v.Controller.DB.GetAppAssetByID(assetID)
	if err != nil || asset.AppProjectID.String() != appID || asset.S3Key == "" {
		http.NotFound(w, r)
		return
	}

	presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), asset.S3Key, 3600)
	if err != nil {
		slog.Error("GetAssetFile: failed to presign", "error", err, "asset_id", assetID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.Redirect(w, r, presigned.URL, http.StatusFound)
}
