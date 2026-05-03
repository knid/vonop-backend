package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// AndroidKeystore stores the encrypted reference to a per-project Android
// signing keystore (.jks) and its key.properties file. Generated once by
// Codemagic on the first Android build; reused for all subsequent builds
// of the same project. CredRefs hold no plaintext.
type AndroidKeystore struct {
	bun.BaseModel `bun:"table:android_keystores,alias:ak"`

	ID                uuid.UUID    `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID         uuid.UUID    `bun:",notnull,type:uuid,unique"`
	OrganizationID    uuid.UUID    `bun:",notnull,type:uuid"`
	KeystoreCredRef   *CredRefJSON `bun:"keystore_cred_ref,type:jsonb,nullzero"`
	PropertiesCredRef *CredRefJSON `bun:"properties_cred_ref,type:jsonb,nullzero"`
	CreatedAt         time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt         time.Time    `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt         time.Time    `bun:",soft_delete,nullzero"`
}
