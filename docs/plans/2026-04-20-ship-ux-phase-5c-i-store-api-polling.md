# Ship UX — Phase 5c-i · Store-API Polling Worker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the "what's the store doing right now" information gap by polling App Store Connect + Google Play Publishing on a schedule and writing `store_review_started`, `store_live`, and `store_rejected` events with `actor_type = "store_api"`. Until now these events only arrived when the flutter-template's Codemagic workflow reported them, which meant a deployment that Apple approved at 3am sat in "submitted" until someone reloaded the page or a new build hit. After this phase, the timeline auto-advances.

**Architecture:** A new `internal/services/storepoll/` package. A `Poller` runs on a ticker, pulls in-flight deployments, buckets them by platform, and invokes an `AppleChecker` or `PlayChecker` per deployment. Each checker re-uses credential decoding from the existing Phase 3a validation code (refactored into `internal/services/storeauth/`). When a checker detects a state transition that hasn't already been recorded, it appends an event and updates `deployment.status` for denormalized dashboard reads. Goroutine lives in `cmd/main.go` next to the preview-code cleanup ticker.

**Tech stack:** Go 1.22+, Bun, golang-jwt/jwt/v5, standard library HTTP.

**Design spec:** `docs/design/2026-04-18-wemob-publish-ux-redesign-design.md` ("Phase 5 · Cold start + Managed polish").

---

## Test strategy

Same discipline as prior phases: `go build ./...` + `go vet ./...` clean; runtime smoke documented for user. No _test.go files in the project by policy.

## File structure overview

### New

```
internal/services/storeauth/apple.go   — AppleToken(ctx, conn, p8Bytes) (string, error)
internal/services/storeauth/google.go  — GoogleToken(ctx, saBytes) (string, error)
internal/services/storepoll/poller.go  — Poller orchestration
internal/services/storepoll/apple.go   — AppleChecker: one deployment → event(s)
internal/services/storepoll/play.go    — PlayChecker: one deployment → event(s)
```

### Modified

```
internal/controllers/connection_validation.go  — delegate JWT/OAuth to storeauth helpers
internal/database/deployment_events.go         — add HasEvent helper
internal/database/deployments.go               — add ListInFlightDeployments
internal/controllers/controller.go             — wire storepoll.Poller
cmd/main.go                                     — periodic goroutine
```

## Why these boundaries

- **`storeauth` is infrastructure, not business logic.** Validation (controller) and polling (service) both need App Store Connect JWT + Google OAuth2 tokens. Extracting this now prevents drift between the two code paths — a bug fixed in polling should fix validation too.
- **Poller has no ambient state.** It takes a `*database.Database` and two HTTP clients; no controller pointer. That makes it trivially stoppable at shutdown.
- **Checkers are pure "compute events from one deployment's current store state."** They append events via the existing `database.AppendDeploymentEvent` after a `HasEvent` dedup check. No side effects beyond events + `deployment.status` denormalization.
- **Periodic goroutine, not Asynq.** The polling work is read-only to the stores, already-idempotent via dedup, and cheap (dozens of requests per tick). Adopting Asynq adds a moving part without adding value.
- **Apple gets richer signals than Play.** `appStoreState` transitions through `IN_REVIEW` → `PENDING_DEVELOPER_RELEASE` / `READY_FOR_SALE` / `REJECTED` — we map all three. Play's `edits.tracks` only reliably exposes `inProgress` / `completed` for `rolloutPercentage` changes; rejections arrive via email. We emit `store_live` on `completed` and leave rejections to Phase 5c-ii (where email parsing might help).

---

## Task 1: Extract `storeauth` helpers

**Files:**
- Create: `internal/services/storeauth/apple.go`
- Create: `internal/services/storeauth/google.go`
- Modify: `internal/controllers/connection_validation.go`

- [ ] **Step 1: Write `storeauth/apple.go`**

