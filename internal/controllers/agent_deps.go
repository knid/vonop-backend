package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/fal"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// AgentDeps builds a per-run RunDependencies bundle for the agent package.
// Credit lifecycle callbacks are bound to the specific user at call time.
// This method is called once per HTTP chat/generate request from the view layer.
func (c *Controller) AgentDeps(user *models.User) *agent.RunDependencies {
	provider := c.llmProvider()

	return &agent.RunDependencies{
		DB:            c.DB,
		GitRepo:       c.GitRepoService,
		AssetsStorage: c.AssetsStorage,
		SecretManager: c.SecretManager,
		NeonService:   c.NeonService,
		LLMProvider:   provider,
		PostHog:       c.PostHog,
		RemoteConfig:  c.RemoteConfig,

		CreditChecker: func(tracker *utils.TokenTracker) func() bool {
			return c.MakeCreditBudgetChecker(user, tracker)
		},
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

// generateImageForAgent calls Fal to generate an image from a prompt, downloads
// the result, and uploads the bytes to the Assets S3 bucket under the given
// project's images folder. If the S3 upload fails after retries (or the download
// fails), it falls back to returning the temporary Fal CDN URL — it's not
// ideal long-term but the image remains usable for the current session.
//
// Width, height, and style are accepted to satisfy the agent's ImageGenerator
// interface but are not currently passed to the Fal wrapper; see the underlying
// `fal.GenerateImage` for the parameters that actually flow through today.
func (c *Controller) generateImageForAgent(ctx context.Context, prompt string, w, h int, style, projectID string) (string, error) {
	_ = w
	_ = h
	_ = style

	apiKey := c.SecretManager.GetSecret("FAL_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("FAL_API_KEY not configured")
	}
	falModel := c.SecretManager.GetSecret("FAL_IMAGE_MODEL")
	if falModel == "" {
		falModel = "fal-ai/flux/schnell"
	}

	cdnURL, err := fal.GenerateImage(ctx, apiKey, falModel, prompt, "")
	if err != nil {
		return "", fmt.Errorf("fal generate: %w", err)
	}

	imgData, err := downloadImage(ctx, cdnURL)
	if err != nil {
		// Fall back to the CDN URL. It's temporary but works for this session.
		slog.Warn("failed to download generated image, returning CDN URL", "error", err, "project_id", projectID)
		return cdnURL, nil
	}

	filename := fmt.Sprintf("generated-%d.jpg", time.Now().UnixNano())
	s3Key := fmt.Sprintf("apps/%s/images/%s", projectID, filename)

	// Detached context for the S3 upload so the image isn't lost if the
	// caller's context is near its deadline.
	uploadCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var publicURL string
	var uploadErr error
	for attempt := 0; attempt < 3; attempt++ {
		publicURL, uploadErr = c.AssetsStorage.UploadFileBytePublic(uploadCtx, s3Key, imgData, "image/jpeg")
		if uploadErr == nil {
			break
		}
		slog.Warn("S3 upload failed for generated image, retrying",
			"project_id", projectID, "s3_key", s3Key,
			"attempt", attempt+1, "error", uploadErr)
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	if uploadErr != nil {
		return cdnURL, nil
	}
	return publicURL, nil
}

// updateMobileBridgeMethodForAgent toggles a bridge method on a project's
// MobileAppConfig row. BridgeMethods is a []string — enabled=true appends,
// enabled=false removes.
func (c *Controller) updateMobileBridgeMethodForAgent(ctx context.Context, projectID, method string, enabled bool) error {
	cfg, err := c.DB.GetMobileAppConfig(projectID)
	if err != nil || cfg == nil {
		// No config row yet — create one with sensible defaults matching the
		// bun:",default:true" column defaults so the upsert doesn't clobber
		// features the user hasn't opted into toggling.
		parsed, perr := uuid.Parse(projectID)
		if perr != nil {
			return perr
		}
		cfg = &models.MobileAppConfig{
			ProjectID:            parsed,
			PushEnabled:          true,
			AnalyticsEnabled:     true,
			PullToRefreshEnabled: true,
			ExternalBrowserLinks: true,
			BridgeEnabled:        true,
			DarkModeEnabled:      true,
			CachingStrategy:      "network-first",
			RatingSessions:       5,
		}
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
