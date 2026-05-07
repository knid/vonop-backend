//go:build integration

package controllers

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/storage"
)

// submitFixture wires a test controller + project + user + org +
// (optional) Apple/Google connections + (optional) successful build.
type submitFixture struct {
	c       *Controller
	db      *database.Database
	cmRec   *expoTriggerRecorder
	user    *models.User
	org     *models.Organization
	project *models.AppProject
	build   *models.Build
}

// newSubmitFixture spins up DB + Controller + connections + (optionally) a
// successful build. Tests opt into Apple/Google credentials and a seeded
// build via the boolean flags so each test can target precisely the
// precondition it's checking.
func newSubmitFixture(t *testing.T, withApple, withGoogle, withBuild bool) *submitFixture {
	t.Helper()
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppProject)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
		(*models.AppleConnection)(nil),
		(*models.GooglePlayConnection)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	c.RemoteConfig = remoteconfig.New(nil)

	// UserApiKeysStorage stub — required for the encryptAndStoreCredential
	// path; we set the legacy *Path to "" on connections to short-circuit
	// reads to CredStore-only.
	c.UserApiKeysStorage = &storage.S3Storage{Region: "us-east-1", Bucket: "test-keys"}

	// Codemagic test server (records trigger calls).
	cmServer, cmRec := newCodemagicTestServer(t)
	cmService := codemagic.NewService("test-token", "test-app-id")
	cmService.SetTestEndpoint(cmServer.URL)
	c.CodemagicService = cmService

	ctx := context.Background()
	user := &models.User{ID: uuid.New(), Email: "u-" + uuid.NewString()[:6] + "@example.com", Name: "U", Password: "x"}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	org := &models.Organization{ID: uuid.New(), Name: "O", Slug: "o-" + uuid.NewString()[:8], OwnerID: user.ID}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID, UserID: user.ID, Role: "owner",
	}).Exec(ctx); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "X", Slug: "x-" + uuid.NewString()[:8], Prompt: "n",
		ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
		AppStoreTrack: "internal", PlayStoreTrack: "internal",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	fx := &submitFixture{c: c, db: db, cmRec: cmRec, user: user, org: org, project: project}

	if withApple {
		// Encrypt a fake .p8 via CredStore so the read path exercises decryption.
		p8 := []byte("-----BEGIN PRIVATE KEY-----\nFAKE\n-----END PRIVATE KEY-----")
		ref, err := c.CredStore.Put(ctx, org.ID, "apple-p8", p8)
		if err != nil {
			t.Fatalf("seed apple cred: %v", err)
		}
		conn := &models.AppleConnection{
			ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
			Name: "test", KeyID: "ABCDEFGHIJ", TeamID: "TEAM01",
			IssuerID:      "11111111-1111-1111-1111-111111111111",
			P8KeyPath:     "p8_keys/dummy.p8",
			APNSP8KeyPath: "p8_keys/dummy-apns.p8",
			APNSP8KeyID:   "K",
			P8KeyCredRef:  credRefFromStorage(ref),
		}
		if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
			t.Fatalf("seed apple conn: %v", err)
		}
		project.DefaultAppleConnectionID = &conn.ID
		project.UpdatedAt = time.Now()
		if _, err := db.UpdateAppProject(project); err != nil {
			t.Fatalf("update project DefaultAppleConnectionID: %v", err)
		}
	}

	if withGoogle {
		sa := []byte(`{"type":"service_account","client_email":"x@y.iam","private_key":"FAKE"}`)
		ref, err := c.CredStore.Put(ctx, org.ID, "google-sa", sa)
		if err != nil {
			t.Fatalf("seed google cred: %v", err)
		}
		conn := &models.GooglePlayConnection{
			ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
			Name: "test", ServiceAccountPath: "service_accounts/dummy.json",
			ServiceAccountCredRef: credRefFromStorage(ref),
		}
		if _, err := db.DB.NewInsert().Model(conn).Exec(ctx); err != nil {
			t.Fatalf("seed google conn: %v", err)
		}
		project.DefaultGoogleConnectionID = &conn.ID
		project.UpdatedAt = time.Now()
		if _, err := db.UpdateAppProject(project); err != nil {
			t.Fatalf("update project DefaultGoogleConnectionID: %v", err)
		}
	}

	if withBuild {
		// Seed Build + successful AppBuildTask for both platforms, scoped via
		// app_project_id directly (no v1 shadow row).
		build := &models.Build{
			ID:                uuid.New(),
			AppProjectID:      &project.ID,
			BuildNumber:       1,
			Title:             "X",
			Description:       "test build",
			Icon:              "",
			SplashScreenColor: "#FFFFFF",
			NavBarStyle:       "default",
			VersionString:     "1",
		}
		if _, err := db.DB.NewInsert().Model(build).Exec(ctx); err != nil {
			t.Fatalf("seed build: %v", err)
		}
		appProjectID := project.ID
		for _, platform := range []string{"Android", "IOS"} {
			task := &models.AppBuildTask{
				ID: uuid.New(), AppProjectID: &appProjectID, BuildID: build.ID, Platform: platform,
				StatusText: "Built", Finished: true,
				CreatedAt: time.Now(),
			}
			if _, err := db.DB.NewInsert().Model(task).Exec(ctx); err != nil {
				t.Fatalf("seed task %s: %v", platform, err)
			}
		}
		fx.build = build
	}

	// Reload project so subsequent reads see the persisted DefaultConnectionIDs.
	reloaded, err := db.GetAppProjectByID(project.ID.String())
	if err != nil {
		t.Fatalf("reload project: %v", err)
	}
	fx.project = reloaded

	return fx
}

