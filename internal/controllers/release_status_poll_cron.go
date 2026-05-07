package controllers

import (
	"context"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/storeauth"
)

// ReleaseStatusPoller periodically polls App Store Connect + Google Play
// for state transitions on Releases that haven't yet hit a terminal status.
// Replaces the lie that "Codemagic finished uploading == live" by writing
// the real status into the Release row.
//
// Cadence: 60s by default (the spec calls for once-per-minute). Apple has
// generous rate limits on /v1/appStoreVersions; Play opens an edit per call
// so we keep the iteration cap modest.
type ReleaseStatusPoller struct {
	Controller *Controller
	// PollEvery defaults to 60s when zero.
	PollEvery time.Duration
	// MaxPerTick caps how many releases we touch per tick — keeps Play edit
	// churn bounded under high load. Defaults to 50.
	MaxPerTick int
}

// Run blocks until ctx is cancelled. Per-release errors are logged and
// skipped — the loop carries on so a single bad row doesn't block the rest.
func (p *ReleaseStatusPoller) Run(ctx context.Context) {
	if p.PollEvery <= 0 {
		p.PollEvery = 60 * time.Second
	}
	if p.MaxPerTick <= 0 {
		p.MaxPerTick = 50
	}
	t := time.NewTicker(p.PollEvery)
	defer t.Stop()
	p.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

func (p *ReleaseStatusPoller) tick(ctx context.Context) {
	releases, err := p.Controller.DB.ListReleasesInNonTerminalStatus(ctx)
	if err != nil {
		slog.Warn("release status poll: list non-terminal releases failed", "err", err)
		return
	}
	if len(releases) == 0 {
		return
	}
	if len(releases) > p.MaxPerTick {
		releases = releases[:p.MaxPerTick]
	}
	slog.Debug("release status poll: ticking", "count", len(releases))
	for i := range releases {
		r := releases[i]
		switch r.Platform {
		case models.ReleasePlatformIOS:
			if err := p.checkApple(ctx, &r); err != nil {
				slog.Warn("release status poll: apple check failed",
					"release_id", r.ID, "err", err)
			}
		case models.ReleasePlatformAndroid:
			if err := p.checkPlay(ctx, &r); err != nil {
				slog.Warn("release status poll: play check failed",
					"release_id", r.ID, "err", err)
			}
		}
	}
}

func (p *ReleaseStatusPoller) checkApple(ctx context.Context, r *models.Release) error {
	if r.StoreReleaseID == "" {
		return nil // submitted but the appStoreVersionId hasn't been recorded yet
	}
	project, err := p.Controller.DB.GetAppProjectByID(r.AppProjectID.String())
	if err != nil || project == nil {
		return err
	}
	if project.DefaultAppleConnectionID == nil {
		return nil
	}
	conn, err := p.Controller.DB.GetAppleConnectionByOrg(ctx, project.OrganizationID, *project.DefaultAppleConnectionID)
	if err != nil || conn == nil {
		return err
	}
	p8Bytes, err := p.Controller.ReadApplePrimaryP8(ctx, conn)
	if err != nil {
		return err
	}
	in := storeauth.AppleConnectionInput{
		KeyID:    conn.KeyID,
		IssuerID: conn.IssuerID,
		P8PEM:    p8Bytes,
	}
	asv, err := storeauth.FetchAppStoreVersion(ctx, in, r.StoreReleaseID)
	if err != nil {
		return err
	}
	target, msg := mapAppleStateToReleaseStatus(asv.AppStoreState, "", "")
	if target == "" || target == r.Status {
		return nil
	}
	_, err = p.Controller.transitionRelease(ctx, r.ID, target, msg, time.Now().UTC())
	return err
}

func (p *ReleaseStatusPoller) checkPlay(ctx context.Context, r *models.Release) error {
	project, err := p.Controller.DB.GetAppProjectByID(r.AppProjectID.String())
	if err != nil || project == nil {
		return err
	}
	if project.DefaultGoogleConnectionID == nil {
		return nil
	}
	conn, err := p.Controller.DB.GetGooglePlayConnectionByOrg(ctx, project.OrganizationID, *project.DefaultGoogleConnectionID)
	if err != nil || conn == nil {
		return err
	}
	saBytes, err := p.Controller.ReadGoogleSA(ctx, conn)
	if err != nil {
		return err
	}
	sa, err := storeauth.ParseServiceAccount(saBytes)
	if err != nil {
		return err
	}
	rel, err := storeauth.GetPlayTrackRelease(ctx, sa, bundleIDForProject(project, ""), r.Track)
	if err != nil {
		return err
	}
	if rel == nil {
		return nil
	}
	// Update the StoreReleaseID on first sight if it wasn't set.
	if r.StoreReleaseID == "" && rel.StoreReleaseRef != "" {
		r.StoreReleaseID = rel.StoreReleaseRef
		if _, err := p.Controller.DB.UpsertRelease(ctx, r); err != nil {
			slog.Warn("release status poll: persist play store ref failed",
				"release_id", r.ID, "err", err)
		}
	}
	target, msg := MapPlayTrackStatusToReleaseStatus(rel.Status)
	if target == "" || target == r.Status {
		return nil
	}
	_, err = p.Controller.transitionRelease(ctx, r.ID, target, msg, time.Now().UTC())
	return err
}

