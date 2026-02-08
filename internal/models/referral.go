package models

import (
	"time"

	"github.com/google/uuid"
)

// ReferralCode is a unique code a user can share to invite others.
type ReferralCode struct {
	ID        uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID    uuid.UUID `bun:",notnull,type:uuid,unique"`
	User      *User     `bun:"rel:belongs-to,join:user_id=id"`
	Code      string    `bun:",unique,notnull"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// Referral tracks a completed referral relationship.
type Referral struct {
	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ReferrerID     uuid.UUID `bun:",notnull,type:uuid"` // user who shared the code
	Referrer       *User     `bun:"rel:belongs-to,join:referrer_id=id"`
	ReferredID     uuid.UUID `bun:",notnull,type:uuid"` // user who signed up with the code
	Referred       *User     `bun:"rel:belongs-to,join:referred_id=id"`
	ReferralCodeID uuid.UUID `bun:",notnull,type:uuid"`
	RewardGranted  bool      `bun:",notnull,default:false"`
	CreatedAt      time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// Referral reward amount (AI credits given to both parties)
const ReferralRewardCredits = 50
