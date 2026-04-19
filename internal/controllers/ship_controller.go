package controllers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

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
	byV1 := map[string]projInfo{}
	v1IDs := make([]string, 0, len(projects))
	for _, p := range projects {
		v1, ok := p.Settings["v1_project_id"].(string)
		if !ok || v1 == "" {
			continue
		}
		byV1[v1] = projInfo{AppID: p.ID.String(), AppName: p.Name}
		v1IDs = append(v1IDs, v1)
	}
	if len(v1IDs) == 0 {
		return &responses.ShipResponse{Items: []responses.ShipItem{}}, nil
	}

	since := time.Now().Add(-60 * 24 * time.Hour)

	var tasks []models.AppBuildTask
	if err := c.DB.DB.NewSelect().
		Model(&tasks).
		Relation("Build").
		Where("t.project_id IN (?)", bun.In(v1IDs)).
		Where("t.created_at > ?", since).
		Order("t.created_at DESC").
		Scan(ctx); err != nil {
		return nil, err
	}

	var deployments []models.Deployment
	if err := c.DB.DB.NewSelect().
		Model(&deployments).
		Where("project_id IN (?)", bun.In(v1IDs)).
		Where("created_at > ?", since).
		Order("created_at DESC").
		Scan(ctx); err != nil {
		return nil, err
	}

	items := make([]responses.ShipItem, 0, len(tasks)+len(deployments))

	for _, t := range tasks {
		info, ok := byV1[t.ProjectID.String()]
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
		items = append(items, responses.ShipItem{
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
		})
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
	}
	return raw, "neutral"
}
