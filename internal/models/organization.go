package models

import (
	"time"

	"github.com/google/uuid"
)

// Organization represents a team/company that can own projects and apps.
// IsPersonal=true marks the auto-created personal workspace each user receives
// at signup. Personal orgs cannot be deleted and the sole-owner cannot leave.
type Organization struct {
	ID         uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	Name       string    `bun:",notnull"`
	Slug       string    `bun:",unique,notnull"`
	OwnerID    uuid.UUID `bun:",notnull,type:uuid"`
	Owner      *User     `bun:"rel:belongs-to,join:owner_id=id"`
	Logo       string    `bun:",nullzero"`
	IsPersonal bool      `bun:",notnull,default:false"`
	// --- Billing (Plan 5) — single source of truth; matching User columns are deprecated ---
	StripeCustomerID    string                    `bun:",nullzero"`
	SubscriptionID      uuid.UUID                 `bun:",type:uuid,nullzero"`
	Subscription        *StandardUserSubscription `bun:"rel:belongs-to,join:subscription_id=id"`
	SubscriptionStatus  string                    `bun:",notnull,default:'inactive'"`
	SubscriptionStartAt time.Time                 `bun:""`
	TrialStartAt        time.Time                 `bun:""`
	TrialUsed           bool                      `bun:",notnull,default:false"`
	CreatedAt  time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt  time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt  time.Time `bun:",soft_delete,nullzero"`
}

// OrganizationMember links a user to an organization with a role.
type OrganizationMember struct {
	ID             uuid.UUID     `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	OrganizationID uuid.UUID     `bun:",notnull,type:uuid"`
	Organization   *Organization `bun:"rel:belongs-to,join:organization_id=id"`
	UserID         uuid.UUID     `bun:",notnull,type:uuid"`
	User           *User         `bun:"rel:belongs-to,join:user_id=id"`
	Role           string        `bun:",notnull,default:'member'"` // owner, admin, member
	MonthlyCreditCap  *int          `bun:",nullzero"` // NULL = unlimited within pool
	CreatedAt      time.Time     `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time     `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt      time.Time     `bun:",soft_delete,nullzero"`
}

// OrganizationInvite represents a pending invitation to join an organization.
type OrganizationInvite struct {
	ID             uuid.UUID     `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	OrganizationID uuid.UUID     `bun:",notnull,type:uuid"`
	Organization   *Organization `bun:"rel:belongs-to,join:organization_id=id"`
	Email          string        `bun:",notnull"`
	Role           string        `bun:",notnull,default:'member'"`
	InvitedByID    uuid.UUID     `bun:",notnull,type:uuid"`
	InvitedBy      *User         `bun:"rel:belongs-to,join:invited_by_id=id"`
	Token          string        `bun:",unique,notnull"`
	ExpiresAt      time.Time     `bun:",notnull"`
	AcceptedAt     time.Time     `bun:",nullzero"`
	CreatedAt      time.Time     `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
