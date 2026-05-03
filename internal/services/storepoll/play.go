package storepoll

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/storeauth"
)

type PlayChecker struct {
	DB *database.Database
}

// Check opens a Play Publishing edit for the package, reads the
// production track's releases, and maps a "completed" release to
// store_live. Play does not surface review-in-progress status via a
// public API, so store_review_started is not emitted on Android.
//
// Edits are ephemeral objects; we open, read, and deleteEdit in one
// pass (no commit, so we don't accidentally mutate user state).
func (c PlayChecker) Check(ctx context.Context, d models.Deployment, saBytes []byte) error {
	if d.GooglePlayConnection == nil || d.Project == nil {
		return fmt.Errorf("deployment missing connection or project")
	}
	sa, err := storeauth.ParseServiceAccount(saBytes)
	if err != nil {
		return fmt.Errorf("parse service account: %w", err)
	}
	token, err := storeauth.GoogleToken(ctx, sa)
	if err != nil {
		return fmt.Errorf("mint google token: %w", err)
	}

	pkg := url.PathEscape(d.Project.PackageName)

	// Open an edit (insert with empty body returns an editId).
	editURL := "https://androidpublisher.googleapis.com/androidpublisher/v3/applications/" + pkg + "/edits"
	editReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, editURL, nil)
	editReq.Header.Set("Authorization", "Bearer "+token)
	editReq.Header.Set("Content-Type", "application/json")
	editResp, err := http.DefaultClient.Do(editReq)
	if err != nil {
		return fmt.Errorf("open edit: %w", err)
	}
	defer editResp.Body.Close()
	editBody, _ := io.ReadAll(io.LimitReader(editResp.Body, 4096))
	if editResp.StatusCode != http.StatusOK {
		return fmt.Errorf("open edit status %d: %s", editResp.StatusCode, string(editBody))
	}
	var edit struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(editBody, &edit); err != nil || edit.Id == "" {
		return fmt.Errorf("parse edit response: %w", err)
	}

	// Always clean up the edit, success or failure.
	defer func() {
		delURL := editURL + "/" + edit.Id
		delReq, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, delURL, nil)
		delReq.Header.Set("Authorization", "Bearer "+token)
		delResp, err := http.DefaultClient.Do(delReq)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, delResp.Body)
		delResp.Body.Close()
	}()

	// Read the production track.
	trackURL := editURL + "/" + edit.Id + "/tracks/production"
	trackReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, trackURL, nil)
	trackReq.Header.Set("Authorization", "Bearer "+token)
	trackResp, err := http.DefaultClient.Do(trackReq)
	if err != nil {
		return fmt.Errorf("read track: %w", err)
	}
	defer trackResp.Body.Close()
	trackBody, _ := io.ReadAll(io.LimitReader(trackResp.Body, 16*1024))
	if trackResp.StatusCode == http.StatusNotFound {
		return nil // no production release yet
	}
	if trackResp.StatusCode != http.StatusOK {
		return fmt.Errorf("read track status %d: %s", trackResp.StatusCode, string(trackBody))
	}
	var track struct {
		Releases []struct {
			Status string `json:"status"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(trackBody, &track); err != nil {
		return fmt.Errorf("parse track: %w", err)
	}
	var completed bool
	for _, r := range track.Releases {
		if r.Status == "completed" {
			completed = true
			break
		}
	}
	if !completed {
		return nil
	}

	already, err := database.HasEvent(ctx, c.DB.DB, d.ID, models.DeploymentEventStoreLive)
	if err != nil {
		return fmt.Errorf("has event check: %w", err)
	}
	if already {
		return nil
	}

	meta := map[string]interface{}{"source": "play_publishing", "track": "production"}
	if err := database.AppendDeploymentEvent(ctx, c.DB.DB, d.ID,
		models.DeploymentEventStoreLive, models.DeploymentEventActorStoreAPI, nil, meta); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	d.Status = "deployed"
	d.StatusMessage = "Google Play: completed"
	if _, err := c.DB.UpdateDeployment(&d); err != nil {
		return fmt.Errorf("update deployment status: %w", err)
	}
	return nil
}
