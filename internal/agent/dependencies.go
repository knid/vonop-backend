package agent

import (
	"context"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// CreditCheckerFactory is bound by the controller with the user, and called by
// the agent once the TokenTracker exists. The returned func is used as the
// per-round ShouldContinue budget check.
type CreditCheckerFactory func(tracker *utils.TokenTracker) func() bool

// CreditDeductor is called on successful run completion with the total credit
// cost. Never called on failure (fixes a known bug in the old system).
type CreditDeductor func(ctx context.Context, credits int, action, projectID string) error

// EventTracker posts a PostHog event. Matches the shape of the existing
// Controller.TrackUserEvent helper.
type EventTracker func(event constants.EventName, props map[string]any)

// ResendEventSender posts a lifecycle event to Resend so an email
// automation can match it (generation completed, chat failed, etc.).
type ResendEventSender func(event constants.EventName, attrs map[string]any)

// ImageGenerator generates an image and returns a stable public URL.
// Implementations call the image service, upload bytes to AssetsStorage, and
// return the S3 public URL.
type ImageGenerator func(ctx context.Context, prompt string, w, h int, style, projectID string) (string, error)

// NeonConnResolver returns the Postgres connection string for a project's
// connected Neon database, or "" if no database is connected.
type NeonConnResolver func(projectID string) (string, error)

// DatabaseProvisioner creates the project's wemob db (Neon under the hood)
// with the given table schema, stores the connection as an AppIntegration,
// flips project.Settings["has_database"] = true, and persists the project.
// Bound by the controller layer to Controller.provisionDatabaseForAgent.
// Idempotent at the tool layer — the ProvisionDatabaseTool checks for an
// existing connection before invoking this callback.
type DatabaseProvisioner func(ctx context.Context, project *models.AppProject, tables []models.DataModel) error

// AIEnabler marks a project as needing the Wemob AI Gateway and persists the
// flag so the AIAddendum is appended to subsequent runs. Bound by the
// controller layer to Controller.enableProjectAIForAgent. Idempotent — the
// SetupAIGatewayTool checks the existing flag before invoking this callback.
type AIEnabler func(ctx context.Context, project *models.AppProject) error

// LocalDatabaseEnabler marks a project as having an on-device SQLite database
// wired (mobile/lib/db.ts + schema + migrations) and persists the flag so
// the LocalDatabaseAddendum is appended to subsequent runs. Bound by the
// controller layer to Controller.enableProjectLocalDatabaseForAgent.
// Idempotent at the tool layer — ProvisionLocalDatabaseTool checks the
// flag before invoking this callback.
type LocalDatabaseEnabler func(ctx context.Context, project *models.AppProject) error

// BackendAutoDeployer queues a Fly deploy for the project's backend/ subtree
// when the agent run touched it. mobile_app_native projects rely on this to
// avoid the "user describes an AI feature but nothing works because they
// didn't click Deploy" UX gap — the agent commits the backend code AND
// queues the deploy in one shot. Bound by the controller layer to
// Controller.autoDeployBackendForAgent. The agent runner calls this AFTER
// FinaliseRun so the just-committed code is what ships.
//
// Best-effort by design: implementations should swallow ErrDeployInProgress
// (a manual deploy was already running) and log other errors. The chat run
// must not fail because a background deploy failed to enqueue.
type BackendAutoDeployer func(ctx context.Context, project *models.AppProject) error

// MobileBuildTrigger kicks off a fresh Expo (mobile_app_native) build for
// the project, on behalf of the chat agent. Used by trigger_build_after_fix
// so the agent can close the read-fix-rebuild loop without forcing the
// user back to the build tab. Returns the build ID + the per-platform
// task IDs so the tool result can include status URLs.
//
// platforms is a (possibly empty) slice of "android" / "ios". Empty means
// "rebuild whatever the last build targeted" — the controller layer
// resolves this from MobileAppConfig.BuildFor* flags.
//
// Best-effort by design: implementations must NOT panic on partial
// failures (single-platform trigger errors). The tool reports the status
// to the model so it can react.
type MobileBuildTrigger func(ctx context.Context, project *models.AppProject, user *models.User, platforms []string) (buildID string, taskIDs map[string]string, err error)

// WebDeployTrigger queues a fresh Fly web deploy for the project, on behalf
// of the chat agent. Used by trigger_web_deploy so the agent can close the
// read-fix-redeploy loop after diagnosing a failed deployment. Returns the
// new deployment ID so the tool result can include a status URL.
//
// Best-effort by design: implementations must swallow ErrDeployInProgress
// (a manual deploy was already running) and log other errors. The chat turn
// must not fail because a background deploy failed to enqueue.
type WebDeployTrigger func(ctx context.Context, project *models.AppProject, user *models.User) (deploymentID string, err error)

// GatewayAllowedModelsFn returns the model IDs (chat + image, OpenRouter /
// fal format) the current user's subscription tier can reach through the
// Wemob AI Gateway. Bound per-run in AgentDeps so the agent package never has
// to resolve tier / remote-config state itself. Used by SetupAIGatewayTool and
// the AI addendum to keep the model list the LLM sees in sync with what the
// gateway will actually accept at runtime.
type GatewayAllowedModelsFn func() []string

// LLMUsageRecorder is invoked at the end of a successful run (and at the end
// of a partial-failure run that still made LLM calls) to record per-call
// $ai_generation events plus a grouping $ai_trace event in PostHog LLM
// Analytics. Bound by the controller layer to Controller.RecordTokenUsage.
type LLMUsageRecorder func(
	tracker *utils.TokenTracker,
	action, model, provider, reference, appType string,
)

// LLMErrorRecorder is invoked when an agent run fails. Sends a final
// $ai_generation event tagged with $ai_is_error=true so the LLM Analytics
// dashboard surfaces the failure. The tracker carries TraceID/RunID so the
// error event links to the same PostHog trace as the run's $ai_generation
// events and can be pivoted to the AppAgentRun row. Bound by the controller
// layer to Controller.RecordLLMError.
type LLMErrorRecorder func(
	tracker *utils.TokenTracker,
	action, model, provider, reference, appType string,
	latencyMs int64,
	errCode, errMsg string,
)

// RunDependencies holds the external services the agent package needs.
// Constructed from *controllers.Controller once per request via
// Controller.AgentDeps(). The agent package never reaches into Controller
// directly — everything goes through this struct.
type RunDependencies struct {
	DB            *database.Database
	GitRepo       *gitrepo.Service

	// mobileConfigFetcher overrides DB for GetMobileAppConfig lookups. Used in
	// tests to inject a fake without a real database connection. When nil,
	// context.go falls back to DB directly.
	mobileConfigFetcher mobileConfigGetter
	AssetsStorage *storage.S3Storage
	SecretManager secret.SecretManager
	NeonService   *neon.Service
	LLMProvider   *utils.LLMProvider
	PostHog       posthog.Client
	RemoteConfig  *remoteconfig.Service

	// UserPlanName is the lowercased subscription plan (starter, hobby, pro, max).
	// Used by creditsBilled to resolve per-plan AI credit formula parameters.
	UserPlanName string

	// Callbacks bound per-run by the view/controller layer.
	CreditChecker            CreditCheckerFactory
	DeductCredits            CreditDeductor
	TrackUserEvent           EventTracker
	SendResendEvent          ResendEventSender
	GenerateImage            ImageGenerator
	NeonConnString           NeonConnResolver
	ProvisionDatabase        DatabaseProvisioner
	EnableAI                 AIEnabler
	EnableLocalDatabase      LocalDatabaseEnabler
	AutoDeployBackend        BackendAutoDeployer
	GatewayAllowedModels GatewayAllowedModelsFn
	TriggerMobileBuild   MobileBuildTrigger
	TriggerWebDeploy     WebDeployTrigger
	RecordLLMUsage       LLMUsageRecorder
	RecordLLMError       LLMErrorRecorder
}