```go
package storeauth

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppleConnectionInput is the minimum set of fields needed to mint an
// App Store Connect JWT. Kept decoupled from the DB model so callers
// (validation, polling) can both use it without circular imports.
type AppleConnectionInput struct {
	KeyID    string
	IssuerID string
	// P8PEM is the raw bytes of the .p8 file as downloaded from App
	// Store Connect.
	P8PEM []byte
}

// AppleToken signs a 10-minute App Store Connect JWT.
func AppleToken(in AppleConnectionInput) (string, error) {
	privateKey, err := parseECPrivateKeyFromP8(in.P8PEM)
	if err != nil {
		return "", err
	}
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": in.IssuerID,
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"aud": "appstoreconnect-v1",
	})
	token.Header["kid"] = in.KeyID
	token.Header["typ"] = "JWT"
	return token.SignedString(privateKey)
}

func parseECPrivateKeyFromP8(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in .p8 file")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PKCS8 private key: %w", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not an EC private key (got %T)", key)
	}
	return ecKey, nil
}
```

- [ ] **Step 2: Write `storeauth/google.go`**

```go
package storeauth

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GoogleServiceAccount is the subset of fields we read from a Google
// service-account JSON key.
type GoogleServiceAccount struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

// ParseServiceAccount unmarshals a service-account JSON blob.
func ParseServiceAccount(saJSON []byte) (GoogleServiceAccount, error) {
	var sa GoogleServiceAccount
	if err := json.Unmarshal(saJSON, &sa); err != nil {
		return sa, fmt.Errorf("parse service account json: %w", err)
	}
	if sa.Type != "service_account" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return sa, fmt.Errorf("service account missing required fields")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return sa, nil
}

// GoogleToken mints a short-lived OAuth2 access token scoped to the
// androidpublisher API via the JWT-bearer grant.
func GoogleToken(ctx context.Context, sa GoogleServiceAccount) (string, error) {
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("no PEM block in service account private key")
	}
	rsaKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse service account private key: %w", err)
	}
	now := time.Now()
	grant := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   sa.ClientEmail,
		"sub":   sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/androidpublisher",
		"aud":   sa.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	signed, err := grant.SignedString(rsaKey)
	if err != nil {
		return "", fmt.Errorf("sign grant jwt: %w", err)
	}
	vals := url.Values{}
	vals.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	vals.Set("assertion", signed)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sa.TokenURI, strings.NewReader(vals.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth2 token exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google oauth2 status %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.AccessToken == "" {
		return "", fmt.Errorf("parse oauth2 response: %w", err)
	}
	return parsed.AccessToken, nil
}
```

- [ ] **Step 3: Refactor `connection_validation.go` to use the helpers**

Replace the inlined JWT/OAuth2 blocks in `ValidateAppleConnection` with:

```go
signed, err := storeauth.AppleToken(storeauth.AppleConnectionInput{
    KeyID:    req.KeyID,
    IssuerID: req.IssuerID,
    P8PEM:    p8Bytes,
})
if err != nil {
    return responses.ValidationResult{
        Valid:   false,
        Message: "Failed to sign the App Store Connect JWT: " + err.Error(),
    }
}
```

And in `ValidateGooglePlayConnection`:

```go
sa, err := storeauth.ParseServiceAccount(saBytes)
if err != nil {
    return responses.ValidationResult{
        Valid:   false,
        Message: "The service account file could not be parsed: " + err.Error(),
    }
}
accessToken, err := storeauth.GoogleToken(ctx, sa)
if err != nil {
    return responses.ValidationResult{
        Valid:   false,
        Message: "Google rejected the service account credentials: " + err.Error(),
    }
}
// then use accessToken in the pubReq
```

Drop the now-unused private helpers (`parseECPrivateKeyFromP8` moves to `storeauth`). Remove the unused `strings`, `url`, `pem`, `x509`, `jwt`, `ecdsa` imports from `connection_validation.go`.

- [ ] **Step 4: Verify + commit**

```bash
cd /Users/knid/Projects/wemob/wemob-backend
go build ./...
go vet ./...
git add internal/services/storeauth/ internal/controllers/connection_validation.go
git commit -m "refactor(ship): extract store-API auth into internal/services/storeauth

Moves App Store Connect JWT minting + Google service-account OAuth2
token exchange out of connection_validation.go into a reusable
storeauth package. Phase 5c-i's polling worker + the existing
Validate* handlers now share the same code path.

No behavior change; connection_validation still returns the same
strings for the same inputs."
```

