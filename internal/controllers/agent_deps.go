package controllers

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/models"
	"github.com/posthog/posthog-go"
)

// AgentDeps builds a per-run RunDependencies bundle for the agent package.
// Credit lifecycle callbacks are bound to the specific user at call time.
// This method is called once per HTTP chat/generate request from the view layer.
func (c *Controller) AgentDeps(user *models.User) *agent.RunDependencies {
	provider := c.llmProvider()
	// Credit checker is used by agent.Run's ShouldContinue — tracker is
	// bound by the agent package internally so we pass nil here and the
	// checker walks the context-scoped tracker set up in agent.Run.
	creditChecker := c.MakeCreditBudgetChecker(user, nil)

	return &agent.RunDependencies{
		DB:            c.DB,
		GitRepo:       c.GitRepoService,
		AssetsStorage: c.AssetsStorage,
		SecretManager: c.SecretManager,
		NeonService:   c.NeonService,
		LLMProvider:   provider,
		PostHog:       c.PostHog,
		RemoteConfig:  c.RemoteConfig,

		CreditChecker: creditChecker,
		DeductCredits: func(ctx context.Context, credits int, action, projectID string) error {
			return c.DeductAICreditsToFloor(user, credits, action, projectID)
		},
		TrackUserEvent: func(event string, props map[string]any) {
			p := posthog.NewProperties()
			for k, v := range props {
				p.Set(k, v)
			}
			c.TrackUserEvent(user, event, p)
		},
		GenerateImage: c.generateImageForAgent,
		NeonConnString: func(projectID string) (string, error) {
			return c.neonConnString(projectID), nil
		},
		UpdateMobileBridgeMethod: c.updateMobileBridgeMethodForAgent,
	}
}

// generateImageForAgent is a shim that calls the existing image generation
// path used by the legacy chat tools. It will be replaced when Task C4.4
// rescues the image client into its own controller method; for now it's a
// thin wrapper that returns an error rather than invoking the legacy path,
// which takes different arguments (including a collectedContext builder and
// an SSE sendEvent callback) that don't fit the agent's ImageGenerator
// signature.
//
// Task C4.4 will replace this body with the real Fal + S3 upload.
func (c *Controller) generateImageForAgent(ctx context.Context, prompt string, w, h int, style, projectID string) (string, error) {
	return "", fmt.Errorf("generateImageForAgent: pending C4.4 rescue")
}

// updateMobileBridgeMethodForAgent toggles a bridge method on a project's
// MobileAppConfig row. BridgeMethods is a []string — enabled=true appends,
// enabled=false removes.
func (c *Controller) updateMobileBridgeMethodForAgent(ctx context.Context, projectID, method string, enabled bool) error {
	cfg, err := c.DB.GetMobileAppConfig(projectID)
	if err != nil || cfg == nil {
		// No config row yet — create one with just this method set.
		cfg = &models.MobileAppConfig{}
		parsed, perr := uuid.Parse(projectID)
		if perr != nil {
			return perr
		}
		cfg.ProjectID = parsed
	}
	// Add or remove the method from the flat slice.
	present := false
	kept := cfg.BridgeMethods[:0]
	for _, m := range cfg.BridgeMethods {
		if m == method {
			present = true
			if enabled {
				kept = append(kept, m)
			}
			continue
		}
		kept = append(kept, m)
	}
	if enabled && !present {
		kept = append(kept, method)
	}
	cfg.BridgeMethods = kept
	_, err = c.DB.UpsertMobileAppConfig(cfg)
	return err
}
