package models

import (
	"time"

	"github.com/google/uuid"
)

type AppIntegration struct {
	ID           uuid.UUID         `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID         `bun:",notnull,type:uuid"`
	AppProject   *AppProject       `bun:"rel:belongs-to,join:app_project_id=id"`
	Provider     string            `bun:",notnull"` // "supabase", "neon", "stripe", "iyzico", "fal", "upstash", "resend"
	Credentials  []byte            `bun:",type:jsonb,default:'{}'"` // encrypted credentials
	Config       map[string]any    `bun:",type:jsonb,default:'{}'"`
	Status       string            `bun:",notnull,default:'connected'"` // connected, disconnected, error
	EnvVars      map[string]string `bun:",type:jsonb,default:'{}'"` // env vars to inject into Vercel
	CreatedAt    time.Time         `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time         `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt    time.Time         `bun:",soft_delete,nullzero"`
}