// findCallByWorkflow returns the first recorded trigger call with the given
// workflow ID, or nil.
func findCallByWorkflow(rec *expoTriggerRecorder, workflowID string) *expoTriggerCall {
	for _, c := range rec.snapshot() {
		if c.WorkflowID == workflowID {
			return &c
		}
	}
	return nil
}

func TestSubmitExpoMobile_RejectsWrongProjectType(t *testing.T) {
	fx := newSubmitFixture(t, true, true, true)
	fx.project.ProjectType = models.ProjectTypeApp
	fx.project.UpdatedAt = time.Now()
	if _, err := fx.c.DB.UpdateAppProject(fx.project); err != nil {
		t.Fatalf("update: %v", err)
	}
	_, err := fx.c.SubmitExpoMobile(context.Background(), fx.project, fx.user, SubmitExpoOptions{Platform: "android"})
	if !errors.Is(err, ErrSubmitWrongProjectType) {
		t.Errorf("got %v want ErrSubmitWrongProjectType", err)
	}
}

func TestSubmitExpoMobile_RejectsBadPlatform(t *testing.T) {
	fx := newSubmitFixture(t, true, true, true)
	_, err := fx.c.SubmitExpoMobile(context.Background(), fx.project, fx.user, SubmitExpoOptions{Platform: "windows"})
	if !errors.Is(err, ErrSubmitInvalidPlatform) {
		t.Errorf("got %v want ErrSubmitInvalidPlatform", err)
	}
}

func TestSubmitExpoMobile_NoBuildYet(t *testing.T) {
	// withBuild=false means no v1 shadow project, no build.
	fx := newSubmitFixture(t, true, true, false)
	_, err := fx.c.SubmitExpoMobile(context.Background(), fx.project, fx.user, SubmitExpoOptions{Platform: "android"})
	if !errors.Is(err, ErrSubmitNoBuildToSubmit) {
		t.Errorf("got %v want ErrSubmitNoBuildToSubmit", err)
	}
}

func TestSubmitExpoMobile_MissingGoogleCredential(t *testing.T) {
	fx := newSubmitFixture(t, false, false, true)
	_, err := fx.c.SubmitExpoMobile(context.Background(), fx.project, fx.user, SubmitExpoOptions{Platform: "android"})
	if !errors.Is(err, ErrSubmitMissingCredential) {
		t.Errorf("got %v want ErrSubmitMissingCredential", err)
	}
}

func TestSubmitExpoMobile_MissingAppleCredential(t *testing.T) {
	fx := newSubmitFixture(t, false, false, true)
	_, err := fx.c.SubmitExpoMobile(context.Background(), fx.project, fx.user, SubmitExpoOptions{Platform: "ios"})
	if !errors.Is(err, ErrSubmitMissingCredential) {
		t.Errorf("got %v want ErrSubmitMissingCredential", err)
	}
}

