package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// AdminListTickets handles GET /v1/admin/tickets
func (v *HttpViews) AdminListTickets(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorResponse(w, "admin access required", http.StatusForbidden)
		return
	}

	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListTickets(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminGetTicket handles GET /v1/admin/tickets/{id}
func (v *HttpViews) AdminGetTicket(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorResponse(w, "admin access required", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "ticketId")
	result, err := v.Controller.AdminGetTicket(id)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

// AdminUpdateTicket handles PATCH /v1/admin/tickets/{id}
func (v *HttpViews) AdminUpdateTicket(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user.Type != "admin" {
		utils.JSONErrorResponse(w, "admin access required", http.StatusForbidden)
		return
	}

	id := chi.URLParam(r, "ticketId")

	var req requests.AdminUpdateTicketRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.AdminUpdateTicket(id, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
