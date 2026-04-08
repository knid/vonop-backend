package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// AdminJobListParams holds query parameters for listing generation jobs in the admin panel.
type AdminJobListParams struct {
	Page     int
	PageSize int
	Search   string
	Status   string
	SortDir  string
}

// Offset returns the pagination offset.
func (p AdminJobListParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// AdminListJobs returns a paginated, filterable list of generation jobs.
func (db *Database) AdminListJobs(params AdminJobListParams) ([]models.GenerationJob, int, error) {
	var jobs []models.GenerationJob

	q := db.DB.NewSelect().
		Model(&jobs)

	// Filter by status
	if params.Status != "" {
		q = q.Where("\"generation_job\".\"status\" = ?", params.Status)
	}

	// Search by type
	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.Where("\"generation_job\".\"type\" ILIKE ?", search)
		})
	}

	// Sorting
	q = q.OrderExpr("\"generation_job\".\"created_at\" " + params.SortDir)

	// Pagination
	q = q.Limit(params.PageSize).Offset(params.Offset())

	total, err := q.ScanAndCount(context.Background())
	return jobs, total, err
}

// AdminGetJobByID returns a single generation job by ID.
func (db *Database) AdminGetJobByID(id string) (*models.GenerationJob, error) {
	var job models.GenerationJob
	err := db.DB.NewSelect().
		Model(&job).
		Where("\"generation_job\".\"id\" = ?", id).
		Scan(context.Background())
	return &job, err
}
