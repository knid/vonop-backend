package responses

import (
	"time"

	"github.com/google/uuid"
)

type OrganizationResponse struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	Slug               string    `json:"slug"`
	Logo               string    `json:"logo,omitempty"`
	OwnerID            uuid.UUID `json:"owner_id"`
	SubscriptionStatus string    `json:"subscription_status"`
	CreatedAt          time.Time `json:"created_at"`
}

type OrganizationMemberResponse struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	UserName    string    `json:"user_name"`
	UserEmail   string    `json:"user_email"`
	Role        string    `json:"role"`
	Permissions []string  `json:"permissions"` // effective (or empty -> UI should derive via role defaults)
	CreatedAt   time.Time `json:"created_at"`
}

type OrganizationInviteResponse struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	Role        string    `json:"role"`
	Permissions []string  `json:"permissions"` // invited-with (empty -> derived from Role at accept time)
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}
