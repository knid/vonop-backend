package controllers

import (
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/slack-go/slack"
)

func (c *Controller) CreateLoggedInUserTicket(ticketCreateReq requests.CreateTicketRequest, user *models.User) (*models.Ticket, error) {
	ticket, err := c.DB.CreateTicket(&models.Ticket{
		UserID:      user.ID,
		Subject:     ticketCreateReq.Subject,
		Message:     ticketCreateReq.Message,
		SenderName:  user.Name,
		SenderEmail: user.Email,
		Status:      "open",
	})

	if err != nil {
		return nil, err
	}

	go c.SlackService.Client.PostMessage(
		constants.SLACK_CHANNEL_ID_NEW_TICKETS,
		slack.MsgOptionText("New Ticket: "+ticket.Subject, false))

	return ticket, nil
}

func (c *Controller) CreateTicket(ticketCreateReq requests.CreateTicketRequest) (*models.Ticket, error) {
	ticket, err := c.DB.CreateTicket(&models.Ticket{
		Subject:     ticketCreateReq.Subject,
		Message:     ticketCreateReq.Message,
		SenderName:  ticketCreateReq.SenderName,
		SenderEmail: ticketCreateReq.SenderEmail,
		Status:      "open",
	})

	if err != nil {
		return nil, err
	}

	go c.SlackService.Client.PostMessage(
		constants.SLACK_CHANNEL_ID_NEW_TICKETS,
		slack.MsgOptionText("New Ticket: "+ticket.Subject, false))

	c.EmailService.SendEmail(&models.User{
		Name:  ticket.SenderName,
		Email: ticket.SenderEmail,
	}, constants.BREVO_TICKET_RECEIVED_EMAIL_TEMPLATE_ID, map[string]interface{}{
		"ticket_id": ticket.ID.String(),
		"subject":   ticket.Subject,
		"message":   ticket.Message,
	})

	return ticket, nil
}
