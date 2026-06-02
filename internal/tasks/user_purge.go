package tasks

import "github.com/google/uuid"

// TypeUserAccountPurge is enqueued when a user deletes their account (self-service
// via DELETE /v2/users/me or admin delete). The handler walks every AppProject
// and legacy Project owned by the user, runs full infra teardown (Neon, Fly via
// enqueue, git, gitea, S3), deletes the project/org rows, removes user-scoped
// data (tokens, notifications), and hard-deletes the user record for complete
// removal. Uses the "cron" queue to match other teardown tasks (low priority,
// best-effort external calls).
const TypeUserAccountPurge = "user:account-purge"

type UserAccountPurgePayload struct {
	UserID uuid.UUID `json:"user_id"`
}
