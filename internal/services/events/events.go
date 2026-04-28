package events

import "github.com/knid/wemob/internal/models"

// EventService is the single fan-out point for user lifecycle events.
// Implementations route events to one or more downstream destinations
// (Twenty CRM, Resend automations, future n8n, etc).
//
// Fire-and-forget by convention at call sites: callers wrap in `go ...`.
// Implementations swallow and log their own errors; SendEvent returns nil
// in normal operation. The error return is preserved for forward
// compatibility but is not used today.
type EventService interface {
	SendEvent(user *models.User, event string, attributes map[string]any) error
}
