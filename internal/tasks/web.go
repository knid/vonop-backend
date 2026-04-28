package tasks

const (
	TypeDeployWeb = "deploy:web"
)

// WebDeployPayload carries only an ID — the worker re-reads project state,
// env vars, and plan limits from the DB at execution time, so a task picked
// up minutes after enqueue never operates on stale data.
type WebDeployPayload struct {
	DeploymentID string `json:"deployment_id"`
}
