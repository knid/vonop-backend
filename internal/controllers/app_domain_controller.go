package controllers

import (
	"errors"
	"fmt"
	"log/slog"
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
// If a Vercel project exists, the domain is also added there.
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

	// If there is a Vercel project, add the domain there too
	vercelProject, vercelErr := c.DB.GetVercelProjectByAppProjectID(project.ID.String())
	if vercelErr == nil && vercelProject != nil && c.VercelService != nil {
		if err := c.VercelService.AddDomain(vercelProject.VercelProjectID, domainName); err != nil {
			slog.Warn("Failed to add domain to Vercel", "domain", domainName, "error", err)
			// Domain was still saved in our DB; Vercel addition is best-effort
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

// RemoveAppDomain removes a custom domain from an app project and Vercel.
func (c *Controller) RemoveAppDomain(domainID string, project *models.AppProject) error {
	domain, err := c.DB.GetAppDomainByID(domainID)
	if err != nil {
		return errors.New("domain not found")
	}

	if domain.AppProjectID != project.ID {
		return errors.New("domain does not belong to this project")
	}

	// Also remove from Vercel if project exists there
	if c.VercelService != nil {
		vercelProject, vercelErr := c.DB.GetVercelProjectByAppProjectID(project.ID.String())
		if vercelErr == nil && vercelProject != nil {
			if err := c.VercelService.RemoveDomain(vercelProject.VercelProjectID, domain.Domain); err != nil {
				slog.Warn("Failed to remove domain from Vercel", "domain", domain.Domain, "error", err)
			}
		}
	}

	return c.DB.DeleteAppDomain(domain)
}

// VerifyAppDomain checks the DNS configuration of a custom domain via Vercel's API
// and updates the domain status accordingly.
func (c *Controller) VerifyAppDomain(domainID string, project *models.AppProject) (*responses.AppDomainResponse, error) {
	domain, err := c.DB.GetAppDomainByID(domainID)
	if err != nil {
		return nil, errors.New("domain not found")
	}

	if domain.AppProjectID != project.ID {
		return nil, errors.New("domain does not belong to this project")
	}

	if c.VercelService == nil {
		return nil, errors.New("vercel service not configured")
	}

	// Check DNS configuration via Vercel
	cfg, err := c.VercelService.GetDomainConfig(domain.Domain)
	if err != nil {
		slog.Warn("Failed to check domain config", "domain", domain.Domain, "error", err)
		return &responses.AppDomainResponse{
			ID:        domain.ID,
			Domain:    domain.Domain,
			Status:    domain.Status,
			DNSRecord: constants.WEMOB_CNAME_TARGET,
			CreatedAt: domain.CreatedAt,
		}, nil
	}

	// Update status based on Vercel's DNS check
	if cfg.ConfiguredBy != nil && !cfg.Misconfigured {
		domain.Status = "active"
	} else if cfg.Misconfigured {
		domain.Status = "misconfigured"
	}
	// If ConfiguredBy is nil, DNS isn't set up yet — keep "pending"

	c.DB.UpdateAppDomain(domain)

	// If domain just became active and Vercel project exists, ensure domain is added
	if domain.Status == "active" {
		vercelProject, vercelErr := c.DB.GetVercelProjectByAppProjectID(project.ID.String())
		if vercelErr == nil && vercelProject != nil {
			if err := c.VercelService.AddDomain(vercelProject.VercelProjectID, domain.Domain); err != nil {
				slog.Warn("Failed to add verified domain to Vercel", "domain", domain.Domain, "error", err)
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
