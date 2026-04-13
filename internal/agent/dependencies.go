package agent

import (
	"context"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// CreditChecker is a function that returns true while the user still has
// credit budget for the current run. The orchestrator calls it between turns.
type CreditChecker func() bool

// CreditDeductor is called on successful run completion with the total credit
// cost. Never called on failure (fixes a known bug in the old system).
type CreditDeductor func(ctx context.Context, credits int, action, projectID string) error

// EventTracker posts a PostHog event. Matches the shape of the existing
// Controller.TrackUserEvent helper.
type EventTracker func(event string, props map[string]any)

// ImageGenerator generates an image and returns a stable public URL.
// Implementations call the image service, upload bytes to AssetsStorage, and
// return the S3 public URL.
type ImageGenerator func(ctx context.Context, prompt string, w, h int, style, projectID string) (string, error)

// NeonConnResolver returns the Postgres connection string for a project's
// connected Neon database, or "" if no database is connected.
type NeonConnResolver func(projectID string) (string, error)

// MobileBridgeUpdater enables/disables a native mobile bridge method on the
// project's MobileAppConfig row.
type MobileBridgeUpdater func(ctx context.Context, projectID, method string, enabled bool, config map[string]any) error

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
	CreditChecker            CreditChecker
	DeductCredits            CreditDeductor
	TrackUserEvent           EventTracker
	GenerateImage            ImageGenerator
	NeonConnString           NeonConnResolver
	UpdateMobileBridgeMethod MobileBridgeUpdater
}