---

## Task 2: `HasEvent` dedup + `ListInFlightDeployments`

**Files:**
- Modify: `internal/database/deployment_events.go`
- Modify: `internal/database/deployments.go`

- [ ] **Step 1: Add `HasEvent`**

Append to `internal/database/deployment_events.go`:

```go
// HasEvent reports whether a deployment already has at least one event
// of the given type. Used by the store-API poller for idempotent writes.
func HasEvent(ctx context.Context, db *bun.DB, deploymentID uuid.UUID, eventType string) (bool, error) {
	count, err := db.NewSelect().
		Model((*models.DeploymentEvent)(nil)).
		Where("deployment_id = ?", deploymentID).
		Where("event_type = ?", eventType).
		Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
```

Imports needed: `"github.com/google/uuid"` (likely already imported in that file).

- [ ] **Step 2: Add `ListInFlightDeployments`**

In `internal/database/deployments.go` (or wherever `GetDeploymentByID` lives), add:

```go
// ListInFlightDeployments returns deployments the poller should check:
// anything submitted, reviewing, uploaded, or deploying, submitted within
// the past 30 days. Terminal states (deployed, rejected, failed) are
// skipped so we don't keep hitting the stores for finished work.
func (d *Database) ListInFlightDeployments(ctx context.Context) ([]models.Deployment, error) {
	var deployments []models.Deployment
	since := time.Now().Add(-30 * 24 * time.Hour)
	if err := d.DB.NewSelect().
		Model(&deployments).
		Relation("Project").
		Relation("AppleConnection").
		Relation("GooglePlayConnection").
		Where("d.created_at > ?", since).
		Where("LOWER(d.status) IN (?)", bun.In([]string{"submitted", "queued", "reviewing", "uploaded_to_store", "deploying"})).
		Scan(ctx); err != nil {
		return nil, err
	}
	return deployments, nil
}
```

If `deployments.go` doesn't exist yet as a dedicated file (most DB helpers are inlined or in `database.go`), locate the method next to the existing `GetDeploymentByID` / `UpdateDeployment` and mirror their style.

- [ ] **Step 3: Verify + commit**

```bash
go build ./...
go vet ./...
git add internal/database/
git commit -m "feat(ship): add HasEvent + ListInFlightDeployments helpers

HasEvent is the idempotency guard for the Phase 5c-i store poller
(don't append store_live twice for the same deployment).
ListInFlightDeployments returns only deployments the poller should
check — 30-day window, non-terminal status — with connections and
project preloaded so the checkers don't issue N+1 queries."
```

---

## Task 3: `storepoll` package — `Poller`, `AppleChecker`, `PlayChecker`

**Files:**
- Create: `internal/services/storepoll/poller.go`
- Create: `internal/services/storepoll/apple.go`
- Create: `internal/services/storepoll/play.go`

- [ ] **Step 1: `poller.go`**

```go
package storepoll

import (
	"context"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/services/storage"
)

// Poller periodically queries App Store Connect + Google Play
// Publishing APIs to detect state transitions on in-flight
// deployments and writes corresponding deployment_events with
// actor_type = "store_api".
type Poller struct {
	DB               *database.Database
	UserKeyStorage   *storage.S3Storage
	// PollEvery controls the ticker cadence. Sensible range 5-15 minutes;
	// under 1 minute will rate-limit against Apple.
	PollEvery time.Duration
}

// Run blocks until ctx is cancelled, ticking every PollEvery.
// Non-fatal errors (bad credentials, store API hiccups) are logged
// and skipped; the loop carries on.
func (p *Poller) Run(ctx context.Context) {
	if p.PollEvery <= 0 {
		p.PollEvery = 10 * time.Minute
	}
	ticker := time.NewTicker(p.PollEvery)
	defer ticker.Stop()

	// Kick once immediately on startup so the timeline freshens on
	// the first load after a deploy of the backend.
	p.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) tick(ctx context.Context) {
	deployments, err := p.DB.ListInFlightDeployments(ctx)
	if err != nil {
		slog.Warn("storepoll: list in-flight deployments failed", "err", err)
		return
	}
	if len(deployments) == 0 {
		return
	}
	slog.Debug("storepoll: ticking", "deployments", len(deployments))

	for i := range deployments {
		d := deployments[i]
		// Pick the checker to run. Platforms are "ios" / "android";
		// a deployment targets exactly one store in practice (Pilot
		// and QuickShip both submit one at a time). If multi-platform
		// deployments appear in the future both branches below run.
		for _, platform := range d.Platforms {
			switch platform {
			case "ios":
				if d.AppleConnection == nil {
					continue
				}
				chk := AppleChecker{DB: p.DB, UserKeyStorage: p.UserKeyStorage}
				if err := chk.Check(ctx, d); err != nil {
					slog.Warn("storepoll: apple check failed", "deployment_id", d.ID, "err", err)
				}
			case "android":
				if d.GooglePlayConnection == nil {
					continue
				}
				chk := PlayChecker{DB: p.DB, UserKeyStorage: p.UserKeyStorage}
				if err := chk.Check(ctx, d); err != nil {
					slog.Warn("storepoll: play check failed", "deployment_id", d.ID, "err", err)
				}
			}
		}
	}
}
```

