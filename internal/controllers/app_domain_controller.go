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
	"golang.org/x/net/publicsuffix"
)

// domainRegex validates domain format: labels separated by dots, TLD at least 2 chars.
var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*\.[a-zA-Z]{2,}$`)

// isApexDomain returns true if the domain is a root/apex domain (e.g. "example.com"
// or "example.co.uk") as opposed to a subdomain (e.g. "www.example.com"). Uses the
// Public Suffix List so multi-part TLDs are handled correctly — "example.co.uk" is
// apex, "www.example.co.uk" is not. Falls back to a 2-label check if PSL lookup
// fails (e.g. unregistered TLD).
func isApexDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	etldPlusOne, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return strings.Count(domain, ".") == 1
	}
	return domain == etldPlusOne
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
// The URL field is intentionally only populated when the Coolify deploy is
// confirmed complete (status == "active") so the frontend cannot expose a
// live-looking URL to users while the app is still mid-deploy.
func buildDomainResponse(d *models.AppDomain, statusMessage string) *responses.AppDomainResponse {
	// Resolve our server's IP for A record instructions
	aRecordTarget := ""
	addrs, err := net.LookupHost(constants.WEMOB_CNAME_TARGET)
	if err == nil && len(addrs) > 0 {
		aRecordTarget = addrs[0]
	}

	var liveURL string
	if d.Status == "active" {
		liveURL = "https://" + d.Domain
	}

	return &responses.AppDomainResponse{
		ID:            d.ID,
		Domain:        d.Domain,
		Status:        d.Status,
		URL:           liveURL,
		DNSRecord:     constants.WEMOB_CNAME_TARGET,
		ARecordTarget: aRecordTarget,
		IsApex:        isApexDomain(d.Domain),
		StatusMessage: statusMessage,
		CreatedAt:     d.CreatedAt,
	}
}

// GetAppDomains returns all domains for an app project. Any domain that is
// still in the "deploying" state has its Coolify deployment polled so it can
// transition to "active" (build finished) or "failed" (build errored) without
// waiting for the user to manually re-verify.
func (c *Controller) GetAppDomains(projectID string) ([]responses.AppDomainResponse, error) {
	domains, err := c.DB.GetAppDomainsByProjectID(projectID)
	if err != nil {
		return nil, err
	}

	result := make([]responses.AppDomainResponse, len(domains))
	for i := range domains {
		d := &domains[i]
		if d.Status == "deploying" && d.DeploymentUUID != "" && c.CoolifyService != nil {
			c.refreshDeployingDomain(d)
		}
		result[i] = *buildDomainResponse(d, "")
	}
	return result, nil
}

// refreshDeployingDomain polls Coolify for the status of the deployment
// associated with this domain and updates the domain's status in-place
// (and in the DB) if the deploy reached a terminal state.
func (c *Controller) refreshDeployingDomain(d *models.AppDomain) {
	dep, err := c.CoolifyService.GetDeploymentByUUID(d.DeploymentUUID)
	if err != nil {
		slog.Warn("Failed to poll Coolify deployment for domain", "domain", d.Domain, "deployment_uuid", d.DeploymentUUID, "error", err)
		return
	}

	coolifyStatus := strings.ToLower(dep.Status)
	switch {
	case coolifyStatus == "finished" || coolifyStatus == "success":
		d.Status = "active"
		d.DeploymentUUID = ""
	case coolifyStatus == "failed" || coolifyStatus == "error" || coolifyStatus == "cancelled":
		d.Status = "failed"
		d.DeploymentUUID = ""
	default:
		// still in-progress (queued, running, ...) — leave as "deploying"
		return
	}

	if _, err := c.DB.UpdateAppDomain(d); err != nil {
		slog.Error("Failed to persist domain status after deploy poll", "domain", d.Domain, "error", err)
	}
}

// AddAppDomain adds a custom domain to an app project.
// The domain is stored with status "pending" and DNS instructions for the
// user; actual proxy activation happens when the Caddy config is reloaded
// (a separate concern). DNS verification is done via VerifyAppDomain.
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

	// Check domain is not already attached to a different project
	existing, err := c.DB.GetAppDomainByName(domainName)
	if err == nil && existing != nil && existing.ID.String() != "" {
		if existing.AppProjectID != project.ID {
			return nil, errors.New("domain already attached to another app")
		}
		return nil, errors.New("domain is already in use")
	}

	// Runtime row must exist (Fly app must be created before attaching a custom domain)
	runtimeApp, err := c.DB.GetRuntimeAppByAppProjectID(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("publish the app first before adding a custom domain")
	}

	domain := models.AppDomain{
		AppProjectID:    project.ID,
		Domain:          domainName,
		Status:          "pending",
		DNSCheckStatus:  "pending",
		DNSInstructions: dnsInstructionsFor(runtimeApp.FlyAppName, domainName),
	}

	created, err := c.DB.CreateAppDomain(&domain)
	if err != nil {
		// WEM-265: Handle unique constraint violation from DB index
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, errors.New("domain is already in use")
		}
		return nil, fmt.Errorf("failed to add domain: %v", err)
	}

	// Run an initial DNS check — domain might already be configured
	_, msg := verifyDomainDNS(domainName)

	return buildDomainResponse(created, msg), nil
}

// dnsInstructionsFor returns the DNS records a user must create at their
// registrar to point the given hostname at the Wemob edge.
func dnsInstructionsFor(flyAppName, hostname string) map[string]any {
	if isApexDomain(hostname) {
		return map[string]any{
			"records": []map[string]string{
				{"type": "A", "host": hostname, "value": "<edge-ipv4>"},
				{"type": "AAAA", "host": hostname, "value": "<edge-ipv6>"},
			},
			"note": "These records must propagate before TLS can be issued.",
		}
	}
	return map[string]any{
		"records": []map[string]string{
			{"type": "CNAME", "host": hostname, "value": "edge.wemob.app"},
		},
		"note": "These records must propagate before TLS can be issued.",
	}
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
		coolifyProject, cpErr := c.DB.GetRuntimeAppByAppProjectID(project.ID.String())
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
			if _, err := c.CoolifyService.DeployApp(coolifyProject.CoolifyAppUUID); err != nil {
				slog.Warn("Failed to redeploy Coolify app after domain remove", "domain", domain.Domain, "error", err)
			}
		}
	}

	// WEM-267: Bun's NewDelete().Model() with soft_delete tag automatically
	// performs a soft delete (UPDATE deleted_at) — no hard delete occurs.
	if err := c.DB.DeleteAppDomain(domain); err != nil {
		return err
	}

	if err := c.CaddyAdmin.RemoveCustomDomainRoute(domain.Domain); err != nil {
		slog.Warn("caddy_route_remove_failed", "domain", domain.Domain, "err", err)
	}
	return nil
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

	// If the domain is already mid-deploy, don't re-trigger Coolify — just
	// poll the existing deployment and report its current state back.
	if domain.Status == "deploying" && domain.DeploymentUUID != "" && c.CoolifyService != nil {
		c.refreshDeployingDomain(domain)
		msg := "DNS verified — waiting for Coolify to finish deploying."
		if domain.Status == "active" {
			msg = "Deployment finished — your custom domain is live."
		} else if domain.Status == "failed" {
			msg = "Coolify build failed — try redeploying or contact support."
		}
		return buildDomainResponse(domain, msg), nil
	}

	status, msg := verifyDomainDNS(domain.Domain)
	if status != "active" {
		domain.Status = status
	} else {
		domain.Status = "active"
	}

	// If DNS is verified and Coolify project exists, sync all domains and
	// kick off a redeploy. The domain stays in "deploying" state until the
	// Coolify build finishes so the frontend doesn't expose the URL early.
	var redeployTriggered bool
	if status == "active" && c.CoolifyService != nil {
		coolifyProject, cpErr := c.DB.GetRuntimeAppByAppProjectID(project.ID.String())
		if cpErr == nil && coolifyProject != nil {
			// Pass domain.Domain as extraDomain so it's included even when its
			// persisted status is still "pending" (first-time verification).
			domains := c.buildCoolifyDomainList(coolifyProject, project.ID.String(), domain.Domain)
			if err := c.CoolifyService.UpdateDomains(coolifyProject.CoolifyAppUUID, domains); err != nil {
				slog.Warn("Failed to sync verified domain to Coolify", "domain", domain.Domain, "error", err)
			} else if deploymentUUID, err := c.CoolifyService.DeployApp(coolifyProject.CoolifyAppUUID); err != nil {
				slog.Warn("Failed to redeploy Coolify app after domain verify", "domain", domain.Domain, "error", err)
			} else {
				redeployTriggered = true
				domain.Status = "deploying"
				domain.DeploymentUUID = deploymentUUID
				msg = "DNS verified — waiting for Coolify to finish deploying."
			}
		}
	}

	// WEM-268: Check UpdateAppDomain error
	if _, err := c.DB.UpdateAppDomain(domain); err != nil {
		return nil, fmt.Errorf("failed to update domain status: %v", err)
	}

	resp := buildDomainResponse(domain, msg)
	resp.RedeployTriggered = redeployTriggered
	return resp, nil
}

// buildCoolifyDomainList builds the complete list of domains for a Coolify app.
// It always includes the default wemob.app subdomain, plus all verified custom
// domains from the database (whether already live or mid-deploy), plus an
// optional extra domain being added.
func (c *Controller) buildCoolifyDomainList(coolifyProject *models.RuntimeApp, projectID string, extraDomain string) []string {
	domains := []string{"https://" + coolifyProject.CustomDomain}

	// WEM-270: Only include verified custom domains. A "deploying" domain is
	// still verified — Coolify already knows about it, we just haven't observed
	// the build finish yet, so we must keep it in the list.
	appDomains, err := c.DB.GetAppDomainsByProjectID(projectID)
	if err == nil {
		for _, d := range appDomains {
			if d.Status == "active" || d.Status == "deploying" {
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
