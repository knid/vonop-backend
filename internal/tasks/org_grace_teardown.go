package tasks

import "github.com/google/uuid"

// TypeOrgGraceTeardown runs OrgGraceTeardownDelay after a Stripe
// customer.subscription.deleted event. The handler walks every app project in
// the org and tears down its Fly app + Neon project + Gitea repo + git repo +
// asset blobs. If the user re-subscribes before the grace window closes, the
// scheduled task is deleted via TaskID lookup so no teardown happens.
const TypeOrgGraceTeardown = "billing:org-grace-teardown"

type OrgGraceTeardownPayload struct {
	OrgID uuid.UUID `json:"org_id"`
}
