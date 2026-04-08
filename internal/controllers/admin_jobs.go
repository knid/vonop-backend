package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// AdminListJobs returns a paginated list of generation jobs.
func (c *Controller) AdminListJobs(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()

	dbParams := database.AdminJobListParams{
		Page:     params.Page,
		PageSize: params.PageSize,
		Search:   params.Search,
		Status:   params.Status,
		SortDir:  params.SortDir,
	}

	jobs, total, err := c.DB.AdminListJobs(dbParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	items := make([]responses.AdminJobResponse, len(jobs))
	for i, j := range jobs {
		items[i] = responses.AdminJobResponse{
			ID:        j.ID,
			UserID:    j.UserID,
			UserEmail: "", // GenerationJob has no User relation
			Type:      j.Type,
			Status:    j.Status,
			Error:     j.Error,
			CreatedAt: j.CreatedAt,
			UpdatedAt: j.UpdatedAt,
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

// AdminGetJob returns detailed info for a single generation job including events and result.
func (c *Controller) AdminGetJob(jobID string) (*responses.AdminJobDetailResponse, error) {
	job, err := c.DB.AdminGetJobByID(jobID)
	if err != nil {
		return nil, fmt.Errorf("job not found: %w", err)
	}

	return &responses.AdminJobDetailResponse{
		AdminJobResponse: responses.AdminJobResponse{
			ID:        job.ID,
			UserID:    job.UserID,
			UserEmail: "", // GenerationJob has no User relation
			Type:      job.Type,
			Status:    job.Status,
			Error:     job.Error,
			CreatedAt: job.CreatedAt,
			UpdatedAt: job.UpdatedAt,
		},
		Events: job.Events,
		Result: job.Result,
	}, nil
}
