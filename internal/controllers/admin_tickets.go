package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// AdminListTickets returns a paginated list of tickets with user info.
func (c *Controller) AdminListTickets(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()

	dbParams := database.AdminTicketListParams{
		Page:     params.Page,
		PageSize: params.PageSize,
		Search:   params.Search,
		Status:   params.Status,
		SortDir:  params.SortDir,
	}

	tickets, total, err := c.DB.AdminListTickets(dbParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list tickets: %w", err)
	}

	items := make([]responses.AdminTicketResponse, len(tickets))
	for i, t := range tickets {
		items[i] = responses.AdminTicketResponse{
			ID:          t.ID,
			UserID:      t.UserID,
			UserEmail:   t.User.Email,
			Subject:     t.Subject,
			Message:     t.Message,
			Status:      t.Status,
			Priority:    t.Priority,
			Response:    t.Response,
			SenderName:  t.SenderName,
			SenderEmail: t.SenderEmail,
			CreatedAt:   t.CreatedAt,
			UpdatedAt:   t.UpdatedAt,
		}
	}

	totalPages := total / params.PageSize
	if total%params.PageSize != 0 {
		totalPages++
	}

	return &responses.PaginatedResponse{
		Data:       items,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
	}, nil
}

// AdminGetTicket returns detailed info for a single ticket.
func (c *Controller) AdminGetTicket(ticketID string) (*responses.AdminTicketResponse, error) {
	ticket, err := c.DB.AdminGetTicketByID(ticketID)
	if err != nil {
		return nil, fmt.Errorf("ticket not found: %w", err)
	}

	return &responses.AdminTicketResponse{
		ID:          ticket.ID,
		UserID:      ticket.UserID,
		UserEmail:   ticket.User.Email,
		Subject:     ticket.Subject,
		Message:     ticket.Message,
		Status:      ticket.Status,
		Priority:    ticket.Priority,
		Response:    ticket.Response,
		SenderName:  ticket.SenderName,
		SenderEmail: ticket.SenderEmail,
		CreatedAt:   ticket.CreatedAt,
		UpdatedAt:   ticket.UpdatedAt,
	}, nil
}

// AdminUpdateTicket updates a ticket's status, response, or priority.
func (c *Controller) AdminUpdateTicket(ticketID string, req requests.AdminUpdateTicketRequest) (*responses.AdminTicketResponse, error) {
	update := database.AdminTicketUpdate{
		Status:   req.Status,
		Response: req.Response,
		Priority: req.Priority,
	}

	ticket, err := c.DB.AdminUpdateTicket(ticketID, update)
	if err != nil {
		return nil, fmt.Errorf("failed to update ticket: %w", err)
	}

	return &responses.AdminTicketResponse{
		ID:          ticket.ID,
		UserID:      ticket.UserID,
		UserEmail:   ticket.User.Email,
		Subject:     ticket.Subject,
		Message:     ticket.Message,
		Status:      ticket.Status,
		Priority:    ticket.Priority,
		Response:    ticket.Response,
		SenderName:  ticket.SenderName,
		SenderEmail: ticket.SenderEmail,
		CreatedAt:   ticket.CreatedAt,
		UpdatedAt:   ticket.UpdatedAt,
	}, nil
}
