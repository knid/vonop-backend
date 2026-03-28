package responses

import (
	"time"

	"github.com/google/uuid"
)

type ReferralCodeResponse struct {
	Code      string    `json:"code"`
	CreatedAt time.Time `json:"created_at"`
}

type ReferralResponse struct {
	ID                        uuid.UUID `json:"id"`
	ReferredEmail             string    `json:"referred_email"`
	ReferredName              string    `json:"referred_name"`
	RewardGranted             bool      `json:"reward_granted"`
	SubscriptionRewardGranted bool      `json:"subscription_reward_granted"`
	CreatedAt                 time.Time `json:"created_at"`
}

type ReferralStatsResponse struct {
	Code           string             `json:"code"`
	TotalReferrals int                `json:"total_referrals"`
	TotalRewards   int                `json:"total_rewards"`
	Referrals      []ReferralResponse `json:"referrals"`
}