- [ ] **Step 2: `apple.go`**

```go
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
//   IN_REVIEW                                    → store_review_started
//   READY_FOR_SALE | PENDING_DEVELOPER_RELEASE  → store_live
//   REJECTED | METADATA_REJECTED | DEVELOPER_REJECTED → store_rejected
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
```

- [ ] **Step 3: `play.go`**

```go
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

type PlayChecker struct {
	DB             *database.Database
	UserKeyStorage *storage.S3Storage
}

// Check opens a Play Publishing edit for the package, reads the
// production track's releases, and maps a "completed" release to
// store_live. Play does not surface review-in-progress status via a
// public API, so store_review_started is not emitted on Android.
//
// Edits are ephemeral objects; we open, read, and deleteEdit in one
// pass (no commit, so we don't accidentally mutate user state).
func (c PlayChecker) Check(ctx context.Context, d models.Deployment) error {
	if d.GooglePlayConnection == nil || d.Project == nil {
		return fmt.Errorf("deployment missing connection or project")
	}
	saBytes, err := c.UserKeyStorage.ReadObjectBytes(ctx, d.GooglePlayConnection.ServiceAccountPath)
	if err != nil {
		return fmt.Errorf("read service account: %w", err)
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
	var edit struct{ Id string `json:"id"` }
	if err := json.Unmarshal(editBody, &edit); err != nil || edit.Id == "" {
		return fmt.Errorf("parse edit response: %w", err)
	}

	// Always clean up the edit, success or failure.
	defer func() {
		delURL := editURL + "/" + edit.Id
		delReq, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, delURL, nil)
		delReq.Header.Set("Authorization", "Bearer "+token)
		_, _ = http.DefaultClient.Do(delReq)
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
```

- [ ] **Step 4: Ensure `DeploymentEventActorStoreAPI` exists**

Check `internal/models/deployment_event.go` for `DeploymentEventActorStoreAPI`. If absent, add it next to the other actor constants:

```go
DeploymentEventActorStoreAPI = "store_api"
```

- [ ] **Step 5: Verify + commit**

```bash
go build ./...
go vet ./...
git add internal/services/storepoll/ internal/models/deployment_event.go
git commit -m "feat(ship): storepoll package — App Store + Play polling checkers

Apple: resolve app by bundleId → read latest appStoreVersion → map
appStoreState to store_review_started / store_live / store_rejected.
Play: open ephemeral edit → read production track → emit store_live
when any release is 'completed'.

Checkers dedup via HasEvent and denormalize status to deployment.
Rejection detection on Play is deferred to Phase 5c-ii (email-driven).

All events use actor_type = 'store_api' so the frontend can attribute
them correctly in the timeline."
```

---

## Task 4: Wire `Poller` into `cmd/main.go`

**Files:**
- Modify: `internal/controllers/controller.go` (if the Controller tracks the poller) OR `cmd/main.go` directly.

The simplest path is `cmd/main.go` — the Poller doesn't need to be on Controller.

- [ ] **Step 1: Spin up the poller goroutine**

