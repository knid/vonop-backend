package controllers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/uptrace/bun"
)

func (c *Controller) GetShipItems(ctx context.Context, user *models.User) (*responses.ShipResponse, error) {
	if user == nil {
		return nil, errors.New("user required")
	}

	var projects []models.AppProject
	if err := c.DB.DB.NewSelect().
		Model(&projects).
		Where("user_id = ?", user.ID).
		Scan(ctx); err != nil {
		return nil, err
	}

	type projInfo struct {
		AppID   string
		AppName string
	}
	// Map keyed by AppProject ID — Build/AppBuildTask carry app_project_id
	// directly (the v1 shadow Project FK was removed 2026-05-07).
	byApp := map[string]projInfo{}
	appIDs := make([]string, 0, len(projects))
	for _, p := range projects {
		info := projInfo{AppID: p.ID.String(), AppName: p.Name}
		byApp[p.ID.String()] = info
		appIDs = append(appIDs, p.ID.String())
	}
	if len(appIDs) == 0 {
		return &responses.ShipResponse{Items: []responses.ShipItem{}}, nil
	}

	since := time.Now().Add(-60 * 24 * time.Hour)

	var tasks []models.AppBuildTask
	if err := c.DB.DB.NewSelect().
		Model(&tasks).
		Relation("Build").
		Where("t.app_project_id IN (?)", bun.In(appIDs)).
		Where("t.created_at > ?", since).
		Order("t.created_at DESC").
		Scan(ctx); err != nil {
		return nil, err
	}

	// Releases — submission tracking (replaces legacy Deployment rows).
	var releases []models.Release
	if err := c.DB.DB.NewSelect().
		Model(&releases).
		Where("app_project_id IN (?)", bun.In(appIDs)).
		Where("created_at > ?", since).
		Where("deleted_at IS NULL").
		Order("created_at DESC").
		Scan(ctx); err != nil {
		return nil, err
	}

	items := make([]responses.ShipItem, 0, len(tasks)+len(releases))

	for _, t := range tasks {
		info, ok := byApp[t.AppProjectID.String()]
		if !ok {
			continue
		}
		status, kind := classifyBuildStatus(t.StatusText, t.Finished, t.ErrorMessage)
		version := ""
		if t.Build != nil {
			if t.Build.VersionString != "" {
				version = t.Build.VersionString
				if t.Build.BuildNumber > 0 {
					version = fmt.Sprintf("%s (Build %d)", version, t.Build.BuildNumber)
				}
			}
		}
		items = append(items, responses.ShipItem{
			ID:         t.ID.String(),
			Kind:       "build",
			AppID:      info.AppID,
			AppName:    info.AppName,
			Platform:   t.Platform,
			Version:    version,
			Status:     status,
			StatusKind: kind,
			CreatedAt:  t.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:  t.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}

	var etaForDeployments int
	if len(releases) > 0 {
		etaForDeployments, _ = database.MedianElapsedSeconds(ctx, c.DB.DB,
			models.DeploymentEventSubmitted,
			models.DeploymentEventStoreLive,
			30)
	}

	for _, r := range releases {
		info, ok := byApp[r.AppProjectID.String()]
		if !ok {
			continue
		}
		store, platform := "", ""
		switch strings.ToLower(r.Platform) {
		case "ios":
			store, platform = "app_store", "ios"
		case "android":
			store, platform = "play_store", "android"
		}
		status, kind := classifyReleaseStatus(r.Status)
		version := r.MarketingVersion
		if version == "" && r.VersionCode > 0 {
			version = fmt.Sprintf("%d", r.VersionCode)
		}
		item := responses.ShipItem{
			ID:         r.ID.String(),
			Kind:       "deployment",
			AppID:      info.AppID,
			AppName:    info.AppName,
			Store:      store,
			Platform:   platform,
			Version:    version,
			Status:     status,
			StatusKind: kind,
			CreatedAt:  r.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:  r.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if kind == "info" && etaForDeployments > 0 {
			item.ETASeconds = etaForDeployments
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt > items[j].UpdatedAt
	})

	return &responses.ShipResponse{Items: items}, nil
}

func classifyBuildStatus(raw string, finished bool, errorMessage string) (string, string) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if errorMessage != "" {
		return "Failed", "danger"
	}
	switch key {
	case "built", "success", "completed", "done":
		return "Built", "success"
	case "error", "failed":
		return "Failed", "danger"
	case "uploading":
		return "Uploading", "info"
	case "building", "reviewing":
		return "Building", "info"
	case "preparing":
		return "Preparing", "info"
	case "queued":
		return "Queued", "neutral"
	case "canceled", "cancelled":
		return "Canceled", "neutral"
	}
	if !finished {
		return "Building", "info"
	}
	return "Done", "neutral"
}

// classifyReleaseStatus maps Release.Status → (human label, kind) for the
// Ship list and Launchpad recent-deployment surfaces. Replaces the legacy
// classifyDeploymentStatus once Deployment rows were retired in favour of
// Release-based submission tracking.
func classifyReleaseStatus(raw string) (string, string) {
	switch raw {
	case models.ReleaseStatusLive:
		return "Live", "success"
	case models.ReleaseStatusInReview:
		return "In review", "info"
	case models.ReleaseStatusUploaded, models.ReleaseStatusApproved, models.ReleaseStatusDraft:
		return "Submitting", "info"
	case models.ReleaseStatusRejected,
		models.ReleaseStatusManuallyRejected,
		models.ReleaseStatusInvalid:
		return "Rejected", "danger"
	case models.ReleaseStatusPaused:
		return "Paused", "warn"
	case models.ReleaseStatusRolledBack:
		return "Rolled back", "warn"
	}
	return raw, "neutral"
}
