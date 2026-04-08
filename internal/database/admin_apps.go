package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// AdminAppListParams holds query parameters for listing app projects in the admin panel.
type AdminAppListParams struct {
	Page     int
	PageSize int
	Search   string
	Status   string
	SortDir  string
	From     string
	To       string
}

// Offset returns the pagination offset.
func (p AdminAppListParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// AdminListApps returns a paginated, searchable, filterable list of app projects with their owners.
func (db *Database) AdminListApps(params AdminAppListParams) ([]models.AppProject, int, error) {
	var apps []models.AppProject

	q := db.DB.NewSelect().
		Model(&apps).
		Relation("User")

	// Search by name or slug
	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.
				Where("\"app_project\".\"name\" ILIKE ?", search).
				WhereOr("\"app_project\".\"slug\" ILIKE ?", search)
		})
	}

	// Filter by status
	if params.Status != "" {
		q = q.Where("\"app_project\".\"status\" = ?", params.Status)
	}

	// Date range filters
	if params.From != "" {
		q = q.Where("\"app_project\".\"created_at\" >= ?", params.From)
	}
	if params.To != "" {
		q = q.Where("\"app_project\".\"created_at\" <= ?", params.To)
	}

	// Sorting
	q = q.OrderExpr("\"app_project\".\"created_at\" " + params.SortDir)

	// Pagination
	q = q.Limit(params.PageSize).Offset(params.Offset())

	total, err := q.ScanAndCount(context.Background())
	return apps, total, err
}

// AdminGetAppByID returns a single app project with its owner relation.
func (db *Database) AdminGetAppByID(id string) (*models.AppProject, error) {
	var app models.AppProject
	err := db.DB.NewSelect().
		Model(&app).
		Relation("User").
		Where("\"app_project\".\"id\" = ?", id).
		Scan(context.Background())
	return &app, err
}
