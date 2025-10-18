package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateTicket(ticket *models.Ticket) (*models.Ticket, error) {
	_, err := db.DB.NewInsert().Model(ticket).Exec(context.Background())

	return ticket, err
}

func (db *Database) GetTickets(user *models.User) ([]models.Ticket, error) {
	var tickets []models.Ticket
	err := db.DB.NewSelect().Model(&tickets).Where("user_id = ?", user.ID.String()).Order("created_at DESC").Scan(context.Background())

	return tickets, err
}

func (db *Database) GetTicketById(id string) (*models.Ticket, error) {
	var ticket models.Ticket
	err := db.DB.NewSelect().Model(&ticket).Where("id = ?", id).Scan(context.Background())

	return &ticket, err
}

func (db *Database) UpdateTicket(ticket *models.Ticket) (*models.Ticket, error) {
	_, err := db.DB.NewUpdate().Model(ticket).Where("id = ?", ticket.ID).Exec(context.Background())

	return ticket, err
}

func (db *Database) DeleteTicket(ticket *models.Ticket) error {
	_, err := db.DB.NewDelete().Model(ticket).Where("id = ?", ticket.ID).Exec(context.Background())

	return err
}
