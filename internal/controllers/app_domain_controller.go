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
		newDomain := "https://" + domainName
		existingDomain := "https://" + coolifyProject.CustomDomain
		if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, []string{existingDomain, newDomain}); err != nil {
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
			defaultDomain := "https://" + coolifyProject.CustomDomain
			if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, []string{defaultDomain}); err != nil {
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

	// If domain just became active and Coolify project exists, add domain
	if domain.Status == "active" && c.CoolifyService != nil {
		coolifyProject, cpErr := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
		if cpErr == nil && coolifyProject != nil {
			newDomain := "https://" + domain.Domain
			existingDomain := "https://" + coolifyProject.CustomDomain
			if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, []string{existingDomain, newDomain}); err != nil {
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
