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
			DNSRecord: d.DNSRecord,
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

	dnsRecord := "cname.vercel-dns.com"

	domain := models.AppDomain{
		AppProjectID: project.ID,
		Domain:       domainName,
		Status:       "pending",
		DNSRecord:    dnsRecord,
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
		DNSRecord: created.DNSRecord,
		CreatedAt: created.CreatedAt,
	}, nil
}

// RemoveAppDomain removes a custom domain from an app project.
func (c *Controller) RemoveAppDomain(domainID string, project *models.AppProject) error {
	domain, err := c.DB.GetAppDomainByID(domainID)
	if err != nil {
		return errors.New("domain not found")
	}

	if domain.AppProjectID != project.ID {
		return errors.New("domain does not belong to this project")
	}

	return c.DB.DeleteAppDomain(domain)
}
