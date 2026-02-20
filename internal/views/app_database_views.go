package views

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// GET /v2/apps/{appId}/database/schema
func (v *HttpViews) GetDatabaseSchema(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	schema, err := v.Controller.GetDatabaseSchema(appID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, schema, http.StatusOK)
}

// GET /v2/apps/{appId}/database/tables/{table}/rows
func (v *HttpViews) GetDatabaseTableRows(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	tableName := chi.URLParam(r, "table")
	if tableName == "" {
		utils.JSONErrorResponse(w, "table name is required", http.StatusBadRequest)
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// POST /v2/apps/{appId}/database/tables/{table}/rows
func (v *HttpViews) InsertDatabaseRow(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	tableName := chi.URLParam(r, "table")
	if tableName == "" {
		utils.JSONErrorResponse(w, "table name is required", http.StatusBadRequest)
		return
	}

	var req requests.InsertRowRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	row, err := v.Controller.InsertTableRow(appID, tableName, req.Data)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	tableName := chi.URLParam(r, "table")
	if tableName == "" {
		utils.JSONErrorResponse(w, "table name is required", http.StatusBadRequest)
		return
	}

	var req requests.UpdateRowRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := v.Controller.UpdateTableCell(appID, tableName, req.Column, req.Value, req.PrimaryKey); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	tableName := chi.URLParam(r, "table")
	if tableName == "" {
		utils.JSONErrorResponse(w, "table name is required", http.StatusBadRequest)
		return
	}

	var req requests.DeleteRowRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := v.Controller.DeleteTableRow(appID, tableName, req.PrimaryKey); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, responses.DeleteRowResponse{Success: true}, http.StatusOK)
}
