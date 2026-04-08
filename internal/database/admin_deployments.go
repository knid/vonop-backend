package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

// AdminDeploymentListParams holds query parameters for listing deployments in the admin panel.
type AdminDeploymentListParams struct {
	Page     int
	PageSize int
	Search   string
	Status   string
	SortDir  string
}

// Offset returns the pagination offset.
func (p AdminDeploymentListParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// AdminListDeployments returns a paginated, filterable list of deployments with app and user relations.
func (db *Database) AdminListDeployments(params AdminDeploymentListParams) ([]models.AppDeployment, int, error) {
	var deployments []models.AppDeployment

	q := db.DB.NewSelect().
		Model(&deployments).
		Relation("AppProject").
		Relation("User")

	// Filter by status
	if params.Status != "" {
		q = q.Where("\"app_deployment\".\"status\" = ?", params.Status)
	}

	// Sorting
	q = q.OrderExpr("\"app_deployment\".\"created_at\" " + params.SortDir)

	// Pagination
	q = q.Limit(params.PageSize).Offset(params.Offset())

	total, err := q.ScanAndCount(context.Background())
	return deployments, total, err
}
