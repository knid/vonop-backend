package gitrepo

import "context"

// FinalizationLocker is the boundary the runtime finalizer will use before
// moving refs/heads/main and app_projects.current_commit. Production wiring
// should provide a distributed implementation (Postgres advisory lock or
// Redis), not the local project mutex used for filesystem operations.
type FinalizationLocker interface {
	WithProjectFinalizationLock(ctx context.Context, projectID string, fn func(context.Context) error) error
}
