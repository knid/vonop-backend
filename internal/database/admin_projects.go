package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// AdminProjectListParams holds query parameters for listing projects in the admin panel.
type AdminProjectListParams struct {
	Page     int
	PageSize int
	Search   string
	SortDir  string
	From     string
	To       string
}

// Offset returns the pagination offset.
func (p AdminProjectListParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// AdminListProjects returns a paginated, searchable list of legacy projects with their owners.
func (db *Database) AdminListProjects(params AdminProjectListParams) ([]models.Project, int, error) {
	var projects []models.Project

	q := db.DB.NewSelect().
		Model(&projects).
		Relation("User")

	// Search by package name
	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.
				Where("\"project\".\"package_name\" ILIKE ?", search).
				WhereOr("\"project\".\"url\" ILIKE ?", search)
		})
	}

	// Date range filters
	if params.From != "" {
		q = q.Where("\"project\".\"created_at\" >= ?", params.From)
	}
	if params.To != "" {
		q = q.Where("\"project\".\"created_at\" <= ?", params.To)
	}

	// Sorting
	q = q.OrderExpr("\"project\".\"created_at\" " + params.SortDir)

	// Pagination
	q = q.Limit(params.PageSize).Offset(params.Offset())

	total, err := q.ScanAndCount(context.Background())
	return projects, total, err
}

// AdminGetProjectByID returns a single project with its owner relation.
func (db *Database) AdminGetProjectByID(id string) (*models.Project, error) {
	var project models.Project
	err := db.DB.NewSelect().
		Model(&project).
		Relation("User").
		Where("\"project\".\"id\" = ?", id).
		Scan(context.Background())
	return &project, err
}
