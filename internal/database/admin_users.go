package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// AdminUserListParams holds the query parameters for listing users in the admin panel.
type AdminUserListParams struct {
	Page     int
	PageSize int
	Search   string
	Status   string
	SortBy   string
	SortDir  string
	From     string
	To       string
}

// Offset returns the pagination offset.
func (p AdminUserListParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// AdminListUsers returns a paginated, searchable, filterable list of users with their subscriptions.
func (db *Database) AdminListUsers(params AdminUserListParams) ([]models.User, int, error) {
	var users []models.User

	q := db.DB.NewSelect().
		Model(&users).
		Relation("Subscription")

	// Search by name or email
	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.
				Where("\"user\".\"name\" ILIKE ?", search).
				WhereOr("\"user\".\"email\" ILIKE ?", search)
		})
	}

	// Filter by subscription status
	if params.Status != "" {
		q = q.Where("\"user\".\"subscription_status\" = ?", params.Status)
	}

	// Date range filters
	if params.From != "" {
		q = q.Where("\"user\".\"created_at\" >= ?", params.From)
	}
	if params.To != "" {
		q = q.Where("\"user\".\"created_at\" <= ?", params.To)
	}

	// Sorting
	sortCol := "created_at"
	allowedSorts := map[string]bool{
		"created_at": true,
		"name":       true,
		"email":      true,
		"type":       true,
	}
	if params.SortBy != "" && allowedSorts[params.SortBy] {
		sortCol = params.SortBy
	}
	q = q.OrderExpr("\"user\".\"" + sortCol + "\" " + params.SortDir)

	// Pagination
	q = q.Limit(params.PageSize).Offset(params.Offset())

	total, err := q.ScanAndCount(context.Background())
	return users, total, err
}

// AdminGetUserByID returns a single user with their subscription relation.
func (db *Database) AdminGetUserByID(id string) (*models.User, error) {
	var user models.User
	err := db.DB.NewSelect().
		Model(&user).
		Relation("Subscription").
		Where("\"user\".\"id\" = ?", id).
		Scan(context.Background())
	return &user, err
}
