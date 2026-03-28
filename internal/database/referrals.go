package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateReferralCode(code *models.ReferralCode) (*models.ReferralCode, error) {
	_, err := db.DB.NewInsert().Model(code).Exec(context.Background())
	return code, err
}

func (db *Database) GetReferralCodeByUserID(userID string) (*models.ReferralCode, error) {
	var code models.ReferralCode
	err := db.DB.NewSelect().
		Model(&code).
		Where("user_id = ?", userID).
		Scan(context.Background())
	return &code, err
}

func (db *Database) GetReferralCodeByCode(code string) (*models.ReferralCode, error) {
	var rc models.ReferralCode
	err := db.DB.NewSelect().
		Model(&rc).
		Where("code = ?", code).
		Scan(context.Background())
	return &rc, err
}

func (db *Database) CreateReferral(referral *models.Referral) (*models.Referral, error) {
	_, err := db.DB.NewInsert().Model(referral).Exec(context.Background())
	return referral, err
}

func (db *Database) GetReferralsByReferrerID(referrerID string) ([]models.Referral, error) {
	var referrals []models.Referral
	err := db.DB.NewSelect().
		Model(&referrals).
		Relation("Referred").
		Where("referrer_id = ?", referrerID).
		OrderExpr("created_at DESC").
		Scan(context.Background())
	return referrals, err
}

func (db *Database) GetReferralByReferredID(referredID string) (*models.Referral, error) {
	var referral models.Referral
	err := db.DB.NewSelect().
		Model(&referral).
		Where("referred_id = ?", referredID).
		Scan(context.Background())
	return &referral, err
}

func (db *Database) UpdateReferral(referral *models.Referral) (*models.Referral, error) {
	_, err := db.DB.NewUpdate().
		Model(referral).
		Where("id = ?", referral.ID).
		Exec(context.Background())
	return referral, err
}

// ClaimSubscriptionReward atomically sets subscription_reward_granted = true
// only if it is currently false. Returns true if the row was updated (i.e.
// this caller won the race), false if already claimed.
func (db *Database) ClaimSubscriptionReward(referralID string) (bool, error) {
	res, err := db.DB.NewUpdate().
		Model((*models.Referral)(nil)).
		Set("subscription_reward_granted = TRUE").
		Where("id = ?", referralID).
		Where("subscription_reward_granted = FALSE").
		Exec(context.Background())
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}
