package controllers

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// domainRegex validates domain format: labels separated by dots, TLD at least 2 chars.
var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*\.[a-zA-Z]{2,}$`)

// isApexDomain returns true if the domain is a root/apex domain (e.g. "example.com")
// as opposed to a subdomain (e.g. "www.example.com"). Apex domains cannot use CNAME records.
func isApexDomain(domain string) bool {
	parts := strings.Split(domain, ".")
	return len(parts) == 2
}

// resolveTargetIPs resolves the IPs that our CNAME target points to.
// Used to verify A/AAAA records for apex domains and Cloudflare-proxied domains.
func resolveTargetIPs() map[string]bool {
	ips := make(map[string]bool)
	addrs, err := net.LookupHost(constants.WEMOB_CNAME_TARGET)
	if err != nil {
		slog.Warn("Failed to resolve CNAME target IPs", "target", constants.WEMOB_CNAME_TARGET, "error", err)
		return ips
	}
	for _, addr := range addrs {
		ips[addr] = true
	}
	return ips
}

// verifyDomainDNS checks DNS configuration and returns the new status + message.
// Supports CNAME verification for subdomains and A/AAAA verification for apex domains
// and Cloudflare-proxied domains.
func verifyDomainDNS(domainName string) (status string, message string) {
	// Step 1: Try CNAME lookup (works for subdomains with direct CNAME)
	cname, cnameErr := net.LookupCNAME(domainName)
	if cnameErr == nil {
		canonical := strings.TrimSuffix(cname, ".")
		if canonical == constants.WEMOB_CNAME_TARGET {
			return "active", "DNS verified — CNAME points to " + constants.WEMOB_CNAME_TARGET
		}
		// CNAME exists but points somewhere else — could be Cloudflare proxy
		// Fall through to A record check
	}

	// Step 2: Resolve the domain's A/AAAA records and compare against our server IPs.
	// This handles: apex domains (A records), Cloudflare proxy mode, other CDN proxies.
	domainIPs, ipErr := net.LookupHost(domainName)
	if ipErr != nil {
		// No DNS records at all
		return "pending", "No DNS records found. Add the required DNS record at your domain provider."
	}

	targetIPs := resolveTargetIPs()
	if len(targetIPs) == 0 {
		// Can't resolve our own target — can't verify, keep pending
		return "pending", "Verification temporarily unavailable. Please try again."
	}

	// Check if any of the domain's IPs match our server
	for _, ip := range domainIPs {
		if targetIPs[ip] {
			return "active", "DNS verified — domain resolves to the correct server"
		}
	}

	// DNS records exist but point to the wrong place
	return "misconfigured", fmt.Sprintf("DNS records found but pointing to wrong address. Expected CNAME to %s or A record to our server IP.", constants.WEMOB_CNAME_TARGET)
}

// buildDomainResponse creates a response with computed fields (apex detection, A record target).
func buildDomainResponse(d *models.AppDomain, statusMessage string) *responses.AppDomainResponse {
	// Resolve our server's IP for A record instructions
	aRecordTarget := ""
	addrs, err := net.LookupHost(constants.WEMOB_CNAME_TARGET)
	if err == nil && len(addrs) > 0 {
		aRecordTarget = addrs[0]
	}

	return &responses.AppDomainResponse{
		ID:            d.ID,
		Domain:        d.Domain,
		Status:        d.Status,
		DNSRecord:     constants.WEMOB_CNAME_TARGET,
		ARecordTarget: aRecordTarget,
		IsApex:        isApexDomain(d.Domain),
		StatusMessage: statusMessage,
		CreatedAt:     d.CreatedAt,
	}
}

// GetAppDomains returns all domains for an app project.
func (c *Controller) GetAppDomains(projectID string) ([]responses.AppDomainResponse, error) {
	domains, err := c.DB.GetAppDomainsByProjectID(projectID)
	if err != nil {
		return nil, err
	}

	result := make([]responses.AppDomainResponse, len(domains))
	for i, d := range domains {
		result[i] = *buildDomainResponse(&d, "")
	}
	return result, nil
}

// AddAppDomain adds a custom domain to an app project.
// The domain is registered with Coolify if available, but stays "pending"
// until DNS is verified via VerifyAppDomain.
func (c *Controller) AddAppDomain(project *models.AppProject, user *models.User, req requests.AddAppDomainRequest) (*responses.AppDomainResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.AddAppDomainRequest](req, validate); err != nil {
		return nil, err
	}

	domainName := strings.ToLower(req.Domain)

	// WEM-262: Secondary regex validation for domain format
	if !domainRegex.MatchString(domainName) {
		return nil, errors.New("invalid domain format")
	}

	// Block manual additions of *.wemob.app subdomains — these are managed automatically
	if strings.HasSuffix(domainName, "."+constants.WEMOB_APP_BASE_DOMAIN) {
		return nil, errors.New("wemob.app subdomains are managed automatically")
	}

	// WEM-264: Check per-project domain limit
	existingDomains, err := c.DB.GetAppDomainsByProjectID(project.ID.String())
	if err == nil && len(existingDomains) >= constants.MAX_DOMAINS_PER_PROJECT {
		return nil, fmt.Errorf("maximum of %d custom domains per project reached", constants.MAX_DOMAINS_PER_PROJECT)
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
		// WEM-265: Handle unique constraint violation from DB index
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, errors.New("domain is already in use")
		}
		return nil, fmt.Errorf("failed to add domain: %v", err)
	}

	// Register with Coolify (but keep status "pending" — DNS must be verified first)
	coolifyProject, cpErr := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
	if cpErr == nil && coolifyProject != nil && c.CoolifyService != nil {
		domains := c.buildCoolifyDomainList(coolifyProject, project.ID.String(), domainName)
		if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, domains); err != nil {
			slog.Warn("Failed to add domain to Coolify", "domain", domainName, "error", err)
		}
	}

	// Run an initial DNS check — domain might already be configured
	status, msg := verifyDomainDNS(domainName)
	if status == "active" {
		created.Status = "active"
		if _, err := c.DB.UpdateAppDomain(created); err != nil {
			slog.Error("Failed to update domain status after DNS check", "domain", domainName, "error", err)
			created.Status = "pending"
		}
	}

	return buildDomainResponse(created, msg), nil
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

	// WEM-267: Bun's NewDelete().Model() with soft_delete tag automatically
	// performs a soft delete (UPDATE deleted_at) — no hard delete occurs.
	return c.DB.DeleteAppDomain(domain)
}

// VerifyAppDomain checks the DNS configuration of a custom domain and
// updates the domain status accordingly. Supports CNAME (subdomains),
// A/AAAA records (apex domains), and Cloudflare-proxied domains.
func (c *Controller) VerifyAppDomain(domainID string, project *models.AppProject) (*responses.AppDomainResponse, error) {
	domain, err := c.DB.GetAppDomainByID(domainID)
	if err != nil {
		return nil, errors.New("domain not found")
	}

	if domain.AppProjectID != project.ID {
		return nil, errors.New("domain does not belong to this project")
	}

	status, msg := verifyDomainDNS(domain.Domain)
	domain.Status = status

	// WEM-268: Check UpdateAppDomain error
	if _, err := c.DB.UpdateAppDomain(domain); err != nil {
		return nil, fmt.Errorf("failed to update domain status: %v", err)
	}

	// If domain became active and Coolify project exists, sync all domains
	if domain.Status == "active" && c.CoolifyService != nil {
		coolifyProject, cpErr := c.DB.GetCoolifyProjectByAppProjectID(project.ID.String())
		if cpErr == nil && coolifyProject != nil {
			domains := c.buildCoolifyDomainList(coolifyProject, project.ID.String(), "")
			if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, domains); err != nil {
				slog.Warn("Failed to sync verified domain to Coolify", "domain", domain.Domain, "error", err)
			}
		}
	}

	return buildDomainResponse(domain, msg), nil
}

// buildCoolifyDomainList builds the complete list of domains for a Coolify app.
// It always includes the default wemob.app subdomain, plus all active custom domains
// from the database, plus an optional extra domain being added.
func (c *Controller) buildCoolifyDomainList(coolifyProject *models.CoolifyProject, projectID string, extraDomain string) []string {
	domains := []string{"https://" + coolifyProject.CustomDomain}

	// WEM-270: Only include verified (active) custom domains
	appDomains, err := c.DB.GetAppDomainsByProjectID(projectID)
	if err == nil {
		for _, d := range appDomains {
			if d.Status == "active" {
				domains = append(domains, "https://"+d.Domain)
			}
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
