//go:build integration

package controllers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/storage"
)

// expoTriggerCall records a single Codemagic trigger POST.
type expoTriggerCall struct {
	WorkflowID string
	Variables  map[string]string
}

// expoTriggerRecorder collects trigger calls for assertions.
type expoTriggerRecorder struct {
	mu    sync.Mutex
	calls []expoTriggerCall
}

func (r *expoTriggerRecorder) record(call expoTriggerCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *expoTriggerRecorder) snapshot() []expoTriggerCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]expoTriggerCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// newCodemagicTestServer returns an httptest.Server that decodes Codemagic
// trigger POSTs into expoTriggerCall records and replies with a fake
// build_id, plus a recorder the test can inspect.
func newCodemagicTestServer(t *testing.T) (*httptest.Server, *expoTriggerRecorder) {
	t.Helper()
	rec := &expoTriggerRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			WorkflowID  string `json:"workflowId"`
			Environment struct {
				Variables map[string]string `json:"variables"`
			} `json:"environment"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode trigger body: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.record(expoTriggerCall{WorkflowID: body.WorkflowID, Variables: body.Environment.Variables})
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"build_id": "cm-build-" + uuid.NewString()})
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// s3PutRecorder captures PUT keys + bodies for the in-memory fake S3.
type s3PutRecorder struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (r *s3PutRecorder) put(key string, body []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]byte, len(body))
	copy(cp, body)
	r.objects[key] = cp
}

func (r *s3PutRecorder) get(key string) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.objects[key]
	return b, ok
}

// newFakeS3Storage returns a *storage.S3Storage whose underlying *s3.Client
// is wired against a path-style httptest.Server that handles PUT requests
// in-memory. Lets us assert tarball uploads without yak-shaving on the
// AWS SDK internals.
//
// Bucket is fixed to "test-bucket"; the SDK puts at /test-bucket/{key}.
func newFakeS3Storage(t *testing.T, bucket string) (*storage.S3Storage, *s3PutRecorder) {
	t.Helper()
	rec := &s3PutRecorder{objects: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// AWS SDK with UsePathStyle hits /<bucket>/<key>; strip the leading
		// /<bucket>/ to get the key.
		path := strings.TrimPrefix(r.URL.Path, "/")
		path = strings.TrimPrefix(path, bucket+"/")
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			rec.put(path, body)
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	client := s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(srv.URL),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
	})

	return &storage.S3Storage{
		Region: "us-east-1",
		Bucket: bucket,
		Client: client,
	}, rec
}

// expoBuildTestFixture bundles everything a test needs.
type expoBuildTestFixture struct {
	c       *Controller
	db      *database.Database
	user    *models.User
	org     *models.Organization
	project *models.AppProject
	cmRec   *expoTriggerRecorder
	s3Rec   *s3PutRecorder
}

// setupExpoBuildTest spins up everything: DB with required tables, a
// Controller wired with a real GitRepoService (temp dir), an
// httptest-backed fake S3 (recording PUTs), an httptest-backed Codemagic
// (recording trigger POSTs), an owner user, an org, a mobile_app_native
// AppProject, and a seeded git repo with a small mobile/ tree.
func setupExpoBuildTest(t *testing.T) *expoBuildTestFixture {
	t.Helper()
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
		(*models.StandardUserLimits)(nil),
		(*models.StandardUserSubscription)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.AppBuildTask)(nil),
		(*models.AICreditsBalance)(nil),
		(*models.AICreditTransaction)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})

	// GitRepoService rooted at a per-test temp dir.
	c.GitRepoService = gitrepo.NewService(t.TempDir())

	// Fake S3 + recorder so we can assert on the tarball upload.
	s3Storage, s3Rec := newFakeS3Storage(t, "test-bucket")
	c.AssetsStorage = s3Storage

	// Codemagic service pointing at an httptest.Server that records calls.
	cmServer, cmRec := newCodemagicTestServer(t)
	cmService := codemagic.NewService("test-token", "test-app-id")
	cmService.SetTestEndpoint(cmServer.URL)
	c.CodemagicService = cmService

	// RemoteConfig in fallback-only mode (PostHog client nil) so
	// GetUserLimits doesn't nil-deref when limits are wired.
	c.RemoteConfig = remoteconfig.New(nil)

	ctx := context.Background()

	user := &models.User{
		ID: uuid.New(), Name: "Owner", Email: "owner-" + uuid.NewString()[:8] + "@example.com", Password: "x",
	}
	if _, err := db.DB.NewInsert().Model(user).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	org := &models.Organization{
		ID: uuid.New(), Name: "Acme", Slug: "acme-expo-" + uuid.NewString()[:8],
		OwnerID: user.ID, IsPersonal: true,
	}
	if _, err := db.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID, UserID: user.ID, Role: "owner",
	}).Exec(ctx); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	project := &models.AppProject{
		ID: uuid.New(), UserID: user.ID, OrganizationID: org.ID,
		Name: "Test App", Slug: "test-app-" + uuid.NewString()[:8], Prompt: "n/a",
		ProjectType: models.ProjectTypeMobileAppNative, Status: "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	// Seed a permissive default plan so GetUserLimits doesn't blow up with
	// "could not load plan limits: sql: no rows in result set". Tests that
	// want to assert plan-level rejection (TestBuildExpoMobile_RejectsWhen
	// AndroidPermissionMissing) can call seedSubscriptionLimits afterwards
	// — Bun's UPDATE on the org's SubscriptionID will swap to the new plan.
	seedSubscriptionLimits(t, db, org, &models.StandardUserLimits{
		CanBuildToAndroid: true,
		CanBuildToIOS:     true,
		CanBuildMobile:    true,
	})

	// Seed an AI credits balance with plenty of credits so ChargeBuildCredits
	// doesn't fail on the platform fan-out path. The Android build charge is
	// 20, iOS is 30 — 10000 covers any realistic test scenario.
	if _, err := db.DB.NewInsert().Model(&models.AICreditsBalance{
		ID: uuid.New(), OrganizationID: org.ID, UserID: user.ID, Balance: 10000,
		PeriodStart: time.Now().Add(-24 * time.Hour),
		PeriodEnd:   time.Now().Add(30 * 24 * time.Hour),
	}).Exec(ctx); err != nil {
		t.Fatalf("insert credits balance: %v", err)
	}

	// Seed the project repo with a small mobile/ skeleton + a backend file
	// that should NOT end up in the tarball.
	repo, err := c.GitRepoService.Init(project.ID.String())
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	for path, content := range map[string]string{
		"mobile/app/index.tsx": "export default function () { return null }",
		"mobile/lib/api.ts":    "export const API_URL = process.env.EXPO_PUBLIC_API_URL",
		"backend/route.ts":     "// not in tarball",
	} {
		if err := repo.WriteFile(path, []byte(content)); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}

	_ = repo // working tree seeded; downstream tests open via c.GitRepoService.Open
	return &expoBuildTestFixture{
		c: c, db: db, user: user, org: org, project: project,
		cmRec: cmRec, s3Rec: s3Rec,
	}
}

// seedSubscriptionLimits inserts a limits row + subscription + binds the
// subscription to the user's personal org so GetUserLimits returns the
// supplied limits. Returns the inserted limits row for further mutation
// in callers.
func seedSubscriptionLimits(t *testing.T, db *database.Database, org *models.Organization, limits *models.StandardUserLimits) *models.StandardUserLimits {
	t.Helper()
	ctx := context.Background()
	if limits.ID == uuid.Nil {
		limits.ID = uuid.New()
	}
	if _, err := db.DB.NewInsert().Model(limits).Exec(ctx); err != nil {
		t.Fatalf("insert limits: %v", err)
	}
	subs := &models.StandardUserSubscription{
		ID: uuid.New(), Name: "test", Description: "test plan",
		LimitsID: limits.ID, MonthlyPrice: 0,
	}
	if _, err := db.DB.NewInsert().Model(subs).Exec(ctx); err != nil {
		t.Fatalf("insert sub: %v", err)
	}
	org.SubscriptionID = subs.ID
	if _, err := db.DB.NewUpdate().Model(org).Where("id = ?", org.ID).Exec(ctx); err != nil {
		t.Fatalf("bind sub to org: %v", err)
	}
	return limits
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestBuildExpoMobile_HappyPath_AndroidOnly verifies that a single-platform
// Android build creates a Build row, exactly one AppBuildTask, calls
// Codemagic once with the right env vars (especially EXPO_PUBLIC_API_URL =
// https://{slug}.wemob.app and EXPO_PUBLIC_PROJECT_ID = the project UUID),
// and uploads the tarball to s3://test-bucket/expo-source/{projectID}/{buildID}.tar.gz.
func TestBuildExpoMobile_HappyPath_AndroidOnly(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	build, tasks, err := f.c.BuildExpoMobile(ctx, f.project, f.user, ExpoBuildOptions{
		Platforms:   []string{"android"},
		AppName:     "My Test App",
		Icon:        "icons/test.png",
		SplashColor: "#112233",
	})
	if err != nil {
		t.Fatalf("BuildExpoMobile: %v", err)
	}
	if build == nil {
		t.Fatal("expected build, got nil")
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Platform != "Android" {
		t.Errorf("task[0].Platform = %q, want Android", tasks[0].Platform)
	}
	if tasks[0].StatusText != "Queued" {
		t.Errorf("task[0].StatusText = %q, want Queued", tasks[0].StatusText)
	}
	if tasks[0].Finished {
		t.Error("task[0].Finished = true, want false")
	}
	if build.BuildNumber != 1 {
		t.Errorf("build.BuildNumber = %d, want 1", build.BuildNumber)
	}
	if build.Title != "My Test App" {
		t.Errorf("build.Title = %q, want My Test App", build.Title)
	}
	if build.SplashScreenColor != "#112233" {
		t.Errorf("build.SplashScreenColor = %q, want #112233", build.SplashScreenColor)
	}

	// Post-Task-2: NO v1 shadow Project row should be created on the mobile path.
	assertNoExpoShadowProject(t, f.db, f.project.Slug)
	// Build row must carry the direct AppProject FK.
	if build.AppProjectID != f.project.ID {
		t.Errorf("build.AppProjectID = %v, want %s", build.AppProjectID, f.project.ID)
	}

	// Verify Codemagic trigger.
	calls := f.cmRec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 codemagic call, got %d", len(calls))
	}
	call := calls[0]
	if call.WorkflowID != codemagic.WorkflowExpoAndroidBuild {
		t.Errorf("workflowID = %q, want %q", call.WorkflowID, codemagic.WorkflowExpoAndroidBuild)
	}
	wantEnv := map[string]string{
		"CM_PROJECT_ID":          f.project.ID.String(),
		"CM_BUILD_ID":            build.ID.String(),
		"CM_TASK_ID":             tasks[0].ID.String(),
		"CM_APP_TITLE":           "My Test App",
		"CM_APP_SLUG":            f.project.Slug,
		"CM_BUNDLE_ID":           bundleIDForProject(f.project, ""),
		"CM_PACKAGE_NAME":        bundleIDForProject(f.project, ""),
		"CM_BUILD_NUMBER":        "1",
		"CM_MARKETING_VERSION":   "1.0.0",
		"CM_SPLASH_COLOR":        "#112233",
		"CM_FIRST_BUILD":         "true",
		"CM_ICON_KEY":            "icons/test.png",
		"CM_EAS_PROJECT_UUID":    f.project.ID.String(),
		"EXPO_PUBLIC_API_URL":       "https://" + f.project.Slug + ".wemob.app",
		"EXPO_PUBLIC_PROJECT_ID":    f.project.ID.String(),
		"EXPO_PUBLIC_WEMOB_API_URL": "https://api.wemob.io",
	}
	for k, want := range wantEnv {
		if got := call.Variables[k]; got != want {
			t.Errorf("env %s = %q, want %q", k, got, want)
		}
	}

	// Verify the tarball upload.
	tarKey := "expo-source/" + f.project.ID.String() + "/" + build.ID.String() + ".tar.gz"
	body, ok := f.s3Rec.get(tarKey)
	if !ok {
		t.Fatalf("expected S3 PUT at key %q; recorded keys: %v", tarKey, recordedKeys(f.s3Rec))
	}
	if len(body) == 0 {
		t.Errorf("uploaded tarball is empty")
	}
	// Smoke check: gzip magic bytes 0x1f 0x8b.
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		t.Errorf("uploaded body is not gzip-framed; first bytes = %x", body[:min(2, len(body))])
	}
}

// TestBuildExpoMobile_HappyPath_BothPlatforms verifies the dual-platform
// fan-out: 1 Build, 2 AppBuildTasks, both Android+iOS triggers fired.
func TestBuildExpoMobile_HappyPath_BothPlatforms(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	build, tasks, err := f.c.BuildExpoMobile(ctx, f.project, f.user, ExpoBuildOptions{
		Platforms:   []string{"android", "ios"},
		AppName:     "Dual App",
		Icon:        "icons/dual.png",
		SplashColor: "#abcdef",
	})
	if err != nil {
		t.Fatalf("BuildExpoMobile: %v", err)
	}
	if build == nil {
		t.Fatal("expected build, got nil")
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Platform != "Android" || tasks[1].Platform != "IOS" {
		t.Errorf("task platforms = [%q,%q], want [Android,IOS]", tasks[0].Platform, tasks[1].Platform)
	}

	calls := f.cmRec.snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 codemagic calls, got %d", len(calls))
	}
	gotWorkflows := map[string]int{}
	for _, c := range calls {
		gotWorkflows[c.WorkflowID]++
	}
	if gotWorkflows[codemagic.WorkflowExpoAndroidBuild] != 1 {
		t.Errorf("expected 1 expo-android-build call, got %d", gotWorkflows[codemagic.WorkflowExpoAndroidBuild])
	}
	if gotWorkflows[codemagic.WorkflowExpoIOSBuild] != 1 {
		t.Errorf("expected 1 expo-ios-build call, got %d", gotWorkflows[codemagic.WorkflowExpoIOSBuild])
	}

	// Both calls should share the same build ID + project ID.
	for _, c := range calls {
		if c.Variables["CM_BUILD_ID"] != build.ID.String() {
			t.Errorf("CM_BUILD_ID mismatch on %s: got %q want %q",
				c.WorkflowID, c.Variables["CM_BUILD_ID"], build.ID.String())
		}
		if c.Variables["CM_PROJECT_ID"] != f.project.ID.String() {
			t.Errorf("CM_PROJECT_ID mismatch on %s: got %q want %q",
				c.WorkflowID, c.Variables["CM_PROJECT_ID"], f.project.ID.String())
		}
	}
}

// TestBuildExpoMobile_RejectsNonMobileNative verifies that calling with an
// `app`-type project errors before any DB writes.
func TestBuildExpoMobile_RejectsNonMobileNative(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()
	f.project.ProjectType = models.ProjectTypeApp // mutate to wrong type

	_, _, err := f.c.BuildExpoMobile(ctx, f.project, f.user, ExpoBuildOptions{
		Platforms: []string{"android"},
	})
	if err == nil {
		t.Fatal("expected error for non-mobile_app_native project, got nil")
	}
	if !strings.Contains(err.Error(), "mobile_app_native") {
		t.Errorf("error = %q, want substring 'mobile_app_native'", err.Error())
	}

	// Ensure no Build / AppBuildTask / shadow Project rows were created.
	count, _ := f.db.DB.NewSelect().Model((*models.Build)(nil)).Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 Build rows, got %d", count)
	}
	count, _ = f.db.DB.NewSelect().Model((*models.AppBuildTask)(nil)).Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 AppBuildTask rows, got %d", count)
	}
	count, _ = f.db.DB.NewSelect().Model((*models.Project)(nil)).Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 Project rows, got %d", count)
	}
	if got := len(f.cmRec.snapshot()); got != 0 {
		t.Errorf("expected 0 codemagic calls, got %d", got)
	}
}

// TestBuildExpoMobile_RejectsWhenAndroidPermissionMissing verifies the
// per-platform CanBuildToAndroid gate aborts the call when the user's plan
// lacks Android permission, before any DB writes or codemagic calls.
func TestBuildExpoMobile_RejectsWhenAndroidPermissionMissing(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	// Wire a subscription with CanBuildToAndroid=false so GetUserLimits
	// returns a populated limits struct that should reject the call.
	seedSubscriptionLimits(t, f.db, f.org, &models.StandardUserLimits{
		CanBuildToAndroid: false,
		CanBuildToIOS:     true,
		CanBuildMobile:    true,
	})

	_, _, err := f.c.BuildExpoMobile(ctx, f.project, f.user, ExpoBuildOptions{
		Platforms: []string{"android"},
	})
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "android") {
		t.Errorf("error = %q, want substring 'android'", err.Error())
	}

	// No DB writes should have happened.
	count, _ := f.db.DB.NewSelect().Model((*models.Build)(nil)).Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 Build rows, got %d", count)
	}
	count, _ = f.db.DB.NewSelect().Model((*models.AppBuildTask)(nil)).Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 AppBuildTask rows, got %d", count)
	}
	count, _ = f.db.DB.NewSelect().Model((*models.Project)(nil)).Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 Project rows, got %d", count)
	}
	if got := len(f.cmRec.snapshot()); got != 0 {
		t.Errorf("expected 0 codemagic calls, got %d", got)
	}
}

// TestBuildExpoMobile_RejectsConcurrentBuild verifies the in-flight-build
// guard: if a non-Finished AppBuildTask already exists for this AppProject
// (direct FK scope, no v1 shadow), a second BuildExpoMobile call must fail
// with "already in progress".
func TestBuildExpoMobile_RejectsConcurrentBuild(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	// Pre-insert a Build + non-Finished AppBuildTask scoped by AppProject.
	pendingBuild, err := f.db.CreateProjectBuild(&models.Build{
		AppProjectID:  f.project.ID,
		BuildNumber:   1,
		VersionString: "0.0.1",
		Title:         "prior",
		Icon:          "",
		SplashScreenColor: "#fff",
		NavBarStyle:   "default",
	})
	if err != nil {
		t.Fatalf("create prior build: %v", err)
	}
	if _, err := f.db.CreateAppBuildTask(*pendingBuild, f.project.ID, "Android", "Building", false); err != nil {
		t.Fatalf("create pending task: %v", err)
	}

	// Now attempt a new build — should fail.
	_, _, err = f.c.BuildExpoMobile(ctx, f.project, f.user, ExpoBuildOptions{
		Platforms: []string{"android"},
	})
	if err == nil {
		t.Fatal("expected concurrent-build error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "already in progress") {
		t.Errorf("error = %q, want substring 'already in progress'", err.Error())
	}

	// Ensure no NEW Build was created beyond the pre-existing one.
	count, _ := f.db.DB.NewSelect().Model((*models.Build)(nil)).Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 pre-existing Build row, got %d", count)
	}
	if got := len(f.cmRec.snapshot()); got != 0 {
		t.Errorf("expected 0 codemagic calls, got %d", got)
	}
	// And no v1 shadow Project row should have appeared.
	assertNoExpoShadowProject(t, f.db, f.project.Slug)
}

// assertNoExpoShadowProject fails the test if any v1 Project row exists with
// URL = "expo://<slug>" (the legacy shadow-Project marker that Task 2 dropped).
func assertNoExpoShadowProject(t *testing.T, db *database.Database, slug string) {
	t.Helper()
	ctx := context.Background()
	var shadowCount int
	if err := db.DB.NewRaw(
		"SELECT COUNT(*) FROM projects WHERE url = ? AND deleted_at IS NULL",
		"expo://"+slug,
	).Scan(ctx, &shadowCount); err != nil {
		t.Fatalf("count shadow projects: %v", err)
	}
	if shadowCount != 0 {
		t.Fatalf("expected no expo:// shadow Project row, got %d", shadowCount)
	}
}

// TestBuildExpoMobile_iOSPermissionsEmitCMPermEnv verifies that iOS builds
// emit CM_PERM_<KEY> env vars rendered from MobileAppConfig.Permissions[].
// The codemagic.yaml step then writes these into Info.plist's
// NS*UsageDescription keys — required for App Store review (P0 #6,
// 2026-05-02 mobile audit).
func TestBuildExpoMobile_iOSPermissionsEmitCMPermEnv(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	// Seed a MobileAppConfig with a couple of sensitive iOS perms + an
	// Android-only "notifications" entry that should be silently dropped.
	if _, err := f.c.DB.UpsertMobileAppConfig(&models.MobileAppConfig{
		ProjectID:   f.project.ID,
		Permissions: []string{"camera", "photo_library", "notifications"},
	}); err != nil {
		t.Fatalf("upsert mobile config: %v", err)
	}

	build, _, err := f.c.BuildExpoMobile(ctx, f.project, f.user, ExpoBuildOptions{
		Platforms:   []string{"ios"},
		AppName:     "Acme",
		Icon:        "icons/x.png",
		SplashColor: "#000000",
	})
	if err != nil {
		t.Fatalf("BuildExpoMobile: %v", err)
	}
	if build == nil {
		t.Fatal("expected build, got nil")
	}

	calls := f.cmRec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 codemagic call, got %d", len(calls))
	}
	got := calls[0].Variables

	if camera := got["CM_PERM_CAMERA"]; camera == "" {
		t.Error("CM_PERM_CAMERA should be set when Permissions[]=[camera]")
	} else if !strings.Contains(camera, "Acme") || !strings.Contains(camera, "camera") {
		t.Errorf("CM_PERM_CAMERA = %q, want substring 'Acme' + 'camera'", camera)
	}
	if photo := got["CM_PERM_PHOTO_LIBRARY"]; photo == "" {
		t.Error("CM_PERM_PHOTO_LIBRARY should be set when Permissions[]=[photo_library]")
	}
	// Notifications has no NS key — must NOT be emitted.
	if _, ok := got["CM_PERM_NOTIFICATIONS"]; ok {
		t.Errorf("CM_PERM_NOTIFICATIONS should not be emitted, got %q", got["CM_PERM_NOTIFICATIONS"])
	}
	// A perm we didn't declare must not leak.
	if _, ok := got["CM_PERM_BLUETOOTH"]; ok {
		t.Errorf("CM_PERM_BLUETOOTH should not be emitted (not in declared list), got %q", got["CM_PERM_BLUETOOTH"])
	}
}

// TestBuildExpoMobile_iOSNoConfigEmitsNoPermEnv verifies the safe-default
// behavior: when MobileAppConfig is missing entirely, no CM_PERM_* env vars
// are emitted (the legacy bundled app.json values stay intact).
func TestBuildExpoMobile_iOSNoConfigEmitsNoPermEnv(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	build, _, err := f.c.BuildExpoMobile(ctx, f.project, f.user, ExpoBuildOptions{
		Platforms:   []string{"ios"},
		AppName:     "NoCfg",
		Icon:        "icons/x.png",
		SplashColor: "#000000",
	})
	if err != nil {
		t.Fatalf("BuildExpoMobile: %v", err)
	}
	if build == nil {
		t.Fatal("expected build, got nil")
	}

	calls := f.cmRec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 codemagic call, got %d", len(calls))
	}
	for k := range calls[0].Variables {
		if strings.HasPrefix(k, "CM_PERM_") {
			t.Errorf("expected no CM_PERM_* without MobileAppConfig, got %s=%q", k, calls[0].Variables[k])
		}
	}
}

// TestTriggerCodemagicOTAPublish_NoShadowProject pins the Task 2 invariant:
// the OTA Quick-Publish path must NOT create an "expo://<slug>" v1 Project
// row. The Build/AppBuildTask rows it produces should carry the new direct
// AppProject FK.
func TestTriggerCodemagicOTAPublish_NoShadowProject(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	build, task, err := f.c.TriggerCodemagicOTAPublish(ctx, f.project, f.user)
	if err != nil {
		t.Fatalf("TriggerCodemagicOTAPublish: %v", err)
	}
	if build == nil || task == nil {
		t.Fatalf("expected build+task, got build=%v task=%v", build, task)
	}
	if build.AppProjectID != f.project.ID {
		t.Errorf("build.AppProjectID = %v, want %s", build.AppProjectID, f.project.ID)
	}
	if task.AppProjectID != f.project.ID {
		t.Errorf("task.AppProjectID = %v, want %s", task.AppProjectID, f.project.ID)
	}
	assertNoExpoShadowProject(t, f.db, f.project.Slug)

	// Codemagic should have been called once with the expo-ota-publish workflow.
	calls := f.cmRec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 codemagic call, got %d", len(calls))
	}
	if calls[0].WorkflowID != codemagic.WorkflowExpoOTAPublish {
		t.Errorf("workflowID = %q, want %q", calls[0].WorkflowID, codemagic.WorkflowExpoOTAPublish)
	}
}

// recordedKeys is a debug helper for the s3 PUT recorder.
func recordedKeys(r *s3PutRecorder) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.objects))
	for k := range r.objects {
		keys = append(keys, k)
	}
	return keys
}
