package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// AdminTicketListParams holds query parameters for listing tickets in the admin panel.
type AdminTicketListParams struct {
	Page     int
	PageSize int
	Search   string
	Status   string
	SortDir  string
}

// Offset returns the pagination offset.
func (p AdminTicketListParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// AdminListTickets returns a paginated, filterable list of tickets with user relations.
func (db *Database) AdminListTickets(params AdminTicketListParams) ([]models.Ticket, int, error) {
	var tickets []models.Ticket

	q := db.DB.NewSelect().
		Model(&tickets).
		Relation("User")

	// Filter by status
	if params.Status != "" {
		q = q.Where("\"ticket\".\"status\" = ?", params.Status)
	}

	// Search by subject
	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.Where("\"ticket\".\"subject\" ILIKE ?", search)
		})
	}

	// Sorting
	q = q.OrderExpr("\"ticket\".\"created_at\" " + params.SortDir)

	// Pagination
	q = q.Limit(params.PageSize).Offset(params.Offset())

	total, err := q.ScanAndCount(context.Background())
	return tickets, total, err
}

// AdminGetTicketByID returns a single ticket with its user relation.
func (db *Database) AdminGetTicketByID(id string) (*models.Ticket, error) {
	var ticket models.Ticket
	err := db.DB.NewSelect().
		Model(&ticket).
		Relation("User").
		Where("\"ticket\".\"id\" = ?", id).
		Scan(context.Background())
	return &ticket, err
}

// AdminTicketUpdate holds the fields that can be updated on a ticket.
type AdminTicketUpdate struct {
	Status   *string
	Response *string
	Priority *string
}

// AdminUpdateTicket updates the given ticket's fields and returns the updated ticket.
func (db *Database) AdminUpdateTicket(id string, update AdminTicketUpdate) (*models.Ticket, error) {
	ticket, err := db.AdminGetTicketByID(id)
	if err != nil {
		return nil, err
	}

	if update.Status != nil {
		ticket.Status = *update.Status
	}
	if update.Response != nil {
		ticket.Response = *update.Response
	}
	if update.Priority != nil {
		ticket.Priority = *update.Priority
	}

	ticket.UpdatedAt = time.Now()

	_, err = db.DB.NewUpdate().
		Model(ticket).
		WherePK().
		Exec(context.Background())
	if err != nil {
		return nil, err
	}

	// Re-fetch with relations
	return db.AdminGetTicketByID(id)
}
