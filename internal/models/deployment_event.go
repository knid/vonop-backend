package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Event types recorded on a Deployment. Strings (not Go enum) so that
// new event types can be added without a migration. The set below
// covers the Phase 5 timeline design; we may add more later.
const (
	DeploymentEventSubmitted          = "submitted"
	DeploymentEventTriaged            = "triaged"            // managed track: human picked it up
	DeploymentEventUploadedToStore    = "uploaded_to_store"
	DeploymentEventStoreReviewStarted = "store_review_started"
	DeploymentEventStoreLive          = "store_live"
	DeploymentEventStoreRejected      = "store_rejected"
	DeploymentEventCanceled           = "canceled"
	DeploymentEventFailed             = "failed"
)

// Actor type — who caused the event.
const (
	DeploymentEventActorUser     = "user"      // the project owner
	DeploymentEventActorWemob    = "wemob"     // Wemob team member (managed track)
	DeploymentEventActorSystem   = "system"    // automated (Codemagic webhook, poll worker)
	DeploymentEventActorStoreAPI = "store_api" // Apple/Google API pushed a state change
)

// DeploymentEvent is an append-only log entry for a deployment.
// Timeline views are rendered from the ordered event stream, not from
// a derived "status" field.
type DeploymentEvent struct {
	bun.BaseModel `bun:"table:deployment_events,alias:de"`

	ID           uuid.UUID              `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	DeploymentID uuid.UUID              `bun:",notnull,type:uuid" json:"deployment_id"`
	EventType    string                 `bun:",notnull" json:"event_type"`
	ActorType    string                 `bun:",notnull" json:"actor_type"`
	ActorID      *uuid.UUID             `bun:",type:uuid,nullzero" json:"actor_id,omitempty"`
	Metadata     map[string]interface{} `bun:",type:jsonb,default:'{}'" json:"metadata"`
	OccurredAt   time.Time              `bun:",notnull,default:current_timestamp" json:"occurred_at"`
}