Find the preview-code cleanup goroutine in `cmd/main.go` (near line 1873 per the current file). Immediately after it, add:

```go
// Phase 5c-i: poll App Store Connect + Google Play for state changes
// on in-flight deployments, writing store_* events.
pollerCtx, cancelPoller := context.WithCancel(context.Background())
poller := &storepoll.Poller{
    DB:             controller.DB,
    UserKeyStorage: controller.UserApiKeysStorage,
    PollEvery:      10 * time.Minute,
}
go poller.Run(pollerCtx)
```

Imports to add at the top:

```go
"github.com/knid/wemob/internal/services/storepoll"
```

(`context` is likely already imported.)

- [ ] **Step 2: Cancel on shutdown**

Inside the existing shutdown goroutine (the one that calls `mainServer.Shutdown`), add before the `mainServer.Shutdown(ctx)` line:

```go
// Stop the store-API poller.
cancelPoller()
```

- [ ] **Step 3: Verify + commit**

```bash
go build ./...
go vet ./...
git add cmd/main.go
git commit -m "feat(ship): run storepoll.Poller as a periodic goroutine

10-minute cadence. Kicks once on startup so the first load after a
deploy freshens any already-completed reviews. Cancelled on
SIGINT/SIGTERM next to the main HTTP server shutdown."
```

---

## Task 5: End-to-end smoke

Live-only — documented for user. No commit.

```
cd /Users/knid/Projects/wemob/wemob-backend
docker-compose -f docker-compose-dev.yaml up -d
go run cmd/main.go
```

Expected log on startup:

```
INFO msg="storepoll: ticking" deployments=<N>
```

Test scenarios:

1. **Healthy Apple deployment in `IN_REVIEW`** — after the first tick, the deployment's timeline should show a fresh `store_review_started` event attributed to `store_api`. Deployment status flips from `submitted` → `reviewing`. The Launchpad status hero re-computes.

2. **Already-live Apple deployment** — tick emits `store_live` once, then is silent on subsequent ticks (dedup). Deployment status `deployed`.

3. **Play deployment with rolled-out release** — tick emits `store_live`. Status flips to `deployed`.

4. **Deployment with bad credentials** — the checker logs a warning and moves on. The ticker does not crash. Next tick still runs other deployments.

5. **No in-flight deployments** — `storepoll: ticking deployments=0` is not logged (early-return suppresses spam).

```sql
-- Useful query to watch events as they arrive:
SELECT e.deployment_id, e.event_type, e.actor_type, e.occurred_at, e.metadata
FROM deployment_events e
WHERE e.actor_type = 'store_api'
ORDER BY e.occurred_at DESC
LIMIT 20;
```

- [ ] **Step 1: No commit — user-run smoke.**

---

## Phase 5c-i completion criteria

- `internal/services/storeauth/` exports `AppleToken`, `ParseServiceAccount`, `GoogleToken`.
- `connection_validation.go` delegates to the storeauth helpers (no inlined JWT/OAuth code).
- `internal/database/deployment_events.go` exports `HasEvent`.
- `internal/database` exports `ListInFlightDeployments`.
- `internal/services/storepoll/` exports `Poller`, `AppleChecker`, `PlayChecker`.
- `DeploymentEventActorStoreAPI = "store_api"` exists.
- `cmd/main.go` spins up the poller on startup with a 10-minute cadence and cancels it on shutdown.
- `go build ./...` + `go vet ./...` clean.
- Zero regressions in existing Validate* handlers.

Phase 5c-ii (pending_connection draft + auto-resume) and Phase 5c-iii (email nudges + flag ramp) unblocked.

## Out of scope

- Play rejection detection (Phase 5c-ii — via email parsing).
- Backoff / rate-limit handling per-deployment (Phase 5c-ii — current behavior is "log and skip").
- Storing the resolved App Store `appStoreVersion.id` on the deployment to avoid re-querying apps list every tick (Phase 5c-ii — current cost is 2 requests per in-flight deployment per tick, fine at our scale).
- Poll cadence flag (PostHog remote config) — defer until observed need.
- Caching the OAuth2 / JWT tokens (both are short-lived; minting costs microseconds).
