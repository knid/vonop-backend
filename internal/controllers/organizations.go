package controllers

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (c *Controller) CreateOrganization(user *models.User, req requests.CreateOrganizationRequest) (*responses.OrganizationResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.CreateOrganizationRequest](req, validate); err != nil {
		return nil, err
	}

	org := models.Organization{
		Name:    req.Name,
		Slug:    strings.ToLower(req.Slug),
		OwnerID: user.ID,
		Logo:    req.Logo,
	}

	createdOrg, err := c.DB.CreateOrganization(&org)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, errors.New("organization with this slug already exists")
		}
		return nil, err
	}

	// Add the creator as owner member
	member := models.OrganizationMember{
		OrganizationID: createdOrg.ID,
		UserID:         user.ID,
		Role:           "owner",
	}
	c.DB.CreateOrganizationMember(&member)

	return &responses.OrganizationResponse{
		ID:        createdOrg.ID,
		Name:      createdOrg.Name,
		Slug:      createdOrg.Slug,
		Logo:      createdOrg.Logo,
		OwnerID:   createdOrg.OwnerID,
		CreatedAt: createdOrg.CreatedAt,
	}, nil
}

func (c *Controller) GetUserOrganizations(user *models.User) ([]responses.OrganizationResponse, error) {
	orgs, err := c.DB.GetOrganizationsByUserID(user.ID.String())
	if err != nil {
		return nil, err
	}

	var result []responses.OrganizationResponse
	for _, org := range orgs {
		result = append(result, responses.OrganizationResponse{
			ID:        org.ID,
			Name:      org.Name,
			Slug:      org.Slug,
			Logo:      org.Logo,
			OwnerID:   org.OwnerID,
			CreatedAt: org.CreatedAt,
		})
	}
	return result, nil
}

func (c *Controller) GetOrganization(user *models.User, orgID string) (*responses.OrganizationResponse, error) {
	// Verify user is a member
	_, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return nil, errors.New("organization not found or access denied")
	}

	org, err := c.DB.GetOrganizationByID(orgID)
	if err != nil {
		return nil, err
	}

	return &responses.OrganizationResponse{
		ID:        org.ID,
		Name:      org.Name,
		Slug:      org.Slug,
		Logo:      org.Logo,
		OwnerID:   org.OwnerID,
		CreatedAt: org.CreatedAt,
	}, nil
}

func (c *Controller) UpdateOrganization(user *models.User, orgID string, req requests.UpdateOrganizationRequest) (*responses.OrganizationResponse, error) {
	member, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return nil, errors.New("organization not found or access denied")
	}
	if member.Role != "owner" && member.Role != "admin" {
		return nil, errors.New("insufficient permissions")
	}

	org, err := c.DB.GetOrganizationByID(orgID)
	if err != nil {
		return nil, err
	}

	org.Name = req.Name
	if req.Logo != "" {
		org.Logo = req.Logo
	}

	updated, err := c.DB.UpdateOrganization(org)
	if err != nil {
		return nil, err
	}

	return &responses.OrganizationResponse{
		ID:        updated.ID,
		Name:      updated.Name,
		Slug:      updated.Slug,
		Logo:      updated.Logo,
		OwnerID:   updated.OwnerID,
		CreatedAt: updated.CreatedAt,
	}, nil
}

func (c *Controller) DeleteOrganization(user *models.User, orgID string) error {
	org, err := c.DB.GetOrganizationByID(orgID)
	if err != nil {
		return errors.New("organization not found")
	}
	if org.OwnerID != user.ID {
		return errors.New("only the owner can delete an organization")
	}
	if org.IsPersonal {
		return errors.New("personal workspaces cannot be deleted")
	}
	return c.DB.DeleteOrganization(org)
}

func (c *Controller) GetOrganizationMembers(user *models.User, orgID string) ([]responses.OrganizationMemberResponse, error) {
	_, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return nil, errors.New("organization not found or access denied")
	}

	members, err := c.DB.GetOrganizationMembers(orgID)
	if err != nil {
		return nil, err
	}

	var result []responses.OrganizationMemberResponse
	for _, m := range members {
		resp := responses.OrganizationMemberResponse{
			ID:        m.ID,
			UserID:    m.UserID,
			Role:      m.Role,
			CreatedAt: m.CreatedAt,
		}
		if m.User != nil {
			resp.UserName = m.User.Name
			resp.UserEmail = m.User.Email
		}
		result = append(result, resp)
	}
	return result, nil
}

