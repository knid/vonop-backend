package agent

import (
	"context"

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
type EventTracker func(event string, props map[string]any)

// CRMEventSender posts a CRM event (Brevo). Used for lifecycle events that
// drive marketing automations (generation completed, chat failed, etc.).
type CRMEventSender func(event string, attrs map[string]any)

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

// MobileBridgeUpdater enables/disables a native mobile bridge method on the
// project's MobileAppConfig row. BridgeMethods is a flat []string of enabled
// method names — enabled=true appends to the slice (idempotent), enabled=false
// removes it.
type MobileBridgeUpdater func(ctx context.Context, projectID, method string, enabled bool) error

// AIEnabler marks a project as needing the Wemob AI Gateway and persists the
// flag so the AIAddendum is appended to subsequent runs. Bound by the
// controller layer to Controller.enableProjectAIForAgent. Idempotent — the
// SetupAIGatewayTool checks the existing flag before invoking this callback.
type AIEnabler func(ctx context.Context, project *models.AppProject) error

// GatewayAllowedModelsFn returns the model IDs (chat + image, OpenRouter /
// fal format) the current user's subscription tier can reach through the
// Wemob AI Gateway. Bound per-run in AgentDeps so the agent package never has
// to resolve tier / remote-config state itself. Used by SetupAIGatewayTool and
// the AI addendum to keep the model list the LLM sees in sync with what the
// gateway will actually accept at runtime.
type GatewayAllowedModelsFn func() []string

// StoreOperator executes one store management operation for an ecommerce
// project. Bound by the controller layer to Controller.ExecuteStoreOp. The
// dispatcher inside the controller routes on `action` to the existing
// *FromChat helpers in ecommerce_controller.go and lazy-creates the Store
// row on first call. Returns a JSON string for list/get actions, a short
// success string for mutations, or a non-nil error on failure.
type StoreOperator func(
	ctx context.Context,
	project *models.AppProject,
	action string,
	params map[string]any,
) (result string, err error)

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
	AssetsStorage *storage.S3Storage
	SecretManager secret.SecretManager
	NeonService   *neon.Service
	LLMProvider   *utils.LLMProvider
	PostHog       posthog.Client
	RemoteConfig  *remoteconfig.Service

	// Callbacks bound per-run by the view/controller layer.
	CreditChecker            CreditCheckerFactory
	DeductCredits            CreditDeductor
	TrackUserEvent           EventTracker
	SendCRMEvent             CRMEventSender
	GenerateImage            ImageGenerator
	NeonConnString           NeonConnResolver
	ProvisionDatabase        DatabaseProvisioner
	EnableAI                 AIEnabler
	GatewayAllowedModels     GatewayAllowedModelsFn
	UpdateMobileBridgeMethod MobileBridgeUpdater
	RecordLLMUsage           LLMUsageRecorder
	RecordLLMError           LLMErrorRecorder
	StoreOp                  StoreOperator
}
