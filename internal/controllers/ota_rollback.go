// Package controllers — OTA channel rollback and rollout-promote helpers.
//
// P2#22 (mobile audit, 2026-05-04): the new manifest endpoint filters
// candidates by (channel, is_active=true). To "roll back" to an earlier
// publish we flip is_active on every newer row in the same
// (project, channel, platform) lane, leaving the target row as the only
// active candidate so manifest queries pick it up. To "promote" a partial
// rollout to higher traffic we just bump rollout_percentage on a row.
package controllers

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// ErrOTAChannelMismatch — the target update is in a different channel
// than the one the operator asked to roll back. Mapped to a 409 in the view.
var ErrOTAChannelMismatch = errors.New("ota: target update is in a different channel")

// ErrOTAProjectMismatch — the target update belongs to a different app
// project. Mapped to 404 in the view to avoid leaking row existence.
var ErrOTAProjectMismatch = errors.New("ota: target update does not belong to this app project")

// RollbackOTAUpdate flips is_active=false on all OTAUpdate rows for
// (appProjectID, channel, target.platform) created strictly after target.CreatedAt,
// and re-activates the target row. The target's channel and platform are
// derived from the row, NOT from the caller — so an operator passing a
// targetUpdateID from a different channel is rejected.
//
// Returns the now-active OTAUpdate row.
//
// Idempotent: if target was already active and there's nothing newer,
// this is a no-op that returns the target.
func (c *Controller) RollbackOTAUpdate(
	ctx context.Context,
	appProjectID uuid.UUID,
	channel string,
	targetUpdateID uuid.UUID,
) (*models.OTAUpdate, error) {
	target, err := c.DB.GetOTAUpdateByID(ctx, targetUpdateID)
	if err != nil {
		if database.IsErrNoOTAUpdate(err) {
			return nil, ErrOTANotFound
		}
		return nil, fmt.Errorf("ota: load target: %w", err)
	}
	if target.AppProjectID != appProjectID {
		// Don't leak which app the row actually belongs to.
		return nil, ErrOTAProjectMismatch
	}
	if channel == "" {
		channel = "production"
	}
	if target.Channel != channel {
		return nil, fmt.Errorf("%w: target channel=%q, requested=%q", ErrOTAChannelMismatch, target.Channel, channel)
	}

	// Deactivate everything newer in the same channel/platform lane.
	if _, err := c.DB.DeactivateNewerOTAUpdates(ctx, appProjectID, target.Platform, target.Channel, target); err != nil {
		return nil, fmt.Errorf("ota: deactivate newer: %w", err)
	}

	// Re-activate the target if it was deactivated by some prior rollback.
	if !target.IsActive {
		if err := c.DB.SetOTAUpdateActive(ctx, target.ID, true); err != nil {
			return nil, fmt.Errorf("ota: re-activate target: %w", err)
		}
		target.IsActive = true
	}
	return target, nil
}

// PromoteRollout bumps rollout_percentage on a single OTAUpdate row.
// percentage is clamped to [0, 100]. Returns the row with the new
// percentage applied (re-fetched so the caller sees the persisted value).
//
// 0 ⇒ effectively pause rollout (no device gets it). 100 ⇒ full rollout.
// To rollback, use RollbackOTAUpdate — setting percentage to 0 doesn't
// restore an older publish, it just turns off the new one.
func (c *Controller) PromoteRollout(
	ctx context.Context,
	appProjectID uuid.UUID,
	updateID uuid.UUID,
	newPercentage int16,
) (*models.OTAUpdate, error) {
	if newPercentage < 0 {
		newPercentage = 0
	}
	if newPercentage > 100 {
		newPercentage = 100
	}
	row, err := c.DB.GetOTAUpdateByID(ctx, updateID)
	if err != nil {
		if database.IsErrNoOTAUpdate(err) {
			return nil, ErrOTANotFound
		}
		return nil, fmt.Errorf("ota: load update: %w", err)
	}
	if row.AppProjectID != appProjectID {
		return nil, ErrOTAProjectMismatch
	}
	if err := c.DB.SetOTAUpdateRolloutPercentage(ctx, row.ID, newPercentage); err != nil {
		return nil, fmt.Errorf("ota: set rollout pct: %w", err)
	}
	row.RolloutPercentage = newPercentage
	return row, nil
}
