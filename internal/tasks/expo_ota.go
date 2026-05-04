package tasks

const (
	TypeExpoOTAPublish = "expo:ota_publish"
)

// ExpoOTAPublishPayload carries only an ID — the worker re-reads
// project + repo state at execution time so a task picked up minutes
// after enqueue never operates on a stale tree.
type ExpoOTAPublishPayload struct {
	DeploymentID string `json:"deployment_id"`
}