func (c *Controller) InviteOrganizationMember(user *models.User, orgID string, req requests.InviteMemberRequest) (*responses.OrganizationInviteResponse, error) {
	member, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return nil, errors.New("organization not found or access denied")
	}
	if member.Role != "owner" && member.Role != "admin" {
		return nil, errors.New("insufficient permissions to invite members")
	}

	token, err := utils.RandomHex(32)
	if err != nil {
		return nil, err
	}

	invite := models.OrganizationInvite{
		OrganizationID: member.OrganizationID,
		Email:          req.Email,
		Role:           req.Role,
		InvitedByID:    user.ID,
		Token:          token,
		ExpiresAt:      time.Now().AddDate(0, 0, 7),
	}

	created, err := c.DB.CreateOrganizationInvite(&invite)
	if err != nil {
		return nil, err
	}

	return &responses.OrganizationInviteResponse{
		ID:        created.ID,
		Email:     created.Email,
		Role:      created.Role,
		ExpiresAt: created.ExpiresAt,
		CreatedAt: created.CreatedAt,
	}, nil
}

func (c *Controller) AcceptOrganizationInvite(user *models.User, inviteToken string) (*responses.OrganizationMemberResponse, error) {
	invite, err := c.DB.GetOrganizationInviteByToken(inviteToken)
	if err != nil {
		return nil, errors.New("invite not found or expired")
	}

	if invite.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("invite has expired")
	}

	if !invite.AcceptedAt.IsZero() {
		return nil, errors.New("invite has already been accepted")
	}

	if user.Email != invite.Email {
		return nil, errors.New("invite was sent to a different email address")
	}

	// Create member
	newMember := models.OrganizationMember{
		OrganizationID: invite.OrganizationID,
		UserID:         user.ID,
		Role:           invite.Role,
	}

	created, err := c.DB.CreateOrganizationMember(&newMember)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, errors.New("you are already a member of this organization")
		}
		return nil, err
	}

	// Mark invite as accepted
	invite.AcceptedAt = time.Now()
	c.DB.UpdateOrganizationInvite(invite)

	return &responses.OrganizationMemberResponse{
		ID:        created.ID,
		UserID:    created.UserID,
		UserName:  user.Name,
		UserEmail: user.Email,
		Role:      created.Role,
		CreatedAt: created.CreatedAt,
	}, nil
}

func (c *Controller) RemoveOrganizationMember(user *models.User, orgID, memberID string) error {
	callerMember, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return errors.New("organization not found or access denied")
	}
	if callerMember.Role != "owner" && callerMember.Role != "admin" {
		return errors.New("insufficient permissions")
	}

	org, err := c.DB.GetOrganizationByID(orgID)
	if err != nil {
		return err
	}

	targetMember, err := c.DB.GetOrganizationMember(orgID, memberID)
	if err != nil {
		return errors.New("member not found")
	}

	if targetMember.UserID == org.OwnerID {
		return errors.New("cannot remove the organization owner")
	}

	return c.DB.DeleteOrganizationMember(targetMember)
}

func (c *Controller) UpdateMemberRole(user *models.User, orgID, memberID string, req requests.UpdateMemberRoleRequest) (*responses.OrganizationMemberResponse, error) {
	callerMember, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return nil, errors.New("organization not found or access denied")
	}
	if callerMember.Role != "owner" {
		return nil, errors.New("only the owner can change member roles")
	}

	targetMember, err := c.DB.GetOrganizationMember(orgID, memberID)
	if err != nil {
		return nil, errors.New("member not found")
	}

	if targetMember.Role == "owner" {
		return nil, errors.New("cannot change the owner's role")
	}

	targetMember.Role = req.Role
	updated, err := c.DB.UpdateOrganizationMember(targetMember)
	if err != nil {
		return nil, err
	}

	return &responses.OrganizationMemberResponse{
		ID:        updated.ID,
		UserID:    updated.UserID,
		Role:      updated.Role,
		CreatedAt: updated.CreatedAt,
	}, nil
}

// ListPendingInvites returns the pending invite list for an org. Caller must be owner or admin.
func (c *Controller) ListPendingInvites(user *models.User, orgID string) ([]responses.OrganizationInviteResponse, error) {
	member, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return nil, errors.New("organization not found or access denied")
	}
	if member.Role != "owner" && member.Role != "admin" {
		return nil, errors.New("insufficient permissions")
	}
	invites, err := c.DB.ListPendingOrganizationInvites(orgID)
	if err != nil {
		return nil, err
	}
	out := make([]responses.OrganizationInviteResponse, 0, len(invites))
	for _, inv := range invites {
		out = append(out, responses.OrganizationInviteResponse{
			ID:        inv.ID,
			Email:     inv.Email,
			Role:      inv.Role,
			ExpiresAt: inv.ExpiresAt,
			CreatedAt: inv.CreatedAt,
		})
	}
	return out, nil
}

// CancelOrganizationInvite hard-deletes a pending invite. Caller must be owner/admin.
func (c *Controller) CancelOrganizationInvite(user *models.User, orgID, inviteID string) error {
	member, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return errors.New("organization not found or access denied")
	}
	if member.Role != "owner" && member.Role != "admin" {
		return errors.New("insufficient permissions")
	}
	return c.DB.DeleteOrganizationInvite(orgID, inviteID)
}

