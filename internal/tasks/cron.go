package tasks

const (
	TypeReapStuckAgentRuns  = "cron:reap-stuck-agent-runs"
	TypeReapStuckBuildTasks = "cron:reap-stuck-build-tasks"
	// TypeFlyBandwidthSync runs once per day; the handler walks every live
	// runtime_app, queries Fly's GraphQL billingDataUsage, and aggregates
	// per-org bytes into app_bandwidth_usages. Read by the deploy gate (E3).
	TypeFlyBandwidthSync = "cron:fly-bandwidth-sync"
)
