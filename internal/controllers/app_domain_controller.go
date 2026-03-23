package controllers

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// GetAppDomains returns all domains for an app project.
func (c *Controller) GetAppDomains(projectID string) ([]responses.AppDomainResponse, error) {
	domains, err := c.DB.GetAppDomainsByProjectID(projectID)
	if err != nil {
		return nil, err
	}

	result := make([]responses.AppDomainResponse, len(domains))
	for i, d := range domains {
		result[i] = responses.AppDomainResponse{
			ID:        d.ID,
			Domain:    d.Domain,
			Status:    d.Status,
			DNSRecord: constants.WEMOB_CNAME_TARGET,
			CreatedAt: d.CreatedAt,
		}
	}
	return result, nil
}

// AddAppDomain adds a custom domain to an app project.
// If a Coolify project exists, the domain is also registered there.
func (c *Controller) AddAppDomain(project *models.AppProject, user *models.User, req requests.AddAppDomainRequest) (*responses.AppDomainResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.AddAppDomainRequest](req, validate); err != nil {
		return nil, err
	}

	domainName := strings.ToLower(req.Domain)

	// Block manual additions of *.wemob.app subdomains — these are managed automatically
	if strings.HasSuffix(domainName, "."+constants.WEMOB_APP_BASE_DOMAIN) {
		return nil, errors.New("wemob.app subdomains are managed automatically")
	}

	// Check domain is not already in use
	existing, err := c.DB.GetAppDomainByName(domainName)
	if err == nil && existing.ID.String() != "" {
		return nil, errors.New("domain is already in use")
	}

	domain := models.AppDomain{
		AppProjectID: project.ID,
		Domain:       domainName,
		Status:       "pending",
	}

	created, err := c.DB.CreateAppDomain(&domain)
	if err != nil {
		return nil, fmt.Errorf("failed to add domain: %v", err)
	}

	// If there is a Coolify project, add the domain there too
	coolifyProject, cpErr := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
	if cpErr == nil && coolifyProject != nil && c.CoolifyService != nil {
		domains := c.buildCoolifyDomainList(coolifyProject, project.ID.String(), domainName)
		if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, domains); err != nil {
			slog.Warn("Failed to add domain to Coolify", "domain", domainName, "error", err)
		} else {
			created.Status = "active"
			c.DB.UpdateAppDomain(created)
		}
	}

	return &responses.AppDomainResponse{
		ID:        created.ID,
		Domain:    created.Domain,
		Status:    created.Status,
		DNSRecord: constants.WEMOB_CNAME_TARGET,
		CreatedAt: created.CreatedAt,
	}, nil
}

// RemoveAppDomain removes a custom domain from an app project and Coolify.
func (c *Controller) RemoveAppDomain(domainID string, project *models.AppProject) error {
	domain, err := c.DB.GetAppDomainByID(domainID)
	if err != nil {
		return errors.New("domain not found")
	}

	if domain.AppProjectID != project.ID {
		return errors.New("domain does not belong to this project")
	}

	// Also remove from Coolify if project exists there
	if c.CoolifyService != nil {
		coolifyProject, cpErr := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
		if cpErr == nil && coolifyProject != nil {
			domains := c.buildCoolifyDomainList(coolifyProject, project.ID.String(), "")
			// Filter out the domain being removed
			filtered := make([]string, 0, len(domains))
			removeDomain := "https://" + domain.Domain
			for _, d := range domains {
				if d != removeDomain {
					filtered = append(filtered, d)
				}
			}
			if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, filtered); err != nil {
				slog.Warn("Failed to remove domain from Coolify", "domain", domain.Domain, "error", err)
			}
		}
	}

	return c.DB.DeleteAppDomain(domain)
}

// VerifyAppDomain checks the DNS configuration of a custom domain via Go net lookups
// and updates the domain status accordingly.
func (c *Controller) VerifyAppDomain(domainID string, project *models.AppProject) (*responses.AppDomainResponse, error) {
	domain, err := c.DB.GetAppDomainByID(domainID)
	if err != nil {
		return nil, errors.New("domain not found")
	}

	if domain.AppProjectID != project.ID {
		return nil, errors.New("domain does not belong to this project")
	}

	// Check DNS via Go net package instead of Vercel API
	cname, err := net.LookupCNAME(domain.Domain)
	if err == nil && strings.Contains(cname, constants.WEMOB_CNAME_TARGET) {
		domain.Status = "active"
	} else {
		// Try A record lookup as fallback
		ips, err := net.LookupHost(domain.Domain)
		if err == nil && len(ips) > 0 {
			domain.Status = "active"
		}
	}

	c.DB.UpdateAppDomain(domain)

	// If domain is active and Coolify project exists, sync all domains
	if domain.Status == "active" && c.CoolifyService != nil {
		coolifyProject, cpErr := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
		if cpErr == nil && coolifyProject != nil {
			domains := c.buildCoolifyDomainList(coolifyProject, project.ID.String(), "")
			if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, domains); err != nil {
				slog.Warn("Failed to add verified domain to Coolify", "domain", domain.Domain, "error", err)
			}
		}
	}

	return &responses.AppDomainResponse{
		ID:        domain.ID,
		Domain:    domain.Domain,
		Status:    domain.Status,
		DNSRecord: constants.WEMOB_CNAME_TARGET,
		CreatedAt: domain.CreatedAt,
	}, nil
}

// buildCoolifyDomainList builds the complete list of domains for a Coolify app.
// It always includes the default wemob.app subdomain, plus all active custom domains
// from the database, plus an optional extra domain being added.
func (c *Controller) buildCoolifyDomainList(coolifyProject *models.CoolifyProject, projectID string, extraDomain string) []string {
	domains := []string{"https://" + coolifyProject.CustomDomain}

	// Add all existing custom domains from DB
	appDomains, err := c.DB.GetAppDomainsByProjectID(projectID)
	if err == nil {
		for _, d := range appDomains {
			domains = append(domains, "https://"+d.Domain)
		}
	}

	// Add the extra domain if provided and not already present
	if extraDomain != "" {
		extra := "https://" + extraDomain
		found := false
		for _, d := range domains {
			if d == extra {
				found = true
				break
			}
		}
		if !found {
			domains = append(domains, extra)
		}
	}

	return domains
}
