package models

import (
	"time"

	"github.com/google/uuid"
)

type AppDomain struct {
	ID             uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID   uuid.UUID   `bun:",notnull,type:uuid"`
	AppProject     *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	Domain         string      `bun:",notnull"`
	Status         string      `bun:",notnull,default:'pending'"` // pending, active, misconfigured, failed
	// DNS verification status. Empty = not yet checked, "pending" = waiting for
	// DNS records to point at the edge, "verified" = ready for cert issuance,
	// "failed" = give up after 30min.
	DNSCheckStatus string `bun:",notnull,default:''"`
	// JSON blob of DNS records the user must add (A, AAAA, CNAME instructions).
	// Surfaced in the UI alongside dns_check_status='pending'.
	DNSInstructions map[string]any `bun:",type:jsonb,notnull,default:'{}'"`
	DNSRecord       string         `bun:",nullzero"` // CNAME value to point to
	CreatedAt       time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt      time.Time   `bun:",soft_delete,nullzero"`
}
