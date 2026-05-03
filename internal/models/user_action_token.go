package models

import (
	"time"

	"github.com/google/uuid"
)

// UserActionTokenKind identifies what a one-time token is for.
// Stored as a text column so new kinds can be added without a migration.
type UserActionTokenKind string

const (
	UserActionTokenKindPasswordReset    UserActionTokenKind = "password_reset"
	UserActionTokenKindVerifyEmail      UserActionTokenKind = "verify_email"
	UserActionTokenKindKeystoreDownload UserActionTokenKind = "keystore_download"
)

// UserActionToken represents a short-lived, single-use token for
// password resets and email verification. These are deliberately kept
// separate from the auth `Token` model, which is long-lived (14 days)
// and tracks active sessions.
type UserActionToken struct {
	ID         uuid.UUID           `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID     uuid.UUID           `bun:",notnull,type:uuid"`
	User       *User               `bun:"rel:belongs-to,join:user_id=id"`
	Kind       UserActionTokenKind `bun:",notnull"`
	Token      string              `bun:",notnull,unique"`
	ExpiredAt  time.Time           `bun:",notnull"`
	ConsumedAt time.Time           `bun:",nullzero"`
	CreatedAt  time.Time           `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// IsExpired reports whether the token has passed its expiration.
func (t *UserActionToken) IsExpired() bool {
	return time.Now().After(t.ExpiredAt)
}

// IsConsumed reports whether the token has already been used.
func (t *UserActionToken) IsConsumed() bool {
	return !t.ConsumedAt.IsZero()
}
