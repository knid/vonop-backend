package controllers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetOrCreateReferralCode(user *models.User) (*responses.ReferralCodeResponse, error) {
	code, err := c.DB.GetReferralCodeByUserID(user.ID.String())
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		// Generate a new code
		raw := make([]byte, 6)
		if _, err := rand.Read(raw); err != nil {
			return nil, errors.New("failed to generate referral code")
		}
		codeStr := strings.ToUpper(hex.EncodeToString(raw))[:8]

		newCode := models.ReferralCode{
			UserID: user.ID,
			Code:   codeStr,
		}
		code, err = c.DB.CreateReferralCode(&newCode)
		if err != nil {
			return nil, err
		}
	}

	return &responses.ReferralCodeResponse{
		Code:      code.Code,
		CreatedAt: code.CreatedAt,
	}, nil
}

func (c *Controller) GetReferralStats(user *models.User) (*responses.ReferralStatsResponse, error) {
	// Auto-create referral code if user doesn't have one yet
	codeResp, err := c.GetOrCreateReferralCode(user)
	if err != nil {
		return nil, err
	}

	referrals, err := c.DB.GetReferralsByReferrerID(user.ID.String())
	if err != nil {
		return nil, err
	}

	var refs []responses.ReferralResponse
	totalRewards := 0
	for _, r := range referrals {
		resp := responses.ReferralResponse{
			ID:                        r.ID,
			RewardGranted:             r.RewardGranted,
			SubscriptionRewardGranted: r.SubscriptionRewardGranted,
			CreatedAt:                 r.CreatedAt,
		}
		if r.Referred != nil {
			resp.ReferredEmail = r.Referred.Email
			resp.ReferredName = r.Referred.Name
		}
		if r.RewardGranted {
			totalRewards += models.ReferralSignupCredits
		}
		if r.SubscriptionRewardGranted {
			totalRewards += models.ReferralSubscriptionCredits
		}
		refs = append(refs, resp)
	}

	return &responses.ReferralStatsResponse{
		Code:           codeResp.Code,
		TotalReferrals: len(referrals),
		TotalRewards:   totalRewards,
		Referrals:      refs,
	}, nil
}

func (c *Controller) ApplyReferralCode(user *models.User, codeStr string) error {
	// Check if user already used a referral code
	_, err := c.DB.GetReferralByReferredID(user.ID.String())
	if err == nil {
		return errors.New("you have already used a referral code")
	}

	code, err := c.DB.GetReferralCodeByCode(codeStr)
	if err != nil {
		return errors.New("invalid referral code")
	}

	// Can't refer yourself
	if code.UserID == user.ID {
		return errors.New("you cannot use your own referral code")
	}

	referral := models.Referral{
		ReferrerID:     code.UserID,
		ReferredID:     user.ID,
		ReferralCodeID: code.ID,
		RewardGranted:  true,
	}

	_, err = c.DB.CreateReferral(&referral)
	if err != nil {
		return err
	}

	// Grant signup credits (10) to both parties
	referrer, err := c.DB.GetUserById(code.UserID.String())
	if err == nil {
		if err := c.AddAICredits(referrer, models.ReferralSignupCredits, "referral_signup_reward", user.ID.String()); err != nil {
			slog.Error("failed to grant referral signup reward credits", "error", err, "referrer_id", referrer.ID, "referred_id", user.ID)
		}
	}
	if err := c.AddAICredits(user, models.ReferralSignupCredits, "referral_signup_bonus", code.UserID.String()); err != nil {
		slog.Error("failed to grant referral signup bonus credits", "error", err, "user_id", user.ID, "referrer_code_owner", code.UserID)
	}

	return nil
}

// GrantReferralSubscriptionReward grants 50 credits to both parties when
// the referred user starts a subscription. Uses an atomic DB claim so
// concurrent calls are safe — only the first caller grants credits.
func (c *Controller) GrantReferralSubscriptionReward(user *models.User) {
	referral, err := c.DB.GetReferralByReferredID(user.ID.String())
	if err != nil {
		// User was not referred — nothing to do
		return
	}

	// Atomic claim: only proceeds if subscription_reward_granted was false
	claimed, err := c.DB.ClaimSubscriptionReward(referral.ID.String())
	if err != nil {
		slog.Error("failed to claim referral subscription reward", "error", err, "referral_id", referral.ID)
		return
	}
	if !claimed {
		// Already granted by another request
		return
	}

	// Grant subscription credits (50) to referrer
	referrer, err := c.DB.GetUserById(referral.ReferrerID.String())
	if err == nil {
		if err := c.AddAICredits(referrer, models.ReferralSubscriptionCredits, "referral_subscription_reward", user.ID.String()); err != nil {
			slog.Error("failed to grant referral subscription reward credits", "error", err, "referrer_id", referrer.ID, "referred_id", user.ID)
		}
	}

	// Grant subscription credits (50) to referred user
	if err := c.AddAICredits(user, models.ReferralSubscriptionCredits, "referral_subscription_bonus", referral.ReferrerID.String()); err != nil {
		slog.Error("failed to grant referral subscription bonus credits", "error", err, "user_id", user.ID, "referrer_id", referral.ReferrerID)
	}
}
