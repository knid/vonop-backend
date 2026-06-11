package controllers

import (
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// ResolveOrgForProjectID returns the OrganizationID owning the given project.
// Falls back to the user's personal org if the project lookup fails or has no
// org set.
func (c *Controller) ResolveOrgForProjectID(user *models.User, projectID string) (uuid.UUID, error) {
	if projectID != "" {
		if project, err := c.DB.GetAppProjectByID(projectID); err == nil && project != nil && project.OrganizationID != uuid.Nil {
			return project.OrganizationID, nil
		}
	}
	return c.RequireOrgIDForUser(user, nil)
}
