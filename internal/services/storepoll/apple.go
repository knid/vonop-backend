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
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/services/storeauth"
)

type AppleChecker struct {
	DB             *database.Database
	UserKeyStorage *storage.S3Storage
}

// Check resolves the Apple app by bundle id, finds the latest
// appStoreVersion, and maps its appStoreState to a deployment event.
//
// Mapping:
//   IN_REVIEW                                                        → store_review_started
//   READY_FOR_SALE | PENDING_DEVELOPER_RELEASE | PROCESSING_FOR_APP_STORE → store_live
//   REJECTED | METADATA_REJECTED | DEVELOPER_REJECTED                → store_rejected
//
// Other states (PREPARE_FOR_SUBMISSION, WAITING_FOR_REVIEW, …) are
// informational and produce no event. Idempotent via HasEvent.
func (c AppleChecker) Check(ctx context.Context, d models.Deployment) error {
	if d.AppleConnection == nil || d.Project == nil {
		return fmt.Errorf("deployment missing connection or project")
	}

	p8Bytes, err := c.UserKeyStorage.ReadObjectBytes(ctx, d.AppleConnection.P8KeyPath)
	if err != nil {
		return fmt.Errorf("read p8 from storage: %w", err)
	}
	token, err := storeauth.AppleToken(storeauth.AppleConnectionInput{
		KeyID:    d.AppleConnection.KeyID,
		IssuerID: d.AppleConnection.IssuerID,
		P8PEM:    p8Bytes,
	})
	if err != nil {
		return fmt.Errorf("mint apple token: %w", err)
	}

	// Resolve the app by bundle id.
	appsURL := "https://api.appstoreconnect.apple.com/v1/apps?filter[bundleId]=" + url.QueryEscape(d.Project.PackageName) + "&limit=1"
	apps, err := ascGet(ctx, appsURL, token)
	if err != nil {
		return fmt.Errorf("query apps: %w", err)
	}
	if len(apps.Data) == 0 {
		return nil // app not on the store yet — nothing to poll
	}
	appID := apps.Data[0].ID

	// Pull the most recent appStoreVersion (sorted desc by version string).
	versURL := "https://api.appstoreconnect.apple.com/v1/apps/" + appID + "/appStoreVersions?limit=1&sort=-versionString"
	vers, err := ascGet(ctx, versURL, token)
	if err != nil {
		return fmt.Errorf("query versions: %w", err)
	}
	if len(vers.Data) == 0 {
		return nil
	}
	state := vers.Data[0].Attributes.AppStoreState

	var eventType, newStatus string
	switch state {
	case "IN_REVIEW":
		eventType = models.DeploymentEventStoreReviewStarted
		newStatus = "reviewing"
	case "READY_FOR_SALE", "PENDING_DEVELOPER_RELEASE", "PROCESSING_FOR_APP_STORE":
		eventType = models.DeploymentEventStoreLive
		newStatus = "deployed"
	case "REJECTED", "METADATA_REJECTED", "DEVELOPER_REJECTED":
		eventType = models.DeploymentEventStoreRejected
		newStatus = "rejected"
	default:
		return nil
	}

	already, err := database.HasEvent(ctx, c.DB.DB, d.ID, eventType)
	if err != nil {
		return fmt.Errorf("has event check: %w", err)
	}
	if already {
		return nil
	}

	meta := map[string]interface{}{"source": "app_store_connect", "state": state}
	if err := database.AppendDeploymentEvent(ctx, c.DB.DB, d.ID,
		eventType, models.DeploymentEventActorStoreAPI, nil, meta); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	// Denormalize status so Launchpad / ship-table reads don't have to
	// re-derive from events.
	d.Status = newStatus
	d.StatusMessage = "Apple: " + state
	if _, err := c.DB.UpdateDeployment(&d); err != nil {
		return fmt.Errorf("update deployment status: %w", err)
	}
	return nil
}

type ascList struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			AppStoreState string `json:"appStoreState"`
		} `json:"attributes"`
	} `json:"data"`
}

func ascGet(ctx context.Context, u, token string) (*ascList, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asc status %d: %s", resp.StatusCode, string(body))
	}
	out := &ascList{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("parse asc body: %w", err)
	}
	return out, nil
}
