package views

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// GET /v2/apps/{appId}/database/schema
func (v *HttpViews) GetDatabaseSchema(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	schema, err := v.Controller.GetDatabaseSchema(appID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, schema, http.StatusOK)
}

// GET /v2/apps/{appId}/database/tables/{table}/rows
func (v *HttpViews) GetDatabaseTableRows(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	tableName := chi.URLParam(r, "table")
	if tableName == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "table"})
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	sortCol := r.URL.Query().Get("sort_column")
	sortDir := r.URL.Query().Get("sort_dir")

	result, err := v.Controller.GetTableRows(appID, tableName, page, pageSize, sortCol, sortDir)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// POST /v2/apps/{appId}/database/tables/{table}/rows
func (v *HttpViews) InsertDatabaseRow(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	tableName := chi.URLParam(r, "table")
	if tableName == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "table"})
		return
	}

	var req requests.InsertRowRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	row, err := v.Controller.InsertTableRow(appID, tableName, req.Data)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, responses.InsertRowResponse{
		Success: true,
		Row:     row,
	}, http.StatusCreated)
}

// PATCH /v2/apps/{appId}/database/tables/{table}/rows
func (v *HttpViews) UpdateDatabaseRow(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	tableName := chi.URLParam(r, "table")
	if tableName == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "table"})
		return
	}

	var req requests.UpdateRowRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	if err := v.Controller.UpdateTableCell(appID, tableName, req.Column, req.Value, req.PrimaryKey); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, responses.CellUpdateResponse{
		Success: true,
		Table:   tableName,
		Column:  req.Column,
		Value:   req.Value,
	}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/database/tables/{table}/rows
func (v *HttpViews) DeleteDatabaseRow(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	tableName := chi.URLParam(r, "table")
	if tableName == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "table"})
		return
	}

	var req requests.DeleteRowRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	if err := v.Controller.DeleteTableRow(appID, tableName, req.PrimaryKey); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, responses.DeleteRowResponse{Success: true}, http.StatusOK)
}

// PUT /v2/apps/{appId}/database/tables/{table}/rows
func (v *HttpViews) BulkUpdateDatabaseRow(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	tableName := chi.URLParam(r, "table")
	if tableName == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "table"})
		return
	}

	var req requests.BulkUpdateRowRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	if err := v.Controller.UpdateTableRow(appID, tableName, req.Data, req.PrimaryKey); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, responses.BulkUpdateRowResponse{Success: true}, http.StatusOK)
}

// POST /v2/apps/{appId}/database/upload-url
func (v *HttpViews) GetDatabaseUploadURL(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	var req requests.DatabaseUploadURLRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	// Validate content type
	allowedTypes := map[string]bool{
		"image/jpeg":    true,
		"image/png":     true,
		"image/webp":    true,
		"image/gif":     true,
		"image/svg+xml": true,
	}
	if !allowedTypes[req.ContentType] {
		utils.JSONError(w, constants.ErrAssetInvalidType, "Unsupported content type. Allowed: jpeg, png, webp, gif, svg.", http.StatusBadRequest, nil)
		return
	}

	ext := filepath.Ext(req.Filename)
	if ext == "" {
		// Fallback extension from content type
		switch req.ContentType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/webp":
			ext = ".webp"
		case "image/gif":
			ext = ".gif"
		case "image/svg+xml":
			ext = ".svg"
		}
	}
	ext = strings.ToLower(ext)

	id := uuid.New().String()
	objectKey := "apps/" + appID + "/db-assets/" + id + ext

	presignedRequest, err := v.Controller.AssetsStorage.PutObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	// Build the public URL (same bucket, GET request).
	// NOTE: This assumes the S3 bucket has public-read ACLs. Without them the URL returns 403.
	publicURL := presignedRequest.URL
	// Strip query params to get the base S3 URL for the object
	if idx := strings.Index(publicURL, "?"); idx != -1 {
		publicURL = publicURL[:idx]
	}

	utils.JSONResponse(w, responses.DatabaseUploadURLResponse{
		UploadURL: presignedRequest.URL,
		PublicURL: publicURL,
	}, http.StatusOK)
}
