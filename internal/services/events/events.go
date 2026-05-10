package events

import (
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
)

// EventService is the single fan-out point for user lifecycle events.
// Today the only adapter is Resend (drives email automations); historically
// Twenty CRM was wired in too but was removed once Resend became the only
// downstream destination.
//
// Fire-and-forget by convention at call sites: callers wrap in `go ...`.
// Implementations swallow and log their own errors; SendEvent returns nil
// in normal operation. The error return is preserved for forward
// compatibility but is not used today.
type EventService interface {
	SendEvent(user *models.User, event constants.EventName, attributes map[string]any) error
}
