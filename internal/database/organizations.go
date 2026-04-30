package database

import (
	"context"
	"errors"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateOrganization(org *models.Organization) (*models.Organization, error) {
	_, err := db.DB.NewInsert().Model(org).Exec(context.Background())
	return org, err
}

func (db *Database) GetOrganizationByID(id string) (*models.Organization, error) {
	var org models.Organization
	err := db.DB.NewSelect().
		Model(&org).
		Where("id = ?", id).
		Scan(context.Background())
	return &org, err
}

func (db *Database) GetOrganizationBySlug(slug string) (*models.Organization, error) {
	var org models.Organization
	err := db.DB.NewSelect().
		Model(&org).
		Where("slug = ?", slug).
		Scan(context.Background())
	return &org, err
}

func (db *Database) GetOrganizationsByUserID(userID string) ([]models.Organization, error) {
	var orgs []models.Organization
	err := db.DB.NewSelect().
		Model(&orgs).
		Join("JOIN organization_members AS om ON om.organization_id = organization.id").
		Where("om.user_id = ?", userID).
		Where("om.deleted_at IS NULL").
		Scan(context.Background())
	return orgs, err
}

func (db *Database) UpdateOrganization(org *models.Organization) (*models.Organization, error) {
	org.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().
		Model(org).
		Where("id = ?", org.ID).
		Exec(context.Background())
	return org, err
}

func (db *Database) DeleteOrganization(org *models.Organization) error {
	_, err := db.DB.NewDelete().
		Model(org).
		Where("id = ?", org.ID).
		Exec(context.Background())
	return err
}

// Members

func (db *Database) CreateOrganizationMember(member *models.OrganizationMember) (*models.OrganizationMember, error) {
	_, err := db.DB.NewInsert().Model(member).Exec(context.Background())
	return member, err
}

func (db *Database) GetOrganizationMembers(orgID string) ([]models.OrganizationMember, error) {
	var members []models.OrganizationMember
	err := db.DB.NewSelect().
		Model(&members).
		Relation("User").
		Where("organization_id = ?", orgID).
		Scan(context.Background())
	return members, err
}

func (db *Database) GetOrganizationMember(orgID, userID string) (*models.OrganizationMember, error) {
	var member models.OrganizationMember
	err := db.DB.NewSelect().
		Model(&member).
		Where("organization_id = ?", orgID).
		Where("user_id = ?", userID).
		Scan(context.Background())
	return &member, err
}

func (db *Database) UpdateOrganizationMember(member *models.OrganizationMember) (*models.OrganizationMember, error) {
	member.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().
		Model(member).
		Where("id = ?", member.ID).
		Exec(context.Background())
	return member, err
}

func (db *Database) DeleteOrganizationMember(member *models.OrganizationMember) error {
	_, err := db.DB.NewDelete().
		Model(member).
		Where("id = ?", member.ID).
		Exec(context.Background())
	return err
}

// Invites

func (db *Database) CreateOrganizationInvite(invite *models.OrganizationInvite) (*models.OrganizationInvite, error) {
	_, err := db.DB.NewInsert().Model(invite).Exec(context.Background())
	return invite, err
}

func (db *Database) GetOrganizationInviteByToken(token string) (*models.OrganizationInvite, error) {
	var invite models.OrganizationInvite
	err := db.DB.NewSelect().
		Model(&invite).
		Where("token = ?", token).
		Scan(context.Background())
	return &invite, err
}

func (db *Database) GetOrganizationInvites(orgID string) ([]models.OrganizationInvite, error) {
	var invites []models.OrganizationInvite
	err := db.DB.NewSelect().
		Model(&invites).
		Where("organization_id = ?", orgID).
		Where("accepted_at IS NULL").
		Where("expires_at > NOW()").
		Scan(context.Background())
	return invites, err
}

func (db *Database) UpdateOrganizationInvite(invite *models.OrganizationInvite) (*models.OrganizationInvite, error) {
	_, err := db.DB.NewUpdate().
		Model(invite).
		Where("id = ?", invite.ID).
		Exec(context.Background())
	return invite, err
}

func (db *Database) GetOrganizationMemberCount(orgID string) (int, error) {
	count, err := db.DB.NewSelect().
		Model((*models.OrganizationMember)(nil)).
		Where("organization_id = ?", orgID).
		Count(context.Background())
	return count, err
}

func (db *Database) CountOrganizationsByUserID(userID string) (int, error) {
	count, err := db.DB.NewSelect().
		Model((*models.OrganizationMember)(nil)).
		Where("user_id = ?", userID).
		Count(context.Background())
	return count, err
}

// GetPersonalOrgByUserID returns the user's personal organization (the auto-
// created one with IsPersonal=true and owner=user). Returns sql.ErrNoRows if
// the user has not been backfilled yet.
func (db *Database) GetPersonalOrgByUserID(userID string) (*models.Organization, error) {
	var org models.Organization
	err := db.DB.NewSelect().
		Model(&org).
		Where("owner_id = ?", userID).
		Where("is_personal = ?", true).
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &org, nil
}

// ListPendingOrganizationInvites returns invites for the given org that have
// not yet been accepted and are not expired. Sorted by created_at DESC.
func (db *Database) ListPendingOrganizationInvites(orgID string) ([]models.OrganizationInvite, error) {
	var invites []models.OrganizationInvite
	err := db.DB.NewSelect().Model(&invites).
		Where("organization_id = ?", orgID).
		Where("accepted_at IS NULL").
		Where("expires_at > NOW()").
		Order("created_at DESC").
		Scan(context.Background())
	return invites, err
}

// DeleteOrganizationInvite hard-deletes an invite by id within an org. Used
// by cancel-invite. Returns an error if no matching invite.
func (db *Database) DeleteOrganizationInvite(orgID, inviteID string) error {
	res, err := db.DB.NewDelete().
		Model((*models.OrganizationInvite)(nil)).
		Where("organization_id = ?", orgID).
		Where("id = ?", inviteID).
		Exec(context.Background())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("invite not found")
	}
	return nil
}

// UpdateMemberCap sets monthly_credit_cap on an OrganizationMember. cap=nil
// clears the cap (member becomes unlimited within the pool). Returns the
// updated member.
func (db *Database) UpdateMemberCap(orgID, memberID string, cap *int) (*models.OrganizationMember, error) {
	q := db.DB.NewUpdate().
		Model((*models.OrganizationMember)(nil)).
		Where("organization_id = ?", orgID).
		Where("id = ?", memberID)
	if cap == nil {
		q = q.Set("monthly_credit_cap = NULL")
	} else {
		q = q.Set("monthly_credit_cap = ?", *cap)
	}
	if _, err := q.Exec(context.Background()); err != nil {
		return nil, err
	}
	var updated models.OrganizationMember
	if err := db.DB.NewSelect().Model(&updated).
		Where("id = ?", memberID).
		Where("organization_id = ?", orgID).
		Scan(context.Background()); err != nil {
		return nil, err
	}
	return &updated, nil
}