// TransferOrganizationOwnership transfers org ownership. Preconditions: caller
// is current owner; new owner is a member; org is not personal.
func (c *Controller) TransferOrganizationOwnership(user *models.User, orgID string, newOwnerUserID uuid.UUID) (*responses.OrganizationResponse, error) {
	org, err := c.DB.GetOrganizationByID(orgID)
	if err != nil {
		return nil, errors.New("organization not found")
	}
	if org.IsPersonal {
		return nil, errors.New("personal workspaces cannot transfer ownership")
	}
	if org.OwnerID != user.ID {
		return nil, errors.New("only the current owner can transfer ownership")
	}
	if newOwnerUserID == user.ID {
		return nil, errors.New("you are already the owner")
	}
	if _, err := c.DB.GetOrganizationMember(orgID, newOwnerUserID.String()); err != nil {
		return nil, errors.New("new owner must be an existing member of this organization")
	}
	updated, err := c.DB.TransferOrganizationOwnership(orgID, user.ID, newOwnerUserID)
	if err != nil {
		return nil, err
	}
	return &responses.OrganizationResponse{
		ID:        updated.ID,
		Name:      updated.Name,
		Slug:      updated.Slug,
		Logo:      updated.Logo,
		OwnerID:   updated.OwnerID,
		CreatedAt: updated.CreatedAt,
	}, nil
}

// LeaveOrganization removes the caller from the org. Personal orgs reject.
// Sole owners of non-personal orgs must transfer ownership first.
func (c *Controller) LeaveOrganization(user *models.User, orgID string) error {
	org, err := c.DB.GetOrganizationByID(orgID)
	if err != nil {
		return errors.New("organization not found")
	}
	if org.IsPersonal {
		return errors.New("you cannot leave your personal workspace")
	}
	member, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return errors.New("you are not a member of this organization")
	}
	if member.Role == "owner" {
		return errors.New("transfer ownership before leaving as the sole owner")
	}
	return c.DB.DeleteOrganizationMember(member)
}

// EnsurePersonalOrg returns the user's personal organization, creating it
// if one does not exist. Idempotent — safe to call from signup paths and
// from backfill. Slug is "<slugified-name>-<random6>"; we always append a
// suffix to avoid scanning the table for free names.
func (c *Controller) EnsurePersonalOrg(user *models.User) (*models.Organization, error) {
	existing, err := c.DB.GetPersonalOrgByUserID(user.ID.String())
	if err == nil && existing != nil {
		return existing, nil
	}

	displayName := strings.TrimSpace(user.Name)
	if displayName == "" {
		displayName = "Workspace"
	}
	orgName := displayName + "'s Workspace"

	base := utils.SlugifyName(displayName)
	slug := utils.SlugWithSuffix(base)

	org := &models.Organization{
		Name:       orgName,
		Slug:       slug,
		OwnerID:    user.ID,
		IsPersonal: true,
	}
	created, err := c.DB.CreateOrganization(org)
	if err != nil {
		return nil, fmt.Errorf("create personal org: %w", err)
	}

	member := &models.OrganizationMember{
		OrganizationID: created.ID,
		UserID:         user.ID,
		Role:           "owner",
	}
	if _, err := c.DB.CreateOrganizationMember(member); err != nil {
		return nil, fmt.Errorf("create personal org owner membership: %w", err)
	}

	return created, nil
}

// RequireOrgIDForUser returns the org UUID a project should be created in,
// given a user and an optional explicit org ID from the request.
//
// If reqOrgID is nil/zero, return the user's personal org. If non-zero, verify
// the user is a member with role owner/admin (creators must have write rights
// on the destination org). Returns an error if the user is not a member or
// does not have a personal org yet (the latter would only happen if backfill
// was skipped — defensive).
func (c *Controller) RequireOrgIDForUser(user *models.User, reqOrgID *uuid.UUID) (uuid.UUID, error) {
	if reqOrgID != nil && *reqOrgID != uuid.Nil {
		member, err := c.DB.GetOrganizationMember(reqOrgID.String(), user.ID.String())
		if err != nil {
			return uuid.Nil, errors.New("organization not found or access denied")
		}
		if member.Role != "owner" && member.Role != "admin" {
			return uuid.Nil, errors.New("insufficient permissions to create project in this organization")
		}
		return *reqOrgID, nil
	}

	personal, err := c.EnsurePersonalOrg(user)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve personal org: %w", err)
	}
	return personal.ID, nil
}

// RequireOrgMembership verifies the user is a member of the org (any role)
// and returns the org's UUID. Use from list handlers when ?org_id= is given.
func (c *Controller) RequireOrgMembership(user *models.User, orgIDStr string) (uuid.UUID, error) {
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		return uuid.Nil, errors.New("invalid organization id")
	}
	if _, err := c.DB.GetOrganizationMember(orgIDStr, user.ID.String()); err != nil {
		return uuid.Nil, errors.New("organization not found or access denied")
	}
	return orgID, nil
}
