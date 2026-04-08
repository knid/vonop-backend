package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// AdminOrgListParams holds query parameters for listing organizations in the admin panel.
type AdminOrgListParams struct {
	Page     int
	PageSize int
	Search   string
	SortDir  string
	From     string
	To       string
}

// Offset returns the pagination offset.
func (p AdminOrgListParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// AdminListOrganizations returns a paginated, searchable list of organizations with their owners.
func (db *Database) AdminListOrganizations(params AdminOrgListParams) ([]models.Organization, int, error) {
	var orgs []models.Organization

	q := db.DB.NewSelect().
		Model(&orgs).
		Relation("Owner")

	// Search by name or slug
	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.
				Where("\"organization\".\"name\" ILIKE ?", search).
				WhereOr("\"organization\".\"slug\" ILIKE ?", search)
		})
	}

	// Date range filters
	if params.From != "" {
		q = q.Where("\"organization\".\"created_at\" >= ?", params.From)
	}
	if params.To != "" {
		q = q.Where("\"organization\".\"created_at\" <= ?", params.To)
	}

	// Sorting
	q = q.OrderExpr("\"organization\".\"created_at\" " + params.SortDir)

	// Pagination
	q = q.Limit(params.PageSize).Offset(params.Offset())

	total, err := q.ScanAndCount(context.Background())
	return orgs, total, err
}

// AdminGetOrganizationByID returns a single organization with its owner relation.
func (db *Database) AdminGetOrganizationByID(id string) (*models.Organization, error) {
	var org models.Organization
	err := db.DB.NewSelect().
		Model(&org).
		Relation("Owner").
		Where("\"organization\".\"id\" = ?", id).
		Scan(context.Background())
	return &org, err
}

// AdminCountOrgMembers returns the number of members in an organization.
func (db *Database) AdminCountOrgMembers(orgID string) (int, error) {
	count, err := db.DB.NewSelect().
		Model((*models.OrganizationMember)(nil)).
		Where("organization_id = ?", orgID).
		Count(context.Background())
	return count, err
}

// AdminGetOrgMembers returns all members of an organization with their user details.
func (db *Database) AdminGetOrgMembers(orgID string) ([]models.OrganizationMember, error) {
	var members []models.OrganizationMember
	err := db.DB.NewSelect().
		Model(&members).
		Relation("User").
		Where("\"organization_member\".\"organization_id\" = ?", orgID).
		Order("organization_member.created_at ASC").
		Scan(context.Background())
	return members, err
}
