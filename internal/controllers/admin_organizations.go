package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// AdminListOrganizations returns a paginated list of organizations with owner info.
func (c *Controller) AdminListOrganizations(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()

	dbParams := database.AdminOrgListParams{
		Page:     params.Page,
		PageSize: params.PageSize,
		Search:   params.Search,
		SortDir:  params.SortDir,
		From:     params.From,
		To:       params.To,
	}

	orgs, total, err := c.DB.AdminListOrganizations(dbParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list organizations: %w", err)
	}

	items := make([]responses.AdminOrgResponse, len(orgs))
	for i, o := range orgs {
		ownerEmail := ""
		if o.Owner != nil {
			ownerEmail = o.Owner.Email
		}
		memberCount, _ := c.DB.AdminCountOrgMembers(o.ID.String())
		items[i] = responses.AdminOrgResponse{
			ID:          o.ID,
			Name:        o.Name,
			Slug:        o.Slug,
			OwnerEmail:  ownerEmail,
			MemberCount: memberCount,
			CreatedAt:   o.CreatedAt,
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

// AdminGetOrganization returns detailed info for a single organization including its members.
func (c *Controller) AdminGetOrganization(orgID string) (*responses.AdminOrgDetailResponse, error) {
	org, err := c.DB.AdminGetOrganizationByID(orgID)
	if err != nil {
		return nil, fmt.Errorf("organization not found: %w", err)
	}

	ownerEmail := ""
	if org.Owner != nil {
		ownerEmail = org.Owner.Email
	}

	memberCount, _ := c.DB.AdminCountOrgMembers(org.ID.String())

	members, err := c.DB.AdminGetOrgMembers(org.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get org members: %w", err)
	}

	memberResponses := make([]responses.AdminOrgMemberResponse, len(members))
	for i, m := range members {
		name := ""
		email := ""
		if m.User != nil {
			name = m.User.Name
			email = m.User.Email
		}
		memberResponses[i] = responses.AdminOrgMemberResponse{
			ID:    m.UserID,
			Name:  name,
			Email: email,
			Role:  m.Role,
		}
	}

	return &responses.AdminOrgDetailResponse{
		AdminOrgResponse: responses.AdminOrgResponse{
			ID:          org.ID,
			Name:        org.Name,
			Slug:        org.Slug,
			OwnerEmail:  ownerEmail,
			MemberCount: memberCount,
			CreatedAt:   org.CreatedAt,
		},
		Members: memberResponses,
	}, nil
}
