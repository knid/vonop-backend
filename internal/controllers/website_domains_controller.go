package controllers

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// AddCustomDomain adds a custom domain to a website.
func (c *Controller) AddCustomDomain(user *models.User, websiteId string, req requests.AddDomainRequest) (*responses.DomainResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.AddDomainRequest](req, validate); err != nil {
		return nil, err
	}

	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	// Check domain is not already in use
	existing, err := c.DB.GetDomainByName(req.Domain)
	if err == nil && existing.ID.String() != "" {
		return nil, errors.New("domain is already in use")
	}

	// Generate verification token
	tokenHex, err := utils.RandomHex(16)
	if err != nil {
		return nil, errors.New("failed to generate verification token")
	}
	verificationToken := "wemob-verify-" + tokenHex

	websiteUUID, err := uuid.Parse(websiteId)
	if err != nil {
		return nil, errors.New("invalid website id")
	}

	domain := models.WebsiteDomain{
		WebsiteID:          websiteUUID,
		Domain:             strings.ToLower(req.Domain),
		VerificationStatus: "pending",
		VerificationToken:  verificationToken,
		SSLStatus:          "pending",
	}

	created, err := c.DB.CreateDomain(&domain)
	if err != nil {
		return nil, fmt.Errorf("failed to add domain: %v", err)
	}

	return &responses.DomainResponse{
		ID:                 created.ID,
		WebsiteID:          created.WebsiteID,
		Domain:             created.Domain,
		VerificationStatus: created.VerificationStatus,
		VerificationToken:  created.VerificationToken,
		SSLStatus:          created.SSLStatus,
		CreatedAt:          created.CreatedAt,
	}, nil
}

// GetDomains returns all domains for a website.
func (c *Controller) GetDomains(user *models.User, websiteId string) ([]responses.DomainResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	domains, err := c.DB.GetDomainsByWebsiteId(websiteId)
	if err != nil {
		return nil, err
	}

	result := make([]responses.DomainResponse, len(domains))
	for i, d := range domains {
		result[i] = responses.DomainResponse{
			ID:                 d.ID,
			WebsiteID:          d.WebsiteID,
			Domain:             d.Domain,
			VerificationStatus: d.VerificationStatus,
			VerificationToken:  d.VerificationToken,
			SSLStatus:          d.SSLStatus,
			CreatedAt:          d.CreatedAt,
			VerifiedAt:         d.VerifiedAt,
		}
	}
	return result, nil
}

// VerifyDomain checks DNS TXT records for domain verification.
func (c *Controller) VerifyDomain(user *models.User, websiteId string, domainId string) (*responses.DomainResponse, error) {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return nil, err
	}

	domain, err := c.DB.GetDomainById(domainId)
	if err != nil {
		return nil, errors.New("domain not found")
	}

	if domain.WebsiteID.String() != websiteId {
		return nil, errors.New("domain does not belong to this website")
	}

	// Check DNS TXT records
	txtRecords, err := net.LookupTXT(domain.Domain)
	if err != nil {
		domain.VerificationStatus = "failed"
		domain.UpdatedAt = time.Now()
		c.DB.UpdateDomain(domain)
		return nil, errors.New("failed to look up DNS records. make sure you've added the TXT record")
	}

	verified := false
	for _, txt := range txtRecords {
		if txt == domain.VerificationToken {
			verified = true
			break
		}
	}

	if !verified {
		domain.VerificationStatus = "failed"
		domain.UpdatedAt = time.Now()
		c.DB.UpdateDomain(domain)
		return nil, errors.New("verification token not found in DNS TXT records")
	}

	domain.VerificationStatus = "verified"
	domain.VerifiedAt = time.Now()
	domain.UpdatedAt = time.Now()
	updated, err := c.DB.UpdateDomain(domain)
	if err != nil {
		return nil, err
	}

	return &responses.DomainResponse{
		ID:                 updated.ID,
		WebsiteID:          updated.WebsiteID,
		Domain:             updated.Domain,
		VerificationStatus: updated.VerificationStatus,
		VerificationToken:  updated.VerificationToken,
		SSLStatus:          updated.SSLStatus,
		CreatedAt:          updated.CreatedAt,
		VerifiedAt:         updated.VerifiedAt,
	}, nil
}

// RemoveCustomDomain soft-deletes a custom domain.
func (c *Controller) RemoveCustomDomain(user *models.User, websiteId string, domainId string) error {
	_, err := c.verifyWebsiteOwnership(user, websiteId)
	if err != nil {
		return err
	}

	domain, err := c.DB.GetDomainById(domainId)
	if err != nil {
		return errors.New("domain not found")
	}

	if domain.WebsiteID.String() != websiteId {
		return errors.New("domain does not belong to this website")
	}

	return c.DB.DeleteDomain(domain)
}

