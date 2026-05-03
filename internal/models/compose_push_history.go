package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// ComposePushHistory is the audit log of every push notification sent from the
// dashboard's compose-push UI for a mobile_app_native project. One row per
// successful OneSignal API call. Failed sends are NOT recorded — the controller
// returns the OneSignal error and the dashboard surfaces it as a toast.
//
// AudienceType is "segments" or "external_ids".
// AudienceIDs holds the segment names OR external user IDs depending on AudienceType.
//
// OrganizationID is stamped at insert time so org-admin queries don't need a
// project-table join. SenderUserID is the dashboard user who hit Send (the
// audit-log "who" field for T8.4).
type ComposePushHistory struct {
	bun.BaseModel `bun:"table:compose_push_histories,alias:cph"`

	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID   uuid.UUID `bun:",notnull,type:uuid" json:"app_project_id"`
	OrganizationID uuid.UUID `bun:",notnull,type:uuid" json:"organization_id"`
	SenderUserID   uuid.UUID `bun:",notnull,type:uuid" json:"sender_user_id"`

	Title string `bun:",notnull" json:"title"`
	Body  string `bun:",notnull" json:"body"`

	AudienceType string   `bun:",notnull" json:"audience_type"` // "segments" | "external_ids"
	AudienceIDs  []string `bun:",array,type:text[]" json:"audience_ids"`

	OneSignalNotificationID string `bun:",nullzero" json:"onesignal_notification_id"`
	RecipientsCount         int    `bun:",notnull,default:0" json:"recipients_count"`

	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
}
