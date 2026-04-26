package views

import (
	"encoding/json"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

func (v *HttpViews) CreateTicket(w http.ResponseWriter, r *http.Request) {
	var ticketCreateReq requests.CreateTicketRequest
	err := json.NewDecoder(r.Body).Decode(&ticketCreateReq)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrValidationBody, http.StatusBadRequest, nil)
		return
	}

	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		ticket, err := v.Controller.CreateTicket(ticketCreateReq)
		if err != nil {
			utils.JSONInternalError(w, err)
			return
		}

		user := &models.User{
			Name:  ticketCreateReq.SenderName,
			Email: ticketCreateReq.SenderEmail,
		}

		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_NEW_TICKET, map[string]any{
			"ticket_subject": ticketCreateReq.Subject,
			"ticket_message": ticketCreateReq.Message,
			"ticket_id":      ticket.ID.String(),
		})

		utils.JSONResponse(w, map[string]string{}, http.StatusCreated)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	ticket, err := v.Controller.CreateLoggedInUserTicket(ticketCreateReq, user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_NEW_TICKET, map[string]any{
		"ticket_subject": ticketCreateReq.Subject,
		"ticket_message": ticketCreateReq.Message,
		"ticket_id":      ticket.ID.String(),
	})

	v.Controller.TrackUserEvent(user, "support_ticket_created", posthog.NewProperties().
		Set("ticket_id", ticket.ID.String()).
		Set("subject", ticketCreateReq.Subject))

	utils.JSONResponse(w, map[string]string{}, http.StatusCreated)

}
