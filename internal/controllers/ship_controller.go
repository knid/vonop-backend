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
	// Deployments still carry the legacy v1 project_id, so keep a parallel
	// v1-keyed map for the Deployment join below. A blank entry means we
	// know the AppProject but it never had a v1 shadow row.
	byV1 := map[string]projInfo{}
	v1IDs := make([]string, 0, len(projects))
	for _, p := range projects {
		info := projInfo{AppID: p.ID.String(), AppName: p.Name}
		byApp[p.ID.String()] = info
		appIDs = append(appIDs, p.ID.String())
		if v1, ok := p.Settings["v1_project_id"].(string); ok && v1 != "" {
			byV1[v1] = info
			v1IDs = append(v1IDs, v1)
		}
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

	var deployments []models.Deployment
	if len(v1IDs) > 0 {
		if err := c.DB.DB.NewSelect().
			Model(&deployments).
			Where("project_id IN (?)", bun.In(v1IDs)).
			Where("created_at > ?", since).
			Order("created_at DESC").
			Scan(ctx); err != nil {
			return nil, err
		}
	}

	items := make([]responses.ShipItem, 0, len(tasks)+len(deployments))

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
	if len(deployments) > 0 {
		etaForDeployments, _ = database.MedianElapsedSeconds(ctx, c.DB.DB,
			models.DeploymentEventSubmitted,
			models.DeploymentEventStoreLive,
			30)
	}

	for _, d := range deployments {
		info, ok := byV1[d.ProjectID.String()]
		if !ok {
			continue
		}
		store, platform := "", ""
		for _, p := range d.Platforms {
			pl := strings.ToLower(p)
			if pl == "ios" {
				store, platform = "app_store", "ios"
				break
			}
			if pl == "android" {
				store, platform = "play_store", "android"
				break
			}
		}
		status, kind := classifyDeploymentStatus(d.Status)
		item := responses.ShipItem{
			ID:         d.ID.String(),
			Kind:       "deployment",
			AppID:      info.AppID,
			AppName:    info.AppName,
			Store:      store,
			Platform:   platform,
			Version:    d.Title,
			Status:     status,
			StatusKind: kind,
			CreatedAt:  d.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:  d.UpdatedAt.UTC().Format(time.RFC3339),
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

func classifyDeploymentStatus(raw string) (string, string) {
	key := strings.ToLower(strings.TrimSpace(raw))
	switch key {
	case "deployed", "published", "live":
		return "Live", "success"
	case "submitted", "reviewing", "in_review":
		return "In review", "info"
	case "queued", "deploying":
		return "Submitting", "info"
	case "rejected":
		return "Rejected", "danger"
	case "error", "failed":
		return "Failed", "danger"
	case "pending_connection":
		return "Waiting for connection", "warn"
	}
	return raw, "neutral"
}
