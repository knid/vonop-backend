package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// DraftListing generates a single drafted field (title / description /
// release_notes) for an app's store listing. Uses the existing
// GetJSONFromLLM helper with the same token-tracking + billing pattern
// as NavBarAutoFillWithAI.
func (c *Controller) DraftListing(ctx context.Context, user *models.User, appID uuid.UUID, req requests.DraftListingRequest) (*responses.ListingDraftResponse, error) {
	if user == nil {
		return nil, errors.New("user required")
	}

	kind := strings.TrimSpace(req.Kind)
	target := strings.TrimSpace(req.Target)
	if kind != "title" && kind != "description" && kind != "release_notes" {
		return nil, fmt.Errorf("invalid kind %q", kind)
	}
	if target != "app_store" && target != "play_store" {
		return nil, fmt.Errorf("invalid target %q", target)
	}

	// Load the app for name + description.
	var project models.AppProject
	if err := c.DB.DB.NewSelect().
		Model(&project).
		Where("id = ?", appID).
		Scan(ctx); err != nil {
		return nil, err
	}
	if err := c.AuthorizeProjectAccess(user, &project); err != nil {
		return nil, errors.New("not authorized")
	}

	// Compose the user message as a compact JSON payload.
	userPayload := map[string]string{
		"app_name":        project.Name,
		"app_description": strings.TrimSpace(project.Prompt),
		"target":          target,
		"kind":            kind,
		"hint":            strings.TrimSpace(req.Hint),
	}
	userJSON, _ := json.Marshal(userPayload)

	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	tracker.ProjectID = project.ID.String()
	tracker.TraceID = uuid.New().String()
	tracker.TrackContent = true
	tracker.TraceInput = string(userJSON)
	if project.OrganizationID != uuid.Nil {
		tracker.OrganizationID = project.OrganizationID.String()
	}
	ctx = utils.WithTokenTracker(ctx, tracker)
	provider := c.LLMProvider()
	model := provider.FastModel()

	raw, err := utils.GetJSONFromLLM(
		ctx,
		model,
		constants.LISTING_DRAFT_PROMPT,
		string(userJSON),
		provider,
	)
	if err != nil {
		return nil, fmt.Errorf("listing draft LLM call failed: %w", err)
	}
	tracker.TraceOutput = raw

	var out responses.ListingDraftResponse
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("listing draft LLM returned invalid JSON: %w", err)
	}
	if out.Kind == "" {
		out.Kind = kind
	}

	c.RecordTokenUsage(user, "listing_draft",
		model,
		provider.Name, project.ID.String(), "standard", tracker)
	_, _, total, _ := tracker.Totals()
	if total > 0 {
		credits := c.CreditsForTracker(tracker)
		orgID, orgErr := c.ResolveOrgForProjectID(user, project.ID.String())
		if orgErr != nil {
			slog.Warn("listing_draft: resolve org failed", "err", orgErr)
		} else if err := c.DeductAICreditsUnchecked(user, orgID, credits, "listing_draft", project.ID.String()); err != nil {
			slog.Warn("listing_draft: deduct credits failed", "err", err)
		}
	}

	return &out, nil
}
