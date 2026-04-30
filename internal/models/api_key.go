package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// APIKey represents a programmatic access key for the API.
type APIKey struct {
	ID             uuid.UUID     `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID         uuid.UUID     `bun:",notnull,type:uuid"`
	User           *User         `bun:"rel:belongs-to,join:user_id=id"`
	OrganizationID uuid.UUID     `bun:",type:uuid,nullzero"` // becomes notnull in Phase 6
	Organization   *Organization `bun:"rel:belongs-to,join:organization_id=id"`
	Name           string        `bun:",notnull"`
	KeyHash        string        `bun:",notnull"` // SHA-256 hash of the key (never store raw key)
	KeyPrefix      string        `bun:",notnull"` // first 8 chars of the key for identification
	Scopes         string        `bun:",notnull"` // comma-separated scopes: "apps:read,apps:write,builds:create"
	LastUsed       time.Time     `bun:",nullzero"`
	ExpiresAt      time.Time     `bun:",nullzero"` // zero = never expires
	CreatedAt      time.Time     `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt      time.Time     `bun:",soft_delete,nullzero"`
}

// HasScope returns true if the API key has the given scope.
func (k *APIKey) HasScope(scope string) bool {
	for _, s := range strings.Split(k.Scopes, ",") {
		if strings.TrimSpace(s) == scope {
			return true
		}
	}
	return false
}