func TestSubmitExpoMobile_AndroidHappyPath(t *testing.T) {
	fx := newSubmitFixture(t, false, true, true)
	res, err := fx.c.SubmitExpoMobile(context.Background(), fx.project, fx.user, SubmitExpoOptions{
		Platform: "android",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Platform != "android" {
		t.Errorf("platform: got %q want android", res.Platform)
	}
	if res.CodemagicBuildID == "" {
		t.Error("codemagic build id empty")
	}
	call := findCallByWorkflow(fx.cmRec, codemagic.WorkflowExpoAndroidDeploy)
	if call == nil {
		t.Fatalf("expected a %s trigger call, got %+v", codemagic.WorkflowExpoAndroidDeploy, fx.cmRec.snapshot())
	}
	saB64 := call.Variables["CM_GOOGLE_PLAY_SA_JSON_B64"]
	decoded, decErr := base64.StdEncoding.DecodeString(saB64)
	if decErr != nil {
		t.Errorf("decode SA b64: %v", decErr)
	}
	if !strings.Contains(string(decoded), "service_account") {
		t.Errorf("decoded SA doesn't contain service_account: %s", decoded)
	}
	if call.Variables["CM_PLAY_TRACK"] != "internal" {
		t.Errorf("default track: got %q want internal", call.Variables["CM_PLAY_TRACK"])
	}
}

func TestSubmitExpoMobile_IOSHappyPathWithOverrides(t *testing.T) {
	fx := newSubmitFixture(t, true, false, true)
	phasedTrue := true
	res, err := fx.c.SubmitExpoMobile(context.Background(), fx.project, fx.user, SubmitExpoOptions{
		Platform:              "ios",
		TrackOverride:         "production",
		PhasedReleaseOverride: &phasedTrue,
		SubmitToAppStore:      true,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Platform != "ios" {
		t.Errorf("platform: got %q want ios", res.Platform)
	}
	call := findCallByWorkflow(fx.cmRec, codemagic.WorkflowExpoIOSDeploy)
	if call == nil {
		t.Fatalf("expected a %s trigger call, got %+v", codemagic.WorkflowExpoIOSDeploy, fx.cmRec.snapshot())
	}
	if call.Variables["CM_PHASED_RELEASE"] != "true" {
		t.Errorf("phased: got %q want true", call.Variables["CM_PHASED_RELEASE"])
	}
	if call.Variables["CM_SUBMIT_TO_APP_STORE"] != "true" {
		t.Errorf("submit_to_app_store: got %q want true", call.Variables["CM_SUBMIT_TO_APP_STORE"])
	}
	p8B64 := call.Variables["CM_ASC_API_KEY_B64"]
	decoded, _ := base64.StdEncoding.DecodeString(p8B64)
	if !strings.Contains(string(decoded), "BEGIN PRIVATE KEY") {
		t.Errorf("decoded p8 doesn't look like a key: %s", decoded)
	}
	if call.Variables["CM_ASC_KEY_ID"] != "ABCDEFGHIJ" {
		t.Errorf("ASC key id: got %q want ABCDEFGHIJ", call.Variables["CM_ASC_KEY_ID"])
	}
	if call.Variables["CM_ASC_ISSUER_ID"] != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("ASC issuer id: got %q", call.Variables["CM_ASC_ISSUER_ID"])
	}
}

func TestSubmitExpoMobile_RejectsBadAndroidTrack(t *testing.T) {
	fx := newSubmitFixture(t, false, true, true)
	_, err := fx.c.SubmitExpoMobile(context.Background(), fx.project, fx.user, SubmitExpoOptions{
		Platform: "android", TrackOverride: "frobnicate",
	})
	if !errors.Is(err, ErrSubmitInvalidTrack) {
		t.Errorf("got %v want ErrSubmitInvalidTrack", err)
	}
}

// TestRecordReleaseFromSubmit_PersistsRowForBothPlatforms is the P1 #6
// regression test for the submit→release wiring. It exercises the
// recordReleaseFromSubmit helper directly so the test doesn't have to mock
// the live App Store / Play Store version-check HTTP calls that the full
// SubmitExpoMobile path makes (those are an unrelated, pre-existing test
// pain point — the existing happy-path tests fail on main for the same
// reason; tracked as adjacent bug, not in this PR's scope).
//
// Without the wiring, the polling cron, ASC notification handler,
// halt/rollback all operate on an empty result set — the entire
// release-management surface is dead.
func TestRecordReleaseFromSubmit_PersistsRowForBothPlatforms(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.Release)(nil),
	)
	if _, err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_releases_project_platform_track_version
		ON releases (app_project_id, platform, track, version_code) WHERE deleted_at IS NULL`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	project := &models.AppProject{
		ID:             uuid.New(),
		OrganizationID: uuid.New(),
	}
	build := &models.Build{
		ID:            uuid.New(),
		BuildNumber:   42,
		VersionString: "1.2.3",
	}
	task := &models.AppBuildTask{ID: uuid.New()}

	// Android — non-phased.
	relID := c.recordReleaseFromSubmit(ctx, project, build, task,
		models.ReleasePlatformAndroid, "internal", 0, false, "cm-build-and-1")
	if relID == uuid.Nil {
		t.Fatalf("android: expected ReleaseID set; got uuid.Nil (release row creation silently failed)")
	}
	andRel, err := c.DB.GetRelease(ctx, relID)
	if err != nil {
		t.Fatalf("load android release: %v", err)
	}
	if andRel.AppProjectID != project.ID {
		t.Errorf("AppProjectID: got %s want %s", andRel.AppProjectID, project.ID)
	}
	if andRel.OrganizationID != project.OrganizationID {
		t.Errorf("OrganizationID: got %s want %s (org-scope leak?)", andRel.OrganizationID, project.OrganizationID)
	}
	if andRel.Platform != models.ReleasePlatformAndroid {
		t.Errorf("Platform: got %q want android", andRel.Platform)
	}
	if andRel.Track != "internal" {
		t.Errorf("Track: got %q want internal", andRel.Track)
	}
	if andRel.VersionCode != build.BuildNumber {
		t.Errorf("VersionCode: got %d want %d", andRel.VersionCode, build.BuildNumber)
	}
	if andRel.MarketingVersion != build.VersionString {
		t.Errorf("MarketingVersion: got %q want %q", andRel.MarketingVersion, build.VersionString)
	}
	if andRel.BuildID == nil || *andRel.BuildID != build.ID {
		t.Errorf("BuildID: got %v want %v", andRel.BuildID, build.ID)
	}
	if andRel.CodemagicTaskID == nil || *andRel.CodemagicTaskID != task.ID {
		t.Errorf("CodemagicTaskID: got %v want %v", andRel.CodemagicTaskID, task.ID)
	}
	if andRel.Status != models.ReleaseStatusUploaded {
		t.Errorf("Status: got %q want uploaded", andRel.Status)
	}

	// iOS — phased = true. PhasedReleaseState should track to ACTIVE.
	iosBuild := &models.Build{
		ID:            uuid.New(),
		BuildNumber:   100,
		VersionString: "2.0.0",
	}
	iosTask := &models.AppBuildTask{ID: uuid.New()}
	iosRelID := c.recordReleaseFromSubmit(ctx, project, iosBuild, iosTask,
		models.ReleasePlatformIOS, "production", 0, true, "cm-build-ios-1")
	if iosRelID == uuid.Nil {
		t.Fatalf("ios: expected ReleaseID set; got uuid.Nil")
	}
	iosRel, err := c.DB.GetRelease(ctx, iosRelID)
	if err != nil {
		t.Fatalf("load ios release: %v", err)
	}
	if iosRel.Platform != models.ReleasePlatformIOS {
		t.Errorf("ios.Platform: got %q want ios", iosRel.Platform)
	}
	if iosRel.Track != "production" {
		t.Errorf("ios.Track: got %q want production", iosRel.Track)
	}
	if iosRel.PhasedReleaseState != "ACTIVE" {
		t.Errorf("ios.PhasedReleaseState: got %q want ACTIVE (phased iOS submit)", iosRel.PhasedReleaseState)
	}
}

// TestRecordReleaseFromSubmit_Idempotent — re-recording the same
// (project, platform, track, version_code) tuple MUST NOT create a duplicate
// Release row. UpsertRelease conflates on the unique index, so a re-submit
// (e.g. user double-clicks) gets the existing row's id back.
func TestRecordReleaseFromSubmit_Idempotent(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.Release)(nil),
	)
	if _, err := db.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_releases_project_platform_track_version
		ON releases (app_project_id, platform, track, version_code) WHERE deleted_at IS NULL`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}
	c, _ := NewTestController(t, db, TestControllerOptions{})
	ctx := context.Background()

	project := &models.AppProject{ID: uuid.New(), OrganizationID: uuid.New()}
	build := &models.Build{ID: uuid.New(), BuildNumber: 1, VersionString: "1"}
	task := &models.AppBuildTask{ID: uuid.New()}

	first := c.recordReleaseFromSubmit(ctx, project, build, task,
		models.ReleasePlatformAndroid, "internal", 0, false, "cm-1")
	second := c.recordReleaseFromSubmit(ctx, project, build, task,
		models.ReleasePlatformAndroid, "internal", 0, false, "cm-2")

	if first == uuid.Nil || second == uuid.Nil {
		t.Fatalf("both ids should be non-nil; got first=%v second=%v", first, second)
	}

	var count int
	if err := db.DB.NewSelect().Model((*models.Release)(nil)).
		Where("app_project_id = ?", project.ID).
		Where("platform = ?", models.ReleasePlatformAndroid).
		Where("track = ?", "internal").
		Where("version_code = ?", build.BuildNumber).
		ColumnExpr("count(*)").Scan(ctx, &count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 release row after re-submit, got %d (idempotency broken)", count)
	}
}
