package responses

type DeploymentEventItem struct {
	ID         string `json:"id"`
	EventType  string `json:"event_type"`
	ActorType  string `json:"actor_type"`
	ActorName  string `json:"actor_name,omitempty"`
	Note       string `json:"note,omitempty"`
	OccurredAt string `json:"occurred_at"`
}

type DeploymentEventsResponse struct {
	Items []DeploymentEventItem `json:"items"`
}
