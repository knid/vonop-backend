package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/go-chi/httplog/v3"
	"github.com/knid/wemob/internal/bootstrap"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/database"
	appMiddleware "github.com/knid/wemob/internal/middleware"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/observability"
	"github.com/knid/wemob/internal/services/mcpserver"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/views"
	"github.com/markbates/goth"
	"github.com/markbates/goth/providers/google"
	"github.com/redis/go-redis/v9"
	"github.com/stripe/stripe-go/v82"
	"github.com/uptrace/bun"
)

func createSchema(db *bun.DB) error {
	models := []interface{}{
		(*models.User)(nil),
		(*models.UserNotifications)(nil),
		(*models.Token)(nil),
		(*models.UserActionToken)(nil),
		(*models.StandardUserLimits)(nil),
		(*models.StandardUserSubscription)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		// <P1#16> releases (replace stub) — promoted to a full release model
		// covering uploaded → in_review → approved → live → rejected lifecycle.
		(*models.Release)(nil),
		(*models.Device)(nil),
		(*models.Notification)(nil),
		(*models.Session)(nil),
		(*models.AppUsageEvent)(nil),
		(*models.AppBuildTask)(nil),
		(*models.Ticket)(nil),
		(*models.NavBarItems)(nil),
		(*models.JSInjectData)(nil),
		(*models.GooglePlayConnection)(nil),
		(*models.AppleConnection)(nil),
		(*models.DeploymentEvent)(nil),
		(*models.AICreditsBalance)(nil),
		(*models.AICreditTransaction)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.OrganizationInvite)(nil),
		(*models.APIKey)(nil),
		(*models.ReferralCode)(nil),
		(*models.Referral)(nil),
		(*models.WebhookEndpoint)(nil),
		// V2 app builder models
		(*models.AppProject)(nil),
		(*models.AppDeployment)(nil),
		(*models.AppIntegration)(nil),
		(*models.MCPConnection)(nil),
		(*models.MCPConnectionTool)(nil),
		(*models.AgentMCPGrant)(nil),
		(*models.MCPToolApproval)(nil),
		(*models.AgentMCPToolCall)(nil),
		(*models.AppConversation)(nil),
		// Agent rewrite: chat messages + agent run tracking
		(*models.AppChatMessage)(nil),
		(*models.AppAgentRun)(nil),
		// AppGeneratedFile and AppSnapshot removed — files now in git repos
		(*models.RuntimeApp)(nil),
		(*models.AppDomain)(nil),
		// Custom environment variables
		(*models.AppEnvVar)(nil),
		// User-uploaded assets (3D models, fonts, media, etc.)
		(*models.AppAsset)(nil),
		// LLM token usage tracking
		(*models.LLMTokenUsage)(nil),
		// Background jobs
		(*models.GenerationJob)(nil),
		// Dashboard analytics
		(*models.AppDashboard)(nil),
		(*models.AppDashboardPanel)(nil),
		// Web analytics (deployed sites)
		(*models.WebAnalyticsEvent)(nil),
		// Notification tasks
		(*models.AppNotificationTask)(nil),
		// Mobile app config (feature flags, plugin toggles, bridge settings)
		(*models.MobileAppConfig)(nil),
		// Compose-push audit log (P2.2: dashboard-driven push notifications for mobile_app_native)
		(*models.ComposePushHistory)(nil),
		// Preview codes for Try With Wemob
		(*models.PreviewCode)(nil),
		// GitHub export tracking
		(*models.AppGitHubExport)(nil),
		// Stripe webhook deduplication
		(*models.WebhookEvent)(nil),
		// AI project memory (persists design prefs, decisions across sessions)
		(*models.AppProjectMemory)(nil),
		// Template marketplace
		(*models.AppTemplate)(nil),
		// AI gateway per-project keys
		(*models.ProjectGatewayKey)(nil),
		// Persistent store listing per AppProject (replaces per-wizard form state)
		(*models.AppStoreListing)(nil),
		// <P1#15> app_store_listing_localizations — per-locale title/desc/keywords/screenshots
		(*models.AppStoreListingLocalization)(nil),
		// Known build failure patterns for human-readable error surfacing
		(*models.BuildAnnotation)(nil),
		// Expo build event stream (append-only, idempotent by task+message_hash)
		(*models.BuildEvent)(nil),
		// Append-only validation outcome per Apple/Google Play connection
		(*models.ConnectionValidation)(nil),
		// Encrypted Android keystore + key.properties (replaces plaintext S3 path).
		(*models.AndroidKeystore)(nil),
		// Self-hosted expo-updates manifests for mobile_app_native projects (P2.1).
		(*models.OTAUpdate)(nil),
		// Per-project OTA manifest signing keys (P0 #8a — code-signed updates).
		(*models.OTAUpdateSigningKey)(nil),
		// <P1#14> apple_signing_assets — auto-generated iOS distribution
		// cert + provisioning profile per project (P1 #14, mobile audit).
		(*models.AppleSigningAssets)(nil),
		// <P2#25> mobile_subscriptions — RC v2 webhook landing zone for
		// native subscription state. Backend mirror of "active subscribers"
		// for dashboard panels + paywall enforcement on backend resources.
		(*models.MobileSubscription)(nil),
		// E3 Phase 7 — per-org bandwidth tally, populated by the daily Fly
		// GraphQL sync cron and read by the deploy gate.
		(*models.AppBandwidthUsage)(nil),
	}

	for _, model := range models {
		_, err := db.NewCreateTable().
			Model(model).
			IfNotExists().
			Exec(context.Background())
		if err != nil {
			return err
		}
	}

	return nil
}

func seedPlans(db *database.Database, sm secret.SecretManager) {
	type planDef struct {
		Name          string
		Description   string
		MonthlyPrice  int
		PriceID       string
		AnnualPrice   int
		AnnualPriceID string
		Limits        models.StandardUserLimits
	}

	plans := []planDef{
		{
			Name:         "Starter",
			Description:  "Free tier — try Wemob with no commitment",
			MonthlyPrice: 0,
			PriceID:      "free_plan",
			Limits: models.StandardUserLimits{
				MaximumCredentialCount:    2,
				MontlyBuildCount:          0,
				MontlyNotificationCount:   3,
				MonthlyAICredits:          50,
				MaxSeats:                  1,
				MaxWebApps:                1,
				MaxMobileApps:             1,
				MaxDeployedWebApps:        1,
				MaxDeployedMobileApps:     0,
				CanDeployMobile:           false,
				DeployMemoryMB:            256,
				DeployCPUs:                "1",
				DeployDBOrgTier:           "free",
				DeployDBMaxCU:             "1",
				DeployDBStorageMB:         512,
				DeployDBActiveTimeSec:     360000,
				DeployDBDataTransferBytes: 5 * 1024 * 1024 * 1024,
				DeployDBSuspendTimeoutSec: 300,
				DeployDBComputeTimeSec:    0,
				MinMachinesRunning:        0,
				MonthlyBandwidthGB:        10,
				MonthlyOTAPublishCount:    0,
				CanChooseRegion:           false,
				CreditRolloverMonths:      0,
				CanBuildToAndroid:         true,
				CanBuildToIOS:             true,
				CanBuildMobile:            false,
				CanUseAnalytics:           false,
				CanAccessCode:             false,
				CanBuyDomain:              false,
				CanUseEmailService:        false,
				CanUseEcommerce:           true,
				CanExportToGitHub:         false,
				CanDeployWeb:              true,
				CanRemoveBrandLogo:        false,
			},
		},
		{
			Name:          "Hobby",
			Description:   "For solo builders shipping their first apps",
			MonthlyPrice:  1900,
			PriceID:       sm.GetSecret("STRIPE_HOBBY_PRICE_ID"),
			AnnualPrice:   18200,
			AnnualPriceID: sm.GetSecret("STRIPE_HOBBY_ANNUAL_PRICE_ID"),
			Limits: models.StandardUserLimits{
				MaximumCredentialCount:    2,
				MontlyBuildCount:          0,
				MontlyNotificationCount:   50,
				MonthlyAICredits:          200,
				MaxSeats:                  2,
				MaxWebApps:                20,
				MaxMobileApps:             1,
				MaxDeployedWebApps:        3,
				MaxDeployedMobileApps:     1,
				CanDeployMobile:           true,
				DeployMemoryMB:            512,
				DeployCPUs:                "1",
				DeployDBOrgTier:           "paid",
				DeployDBMaxCU:             "1",
				DeployDBStorageMB:         2048,
				DeployDBActiveTimeSec:     360000,
				DeployDBDataTransferBytes: 25 * 1024 * 1024 * 1024,
				DeployDBSuspendTimeoutSec: 300,
				DeployDBComputeTimeSec:    180000,
				MinMachinesRunning:        0,
				MonthlyBandwidthGB:        100,
				MonthlyOTAPublishCount:    0,
				CanChooseRegion:           false,
				CreditRolloverMonths:      1,
				CanBuildToAndroid:         true,
				CanBuildToIOS:             true,
				CanBuildMobile:            true,
				CanUseAnalytics:           true,
				CanAccessCode:             true,
				CanBuyDomain:              true,
				CanUseEmailService:        false,
				CanUseEcommerce:           true,
				CanExportToGitHub:         true,
				CanDeployWeb:              true,
				CanRemoveBrandLogo:        true,
			},
		},
		{
			Name:          "Pro",
			Description:   "For freelancers and small teams shipping production apps",
			MonthlyPrice:  3900,
			PriceID:       sm.GetSecret("STRIPE_PRO_PRICE_ID"),
			AnnualPrice:   37400,
			AnnualPriceID: sm.GetSecret("STRIPE_PRO_ANNUAL_PRICE_ID"),
			Limits: models.StandardUserLimits{
				MaximumCredentialCount:    10,
				MontlyBuildCount:          0,
				MontlyNotificationCount:   200,
				MonthlyAICredits:          500,
				MaxSeats:                  5,
				MaxWebApps:                100,
				MaxMobileApps:             5,
				MaxDeployedWebApps:        5,
				MaxDeployedMobileApps:     3,
				CanDeployMobile:           true,
				DeployMemoryMB:            512,
				DeployCPUs:                "1",
				DeployDBOrgTier:           "paid",
				DeployDBMaxCU:             "1",
				DeployDBStorageMB:         10240,
				DeployDBActiveTimeSec:     720000,
				DeployDBDataTransferBytes: 100 * 1024 * 1024 * 1024,
				DeployDBSuspendTimeoutSec: 60,
				DeployDBComputeTimeSec:    360000,
				MinMachinesRunning:        0,
				MonthlyBandwidthGB:        250,
				MonthlyOTAPublishCount:    0,
				CanChooseRegion:           true,
				CreditRolloverMonths:      1,
				CanBuildToAndroid:         true,
				CanBuildToIOS:             true,
				CanBuildMobile:            true,
				CanUseAnalytics:           true,
				CanAccessCode:             true,
				CanBuyDomain:              true,
				CanUseEmailService:        true,
				CanUseEcommerce:           true,
				CanExportToGitHub:         true,
				CanDeployWeb:              true,
				CanRemoveBrandLogo:        true,
			},
		},
		{
			Name:          "Max",
			Description:   "For agencies and teams running multiple production apps",
			MonthlyPrice:  6900,
			PriceID:       sm.GetSecret("STRIPE_MAX_PRICE_ID"),
			AnnualPrice:   66200,
			AnnualPriceID: sm.GetSecret("STRIPE_MAX_ANNUAL_PRICE_ID"),
			Limits: models.StandardUserLimits{
				MaximumCredentialCount:    25,
				MontlyBuildCount:          0,
				MontlyNotificationCount:   500,
				MonthlyAICredits:          1000,
				MaxSeats:                  10,
				MaxWebApps:                200,
				MaxMobileApps:             10,
				MaxDeployedWebApps:        10,
				MaxDeployedMobileApps:     5,
				CanDeployMobile:           true,
				DeployMemoryMB:            512,
				DeployCPUs:                "1",
				DeployDBOrgTier:           "paid",
				DeployDBMaxCU:             "2",
				DeployDBStorageMB:         20480,
				DeployDBActiveTimeSec:     1080000,
				DeployDBDataTransferBytes: 200 * 1024 * 1024 * 1024,
				DeployDBSuspendTimeoutSec: 60,
				DeployDBComputeTimeSec:    360000,
				MinMachinesRunning:        0,
				MonthlyBandwidthGB:        500,
				MonthlyOTAPublishCount:    0,
				CanChooseRegion:           true,
				CreditRolloverMonths:      2,
				CanBuildToAndroid:         true,
				CanBuildToIOS:             true,
				CanBuildMobile:            true,
				CanUseAnalytics:           true,
				CanAccessCode:             true,
				CanBuyDomain:              true,
				CanUseEmailService:        true,
				CanUseEcommerce:           true,
				CanExportToGitHub:         true,
				CanDeployWeb:              true,
				CanRemoveBrandLogo:        true,
			},
		},
	}

	for _, p := range plans {
		// First check including soft-deleted rows to prevent unique constraint violations
		existing, err := db.GetSubscriptionByNameIncludeDeleted(p.Name)
		if err != nil {
			// Plan truly doesn't exist — create limits then subscription
			limits := p.Limits
			createdLimits, err := db.CreateStandardUserLimits(&limits)
			if err != nil {
				slog.Error("failed to create limits for plan", "plan", p.Name, "error", err)
				continue
			}
			sub := &models.StandardUserSubscription{
				Name:          p.Name,
				Description:   p.Description,
				LimitsID:      createdLimits.ID,
				MonthlyPrice:  p.MonthlyPrice,
				PriceID:       p.PriceID,
				AnnualPrice:   p.AnnualPrice,
				AnnualPriceID: p.AnnualPriceID,
			}
			if _, err := db.CreateSubscription(sub); err != nil {
				slog.Error("failed to create subscription plan", "plan", p.Name, "error", err)
				continue
			}
			slog.Info("created subscription plan", "plan", p.Name)
			continue
		}

		// If plan was soft-deleted, restore it
		if !existing.DeletedAt.IsZero() {
			existing.DeletedAt = time.Time{}
			slog.Info("restoring soft-deleted subscription plan", "plan", p.Name)
		}

		// Plan exists — update limits
		limits, err := db.GetStandardUserLimitsById(existing.LimitsID.String())
		if err != nil {
			slog.Error("failed to get limits for plan", "plan", p.Name, "error", err)
			continue
		}

		limits.MaximumCredentialCount = p.Limits.MaximumCredentialCount
		limits.MontlyBuildCount = p.Limits.MontlyBuildCount
		limits.MontlyNotificationCount = p.Limits.MontlyNotificationCount
		limits.MonthlyAICredits = p.Limits.MonthlyAICredits
		limits.MaxSeats = p.Limits.MaxSeats
		limits.MaxWebApps = p.Limits.MaxWebApps
		limits.MaxMobileApps = p.Limits.MaxMobileApps
		limits.MaxDeployedWebApps = p.Limits.MaxDeployedWebApps
		limits.MaxDeployedMobileApps = p.Limits.MaxDeployedMobileApps
		limits.CanDeployMobile = p.Limits.CanDeployMobile
		limits.DeployMemoryMB = p.Limits.DeployMemoryMB
		limits.DeployCPUs = p.Limits.DeployCPUs
		limits.DeployDBOrgTier = p.Limits.DeployDBOrgTier
		limits.DeployDBMaxCU = p.Limits.DeployDBMaxCU
		limits.DeployDBStorageMB = p.Limits.DeployDBStorageMB
		limits.DeployDBActiveTimeSec = p.Limits.DeployDBActiveTimeSec
		limits.DeployDBDataTransferBytes = p.Limits.DeployDBDataTransferBytes
		limits.DeployDBSuspendTimeoutSec = p.Limits.DeployDBSuspendTimeoutSec
		limits.DeployDBComputeTimeSec = p.Limits.DeployDBComputeTimeSec
		limits.MinMachinesRunning = p.Limits.MinMachinesRunning
		limits.MonthlyBandwidthGB = p.Limits.MonthlyBandwidthGB
		limits.MonthlyOTAPublishCount = p.Limits.MonthlyOTAPublishCount
		limits.CanChooseRegion = p.Limits.CanChooseRegion
		limits.CreditRolloverMonths = p.Limits.CreditRolloverMonths
		limits.CanBuildToAndroid = p.Limits.CanBuildToAndroid
		limits.CanBuildToIOS = p.Limits.CanBuildToIOS
		limits.CanBuildMobile = p.Limits.CanBuildMobile
		limits.CanUseAnalytics = p.Limits.CanUseAnalytics
		limits.CanAccessCode = p.Limits.CanAccessCode
		limits.CanBuyDomain = p.Limits.CanBuyDomain
		limits.CanUseEmailService = p.Limits.CanUseEmailService
		limits.CanUseEcommerce = p.Limits.CanUseEcommerce
		limits.CanExportToGitHub = p.Limits.CanExportToGitHub
		limits.CanDeployWeb = p.Limits.CanDeployWeb
		limits.CanRemoveBrandLogo = p.Limits.CanRemoveBrandLogo

		if _, err := db.UpdateStandardUserLimits(limits); err != nil {
			slog.Error("failed to update limits for plan", "plan", p.Name, "error", err)
		}

		// Update subscription price IDs if changed
		existing.PriceID = p.PriceID
		existing.AnnualPriceID = p.AnnualPriceID
		existing.MonthlyPrice = p.MonthlyPrice
		existing.AnnualPrice = p.AnnualPrice
		existing.Description = p.Description
		if _, err := db.UpdateSubscription(existing); err != nil {
			slog.Error("failed to update subscription plan", "plan", p.Name, "error", err)
		}

		slog.Info("updated subscription plan", "plan", p.Name)
	}

}

// isSensitiveLogPath returns true if a request URL path is known to carry
// secrets, credentials, or signed third-party payloads that must never land
// in BetterStack. Used by httplog LogExtraAttrs to skip body capture on
// auth and webhook routes even when the status is 4xx/5xx.
func isSensitiveLogPath(path string) bool {
	lower := strings.ToLower(path)
	prefixes := []string{
		"/webhooks/",      // Stripe, Iyzico, Coolify — all signed, all contain PII
		"/v2/webhooks/",   // <P1#16> ASC notifications + Play RTDN
		"/v1/auth/",       // login / signup / password reset
		"/v1/password/",   // password reset
		"/v1/login",       // plain login
		"/v1/signup",      // plain signup
		"/v1/register",    // alt signup
		"/v1/billing/",    // Stripe payloads pass through here
		"/v2/gateway-key", // reveal / rotate — returns raw API key material
	}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func main() {
	controller, _ := bootstrap.BootstrapController()
	langfuseHandle, err := observability.InitLangfuseWithSecrets(context.Background(), controller.SecretManager)
	if err != nil {
		slog.Error("failed to initialize Langfuse OpenTelemetry exporter", "error", err)
		os.Exit(1)
	}
	observability.EmitStartupCanary(context.Background(), "api")
	if err := langfuseHandle.ForceFlush(context.Background()); err != nil {
		slog.Warn("Langfuse startup canary flush failed", "error", err)
	}

	// Idempotent: ensures the keystore-bundles/ prefix expires after 1 day.
	// Failure-soft — log+continue so a missing IAM permission doesn't block startup.
	if err := storage.EnsureKeystoreBundlesLifecycle(context.Background(), controller.AssetsStorage.Client, controller.AssetsStorage.Bucket); err != nil {
		slog.Warn("keystore-bundles lifecycle bootstrap failed (non-fatal)", "err", err)
	}

	// Schema migrations run in main only — workers and migration scripts share
	// the same BootstrapController but each decides independently whether to run
	// createSchema / seedPlans / backfills.
	bundb := bootstrap.BunDB(controller)
	db := controller.DB

	// uuid-ossp is only needed by the API server (schema owner). Worker and
	// migration binaries don't run createSchema, so the extension call lives
	// here rather than in bootstrap.go.
	if _, err := bundb.ExecContext(context.Background(), `CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`); err != nil {
		slog.Error("create extension uuid-ossp failed", "err", err)
		os.Exit(1)
	}
	if err := createSchema(bundb); err != nil {
		slog.Error("Failed to create schema", "error", err)
		os.Exit(1)
	}

	if n, err := db.MarkOrphanedRunsFailed(context.Background(), 0); err != nil {
		slog.Warn("failed to mark orphaned agent runs", "error", err)
	} else if n > 0 {
		slog.Info("marked orphaned agent runs as failed", "count", n)
	}

	if n, err := db.MarkOrphanedGenerationJobs(); err != nil {
		slog.Warn("failed to mark orphaned generation jobs", "error", err)
	} else if n > 0 {
		slog.Info("marked orphaned generation jobs as failed", "count", n)
	}

	if n, err := db.BackfillProjectStatusReady(); err != nil {
		slog.Warn("failed to backfill project status", "error", err)
	} else if n > 0 {
		slog.Info("backfilled draft projects with completed runs to ready", "count", n)
	}

	if err := database.SeedBuildAnnotations(context.Background(), bundb); err != nil {
		slog.Error("seed build annotations failed", "err", err)
	}

	sm := controller.SecretManager

	seedPlans(db, sm)

	go utils.CheckSessionStatusJob(db)
	go utils.CheckUsageAlertsJob(db, controller.EventService)

	// GitHub OAuth secrets (required for GitHub export feature)
	// GITHUB_CLIENT_ID — OAuth App client ID
	// GITHUB_CLIENT_SECRET — OAuth App client secret
	// GITHUB_REDIRECT_URI — OAuth callback URL (e.g. https://api.wemob.io/v2/auth/github/callback)
	// GITHUB_TOKEN_ENCRYPTION_KEY — 64-char hex string for AES-256-GCM token encryption

	goth.UseProviders(
		google.New(sm.GetSecret("GOOGLE_CLIENT_ID"), sm.GetSecret("GOOGLE_SECRET_KEY"), "https://api.wemob.io/v1/users/auth/google/callback", "email", "profile"),
	)

	stripe.Key = sm.GetSecret("STRIPE_API_KEY")

	// Redis client for rate limiting (reuses same Redis as Asynq)
	redisClient := redis.NewClient(&redis.Options{
		Addr:         sm.GetSecret("REDIS_ADDR"),
		Username:     sm.GetSecret("REDIS_USERNAME"),
		Password:     sm.GetSecret("REDIS_PASSWORD"),
		TLSConfig:    &tls.Config{},
		PoolSize:     20,
		MinIdleConns: 5,
	})
	rateLimiter := appMiddleware.NewRateLimiter(redisClient)

	httpViews := views.HttpViews{
		Controller: controller,
	}

	// Fail stale awaiting_input jobs from previous server runs
	if staleCount, err := db.FailStaleAwaitingInputJobs(10 * time.Minute); err != nil {
		slog.Error("Failed to clean up stale awaiting_input jobs", "error", err)
	} else if staleCount > 0 {
		slog.Info("Cleaned up stale awaiting_input jobs", "count", staleCount)
	}

	workDir, _ := os.Getwd()
	staticDir := http.Dir(filepath.Join(workDir, "storage/public"))

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(20 * time.Minute))

	// CORS must run before rate limiter so 429 responses still carry
	// Access-Control-Allow-Origin headers (otherwise browsers report a
	// CORS error instead of showing the real 429).
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://app.wemob.io", "https://dashboard.wemob.io", "https://wemob.io", "https://www.wemob.io", "http://localhost:3000", "http://127.0.0.1:3000", "https://*.wemob.app", "https://*.preview.wemob.app", "https://device1.zebu-pain.ts.net", "https://mba.zebu-pain.ts.net", "http://wemob-backend.zebu-pain.ts.net:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "Location"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Use(rateLimiter.RateLimit(1200, time.Minute, appMiddleware.IPKey))

	r.Use(httplog.RequestLogger(slog.Default(), &httplog.Options{
		// Level defines the verbosity of the request logs:
		// slog.LevelDebug - log all responses (incl. OPTIONS)
		// slog.LevelInfo  - log responses (excl. OPTIONS)
		// slog.LevelWarn  - log 4xx and 5xx responses only (except for 429)
		// slog.LevelError - log 5xx responses only
		Level: slog.LevelInfo,

		Schema:        httplog.SchemaOTEL,
		RecoverPanics: true,
		// Body logging policy:
		//   - Gateway chat/image routes: always log the request body. It's
		//     the core diagnostic signal for "what did the user app send?"
		//     and bodies are small, JSON, and don't carry secrets (the
		//     gateway key is in the Authorization header, which we never
		//     log). Response bodies stay off — chat completions can stream
		//     tens of KB, and upstream errors are already slog.Warn'd at
		//     the application layer with the full body.
		//   - Everything else: bodies are captured only on 4xx/5xx via
		//     LogExtraAttrs below, which lets the library tee the body
		//     into a buffer and emit it as an attribute post-handler.
		LogBodyMaxLen: 2048,
		LogRequestBody: func(req *http.Request) bool {
			return strings.HasPrefix(req.URL.Path, "/v2/gateway/")
		},
		LogResponseBody: func(req *http.Request) bool { return false },
		LogExtraAttrs: func(req *http.Request, reqBody string, status int) []slog.Attr {
			if status < 400 || reqBody == "" {
				return nil
			}
			// Skip routes where the body is known to contain secrets or
			// signed webhook payloads we must not copy into logs.
			if isSensitiveLogPath(req.URL.Path) {
				return nil
			}
			if len(reqBody) > 2048 {
				reqBody = reqBody[:2048] + "...(truncated)"
			}
			return []slog.Attr{slog.String("request.body", reqBody)}
		},
	}))

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			httplog.SetAttrs(ctx, slog.String("from", "httplog"))

			httplog.SetAttrs(ctx,
				slog.String("client.ip", r.RemoteAddr),
				slog.String("client.xff", r.Header.Get("X-Forwarded-For")),
				slog.String("client.xri", r.Header.Get("X-Real-IP")),
			)

			tokenString, err := utils.ExtractTokenFromHeader(r)
			if err == nil && len(tokenString) >= 12 {
				// Token prefix intentionally omitted from logs for security
				userInformationRes, err := httpViews.Controller.GetUser(tokenString)
				if err == nil {
					httplog.SetAttrs(ctx, slog.String("user.id", userInformationRes.ID.String()))
					httplog.SetAttrs(ctx, slog.String("user.email", userInformationRes.Email))
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	r.Use(middleware.Recoverer)
	r.Use(appMiddleware.Locale)

	r.Route("/", func(r chi.Router) {
		mcpServer := mcpserver.New(controller)
		r.Get("/.well-known/oauth-protected-resource", mcpServer.ServeProtectedResourceMetadata)
		r.Get("/.well-known/oauth-protected-resource/mcp", mcpServer.ServeProtectedResourceMetadata)
		r.Get("/.well-known/oauth-authorization-server", mcpServer.ServeAuthorizationServerMetadata)
		r.Get("/.well-known/oauth-authorization-server/mcp", mcpServer.ServeAuthorizationServerMetadata)
		r.HandleFunc("/oauth/register", mcpServer.RegisterOAuthClient)
		r.HandleFunc("/oauth/authorize", mcpServer.Authorize)
		r.HandleFunc("/oauth/token", mcpServer.Token)
		r.Handle("/mcp", mcpServer)
		r.Handle("/mcp/*", mcpServer)

		r.Route("/webhooks", func(r chi.Router) {
			r.Post("/stripe", httpViews.Controller.HandleWebhook)
			// Fly machine state webhook (signature verified inside handler)
			r.Post("/fly", httpViews.HandleFlyWebhook)
		})
		// <P1#16> webhook routes — store-side notifications for Release status.
		// Mounted under /v2/webhooks (not /webhooks) to align with the v2 API
		// surface; signature verification happens inside each handler.
		r.Route("/v2/webhooks", func(r chi.Router) {
			r.Post("/asc-notifications", httpViews.HandleASCNotification)
			r.Post("/play-rtdn", httpViews.HandlePlayRTDN)
			// <P2#25> RevenueCat v2 webhook — bearer-token auth, see
			// internal/views/revenuecat_webhook_view.go. Powers the
			// Subscriptions panel in Studio + dashboard-side churn fires.
			r.Post("/revenuecat", httpViews.HandleRevenueCatWebhook)
		})
		r.Route("/v1", func(r chi.Router) {
			// OAuth begin/callback are pre-auth, so UserKey falls back to IPKey
			// and they share the /v1 bucket. A busy household NAT (or a buggy
			// frontend tab) can fill that bucket and lock every device on the
			// same public IP out of login. Exempt /v1/users/auth/* — the
			// global 1200/min IP limiter at the parent router still applies.
			v1Limiter := rateLimiter.RateLimit(600, time.Minute, appMiddleware.UserKey)
			r.Use(func(next http.Handler) http.Handler {
				wrapped := v1Limiter(next)
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if strings.HasPrefix(req.URL.Path, "/v1/users/auth/") {
						next.ServeHTTP(w, req)
						return
					}
					wrapped.ServeHTTP(w, req)
				})
			})
			r.Handle("/static/*", http.StripPrefix("/v1/static/", http.FileServer(utils.NoDirFileSystem{FS: staticDir})))
			r.Route("/storage", func(r chi.Router) {
				r.Route("/credentials", func(r chi.Router) {
					r.Route("/apple", func(r chi.Router) {
						r.Post("/", httpViews.PutP8KeyToS3Presign)
					})
					r.Route("/google", func(r chi.Router) {
						r.Post("/", httpViews.PutServiceAccountToS3Presign)
					})
				})
				r.Route("/users", func(r chi.Router) {
					r.Get("/me/photo", httpViews.GetProfilePhotoFromS3Presign)
					r.Post("/me/photo", httpViews.PutProfilePhotoToS3Presign)
				})
				r.Route("/notifications", func(r chi.Router) {
					r.Get("/image/{notificationId}", httpViews.GetNotificationImageFromS3Presign)
					r.Post("/image", httpViews.PutNotificationImageToS3Presign)
				})
				r.Route("/deployments", func(r chi.Router) {
					r.Route("/screenshots", func(r chi.Router) {
						r.Get("/{screenshotId}", httpViews.GetScreenshotFromS3Presign)
						r.Post("/", httpViews.PutScreenshotToS3Presign)
					})
					r.Route("/icons", func(r chi.Router) {
						r.Get("/{iconId}", httpViews.GetIconFromS3Presign)
						r.Post("/", httpViews.PutIconToS3Presign)
					})
				})
				r.Route("/builds", func(r chi.Router) {
					r.Get("/aab/{buildId}", httpViews.DownloadAABFromS3Presign)
					r.Get("/ipa/{buildId}", httpViews.DownloadIPAFromS3Presign)
				})
				r.Route("/projects", func(r chi.Router) {
					r.Route("/icons", func(r chi.Router) {
						r.Get("/{iconId}", httpViews.GetIconFromS3Presign)
						r.Post("/", httpViews.PutIconToS3Presign)
					})
				})
			})
			r.Get("/check/{projectId}", httpViews.CheckStatus)
			r.Route("/users", func(r chi.Router) {
				r.Route("/me", func(r chi.Router) {
					r.Get("/", httpViews.GetUser)
					r.Put("/", httpViews.UpdateUser)
					r.Put("/language", httpViews.UpdateUserLanguage)
					r.Post("/setup-finished", httpViews.FinishSetup)
					r.Delete("/", httpViews.DeleteUser)

					r.Get("/usage", httpViews.GetMontlyUsage)
				})

				r.Get("/notifications", httpViews.GetUserNotifications)
				r.Get("/notifications/{userNotificationId}", httpViews.GetUserNotification)

				r.Post("/login", httpViews.UserLogin)
				r.Post("/register", httpViews.UserRegister)

				// Self-service password reset (public, token-gated inside).
				r.Post("/forgot-password", httpViews.UserForgotPassword)
				r.Post("/reset-password", httpViews.UserResetPassword)

				// Email verification. /send requires auth, /confirm is public
				// because the email token is the authenticator.
				r.With(appMiddleware.RequireAuth(controller)).Post("/verify-email/send", httpViews.UserSendVerifyEmail)
				r.Post("/verify-email/confirm", httpViews.UserConfirmVerifyEmail)

				r.Get("/auth/{provider}/callback", httpViews.OAuthCallback)
				r.Get("/auth/{provider}", httpViews.OAuthBegin)
				r.Post("/auth/google/mobile", httpViews.GoogleMobileLogin)
			})
			r.Get("/tasks", httpViews.GetAllTasks)
			r.Post("/tasks/{taskId}/cancel", httpViews.CancelBuildTask)
			r.Get("/ship", httpViews.GetShipItems)
			r.Route("/projects", func(r chi.Router) {
				r.Get("/", httpViews.GetProjects)
				r.Route("/{projectId}", func(r chi.Router) {
					r.Get("/", httpViews.GetProject)
					r.Delete("/", httpViews.DeleteProject)
					r.Route("/analytics", func(r chi.Router) {
						r.Route("/events", func(r chi.Router) {
							r.Get("/", httpViews.GetProjectEvents)
							r.With(rateLimiter.RateLimit(120, time.Minute, appMiddleware.IPKey)).Post("/", httpViews.LogAppEvent)
						})
						r.Route("/session", func(r chi.Router) {
							r.With(rateLimiter.RateLimit(30, time.Minute, appMiddleware.IPKey)).Post("/", httpViews.CreateSession)
						})
						r.Get("/devices", httpViews.GetAllDevices)
						r.Get("/active-users", httpViews.GetActiveDevices)
						r.Get("/unique-users", httpViews.GetUniqueDevices)
						r.Get("/sessions", httpViews.GetSessions)
					})
					r.Route("/notifications", func(r chi.Router) {
						r.Get("/", httpViews.GetProjectNotifications)
						r.Post("/", httpViews.SendProjectNotification)
						r.Get("/{notificationId}", httpViews.GetProjectNotification)
					})
					r.Route("/public", func(r chi.Router) {
						r.Route("/navbar", func(r chi.Router) {
							r.Get("/items", httpViews.GetNavBarItems)
							r.Get("/color", httpViews.GetNavBarItemsBackground)
						})
						r.Route("/injection", func(r chi.Router) {
							r.Get("/js", httpViews.GetJSInjectionData)
						})
					})
					r.Route("/keystore/download", func(r chi.Router) {
						r.Post("/request", httpViews.RequestKeystoreDownload)
						r.Post("/redeem", httpViews.RedeemKeystoreDownload)
					})
				})
				r.Route("/autofill", func(r chi.Router) {
					r.Post("/navbar", httpViews.NavBarAutoFillWithAI)
				})
			})
			// New route names (preferred from Phase 1 onward)
			r.Route("/connections", func(r chi.Router) {
				r.Route("/google-play", func(r chi.Router) {
					r.Get("/", httpViews.ListGooglePlayConnections)
					r.Post("/", httpViews.CreateGooglePlayConnection)
					r.Post("/validate", httpViews.ValidateGooglePlayConnection)
					r.Route("/{connectionId}", func(r chi.Router) {
						r.Get("/", httpViews.GetGooglePlayConnection)
						r.Put("/", httpViews.UpdateGooglePlayConnection)
						r.Delete("/", httpViews.DeleteGooglePlayConnection)
					})
				})
				r.Route("/apple", func(r chi.Router) {
					r.Get("/", httpViews.ListAppleConnections)
					r.Post("/", httpViews.CreateAppleConnection)
					r.Post("/validate", httpViews.ValidateAppleConnection)
					r.Route("/{connectionId}", func(r chi.Router) {
						r.Get("/", httpViews.GetAppleConnection)
						r.Put("/", httpViews.UpdateAppleConnection)
						r.Delete("/", httpViews.DeleteAppleConnection)
					})
				})
			})

			// Legacy aliases — kept for 2 releases so existing frontend keeps working.
			// Remove after Phase 2 ships the updated dashboard.
			r.Route("/credentials", func(r chi.Router) {
				r.Route("/google", func(r chi.Router) {
					r.Get("/", httpViews.ListGooglePlayConnections)
					r.Post("/", httpViews.CreateGooglePlayConnection)
					r.Route("/{credentialId}", func(r chi.Router) {
						r.Get("/", httpViews.GetGooglePlayConnection)
						r.Put("/", httpViews.UpdateGooglePlayConnection)
						r.Delete("/", httpViews.DeleteGooglePlayConnection)
					})
				})
				r.Route("/appstore", func(r chi.Router) {
					r.Get("/", httpViews.ListAppleConnections)
					r.Post("/", httpViews.CreateAppleConnection)
					r.Route("/{credentialId}", func(r chi.Router) {
						r.Get("/", httpViews.GetAppleConnection)
						r.Put("/", httpViews.UpdateAppleConnection)
						r.Delete("/", httpViews.DeleteAppleConnection)
					})
				})
			})
			r.Route("/billing", func(r chi.Router) {
				r.Get("/plans", httpViews.GetBillingPlans)
				r.Get("/gateway-models", httpViews.GetBillingGatewayModels)
				r.Post("/checkout", httpViews.StripeCheckout)
				r.Route("/subscription", func(r chi.Router) {
					r.Post("/portal", httpViews.StripeCustomerPortal)
					r.Get("/", httpViews.GetSubscription)
					r.Put("/", httpViews.UpdateSubscription)
					r.Delete("/", httpViews.CancelSubscription)
					r.Get("/limits", httpViews.GetLimits)
				})
				r.Route("/credits", func(r chi.Router) {
					r.Get("/", httpViews.GetAICreditsBalance)
					r.Get("/breakdown", httpViews.GetCreditBreakdown)
					r.Get("/transactions", httpViews.GetAICreditTransactions)
					r.Get("/history", httpViews.GetCreditHistory)
					r.Get("/packages", httpViews.GetCreditPackages)
					r.Post("/purchase", httpViews.PurchaseCredits)
				})
				r.Get("/token-usage", httpViews.GetTokenUsage)
			})

			r.Route("/organizations", func(r chi.Router) {
				r.Get("/", httpViews.GetUserOrganizations)
				r.Post("/", httpViews.CreateOrganization)
				r.Post("/invites/{token}/accept", httpViews.AcceptOrganizationInvite)
				r.Route("/{orgId}", func(r chi.Router) {
					r.Get("/", httpViews.GetOrganization)
					r.Put("/", httpViews.UpdateOrganization)
					r.Delete("/", httpViews.DeleteOrganization)
					r.Get("/credits", httpViews.GetOrgCreditsForAdmin)
					r.Get("/credits/me", httpViews.GetOrgCreditsForMember)
					r.Get("/credits/projects", httpViews.GetOrgProjectCredits)
					r.Get("/credits/projects/actions", httpViews.GetOrgProjectActionCredits)
					r.Get("/invites", httpViews.ListPendingInvites)
					r.Delete("/invites/{inviteId}", httpViews.CancelOrganizationInvite)
					r.Post("/transfer-ownership", httpViews.TransferOrganizationOwnership)
					r.Route("/members", func(r chi.Router) {
						r.Get("/", httpViews.GetOrganizationMembers)
						r.Post("/invite", httpViews.InviteOrganizationMember)
						r.Delete("/me", httpViews.LeaveOrganization)
						r.Route("/{memberId}", func(r chi.Router) {
							r.Put("/role", httpViews.UpdateMemberRole)
							r.Patch("/role", httpViews.UpdateMemberRole)
							r.Delete("/", httpViews.RemoveOrganizationMember)
							r.Patch("/credit-cap", httpViews.SetMemberCreditCap)
						})
					})
				})
			})
			r.Route("/api-keys", func(r chi.Router) {
				r.Get("/", httpViews.ListAPIKeys)
				r.Post("/", httpViews.CreateAPIKey)
				r.Delete("/{keyId}", httpViews.DeleteAPIKey)
			})
			r.Route("/referrals", func(r chi.Router) {
				r.Get("/code", httpViews.GetReferralCode)
				r.Get("/stats", httpViews.GetReferralStats)
				r.Post("/apply", httpViews.ApplyReferralCode)
			})
			r.Route("/webhooks", func(r chi.Router) {
				r.Get("/", httpViews.ListWebhookEndpoints)
				r.Post("/", httpViews.CreateWebhookEndpoint)
				r.Route("/{webhookId}", func(r chi.Router) {
					r.Put("/", httpViews.UpdateWebhookEndpoint)
					r.Delete("/", httpViews.DeleteWebhookEndpoint)
				})
			})
			r.Route("/admin", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireAdmin())

				r.Get("/dashboard", httpViews.GetAdminDashboard)

				r.Route("/users", func(r chi.Router) {
					r.Get("/", httpViews.AdminListUsers)
					r.Route("/{userId}", func(r chi.Router) {
						r.Get("/", httpViews.AdminGetUser)
						r.Put("/", httpViews.AdminUpdateUser)
						r.Delete("/", httpViews.AdminDeleteUser)
						r.Post("/impersonate", httpViews.AdminImpersonateUser)
						r.Post("/reset-password", httpViews.AdminResetPassword)
						r.Post("/credits", httpViews.AdminGrantCredits)
						r.Post("/subscription", httpViews.AdminForceSubscription)
					})
				})

				r.Route("/apps", func(r chi.Router) {
					r.Get("/", httpViews.AdminListApps)
					r.Route("/{appId}", func(r chi.Router) {
						r.Get("/", httpViews.AdminGetApp)
						r.Put("/", httpViews.AdminUpdateApp)
						r.Delete("/", httpViews.AdminDeleteApp)
					})
				})

				r.Route("/projects", func(r chi.Router) {
					r.Get("/", httpViews.AdminListProjects)
					r.Route("/{projectId}", func(r chi.Router) {
						r.Get("/", httpViews.AdminGetProject)
						r.Delete("/", httpViews.AdminDeleteProject)
					})
				})

				r.Route("/subscriptions", func(r chi.Router) {
					r.Get("/", httpViews.AdminListSubscriptions)
					r.Put("/{subId}", httpViews.AdminUpdateSubscription)
				})

				r.Route("/credits", func(r chi.Router) {
					r.Get("/transactions", httpViews.AdminListCreditTransactions)
					r.Get("/balances", httpViews.AdminListCreditBalances)
					r.Get("/usage", httpViews.AdminGetTokenUsage)
				})

				r.Route("/organizations", func(r chi.Router) {
					r.Get("/", httpViews.AdminListOrganizations)
					r.Get("/{orgId}", httpViews.AdminGetOrganization)
				})

				r.Route("/tickets", func(r chi.Router) {
					r.Get("/", httpViews.AdminListTickets)
					r.Route("/{ticketId}", func(r chi.Router) {
						r.Get("/", httpViews.AdminGetTicket)
						r.Put("/", httpViews.AdminUpdateTicket)
					})
				})

				r.Route("/jobs", func(r chi.Router) {
					r.Get("/", httpViews.AdminListJobs)
					r.Get("/{jobId}", httpViews.AdminGetJob)
				})

				r.Get("/deployments", httpViews.AdminListDeployments)

				r.Get("/analytics/overview", httpViews.AdminGetAnalyticsOverview)
			})

			r.Post("/support/ticket", httpViews.CreateTicket)
			r.Post("/campaigns/kadin-girisimciler", httpViews.SubmitKadinGirisimciler)

			r.Route("/preview-codes", func(r chi.Router) {
				r.Post("/", httpViews.GeneratePreviewCode)
				r.Post("/{code}/redeem", httpViews.RedeemPreviewCode)
			})

			r.Route("/jobs", func(r chi.Router) {
				r.Get("/active", httpViews.GetActiveJobs)
				r.Get("/{jobId}", httpViews.GetJob)
				r.Delete("/{jobId}", httpViews.CancelJob)
			})

			// Web analytics collection (public, rate-limited per IP)
			r.Route("/analytics", func(r chi.Router) {
				r.Use(rateLimiter.RateLimit(240, time.Minute, appMiddleware.IPKey))
				r.Post("/collect", httpViews.CollectAnalytics)
				r.Post("/collect/batch", httpViews.CollectAnalyticsBatch)
			})

			r.Get("/integrations/providers", httpViews.GetV2IntegrationProviders)

			r.Route("/mobile/{projectId}", func(r chi.Router) {
				r.Use(rateLimiter.RateLimit(120, time.Minute, appMiddleware.IPKey))
				r.Get("/config", httpViews.GetPublicMobileConfig)
			})

			// Force-update gate: the mobile app cold-start hits this with the
			// project slug (typical case) or UUID (fallback). Un-authenticated
			// — the response only exposes operator-controlled values.
			r.Route("/mobile/{slugOrID}/min-version", func(r chi.Router) {
				r.Use(rateLimiter.RateLimit(120, time.Minute, appMiddleware.IPKey))
				r.Get("/", httpViews.GetMinVersion)
			})
		})
		// AI Gateway — separate from /v2 to avoid inheriting UserKey rate limiter
		r.Route("/v2/gateway", func(r chi.Router) {
			r.Use(appMiddleware.RequireGatewayKey(controller))
			r.Use(appMiddleware.GatewayRateLimit(rateLimiter, controller))
			r.Post("/chat/completions", httpViews.GatewayChat)
			r.Post("/images/generate", httpViews.GatewayImage)
			r.Get("/models", httpViews.GatewayModels)
		})

		// V2 App Builder routes
		r.Route("/v2", func(r chi.Router) {
			r.Use(rateLimiter.RateLimit(600, time.Minute, appMiddleware.UserKey))

			// Public asset endpoints (no auth required)
			r.Get("/apps/{appId}/logo", httpViews.GetAppLogo)
			r.Get("/apps/{appId}/assets/{assetId}/file", httpViews.GetAssetFile)
			// Generic app asset proxy — redirects to a fresh presigned S3
			// URL for any object under apps/{appId}/*. Used by seed images
			// and other auto-generated content referenced by the app.
			r.Get("/apps/{appId}/file/*", httpViews.GetAppAsset)

			// Public template marketplace (no auth)
			r.Route("/templates", func(r chi.Router) {
				r.Get("/", httpViews.ListPublishedTemplates)
				r.Get("/{slug}", httpViews.GetPublishedTemplate)
			})

			// Project MCP catalog and OAuth callback. The handlers perform
			// their own auth checks where needed; the callback is public
			// because the OAuth state/code are the authenticators.
			r.Get("/mcp/templates", httpViews.GetMCPTemplates)
			r.Get("/mcp/oauth/callback", httpViews.CompleteMCPOAuthCallback)
			r.Get("/orgs/{orgId}/agents/mcp-grants", httpViews.GetOrgAgentMCPGrants)

			// Template copy (requires auth + subscription)
			r.With(appMiddleware.RequireAuth(controller), appMiddleware.RequireSubscription(controller, "active", "trial", "free")).
				Post("/templates/{slug}/copy", httpViews.CopyTemplate)

			r.Route("/apps", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireSubscription(controller, "active", "trial", "free"))

				// Generation endpoints (AI rate limited)
				r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/generate/stream", httpViews.GenerateAppStream)

				r.Get("/jobs/{jobId}/stream", httpViews.StreamJob)
				r.Post("/jobs/{jobId}/approvals/{approvalId}", httpViews.SubmitMCPToolApproval)

				r.Get("/", httpViews.GetApps)
				r.Post("/", httpViews.CreateApp)

				r.Route("/{appId}", func(r chi.Router) {
					r.Get("/", httpViews.GetApp)
					r.Get("/init", httpViews.GetAppInit)
					r.Put("/", httpViews.UpdateApp)
					r.Delete("/", httpViews.DeleteApp)
					r.Post("/transfer", httpViews.TransferAppProject)

					// Files (code-first CRUD)
					r.Get("/files/{platform}", httpViews.GetAppFiles)
					r.Post("/files", httpViews.CreateAppFile)
					r.Put("/files", httpViews.UpdateAppFile)
					r.Put("/files/{fid}", httpViews.UpdateAppFile)
					r.Delete("/files/{fid}", httpViews.DeleteAppFile)

					// Chat
					r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/chat", httpViews.ChatApp)

					// Chat history (new AppChatMessage table)
					r.Get("/messages", httpViews.GetAppMessages)
					r.Delete("/messages", httpViews.ClearAppMessages)

					// Active job lookup — lets the chat UI reconnect to an
					// in-flight SSE stream after a page refresh without holding
					// the job id in localStorage.
					r.Get("/active-job", httpViews.GetAppActiveJob)

					// Integrations
					r.Route("/integrations", func(r chi.Router) {
						r.Get("/", httpViews.GetAppIntegrations)
						r.Post("/", httpViews.ConnectAppIntegration)
						r.Route("/{iid}", func(r chi.Router) {
							r.Put("/", httpViews.UpdateAppIntegration)
							r.Delete("/", httpViews.DisconnectAppIntegration)
						})
					})

					// Project-scoped MCP connections and per-agent grants
					r.Route("/mcp", func(r chi.Router) {
						r.Get("/connections", httpViews.GetAppMCPConnections)
						r.Post("/connections", httpViews.CreateAppMCPConnection)
						r.Post("/connections/oauth/start", httpViews.StartAppMCPOAuth)
						r.Route("/connections/{connectionId}", func(r chi.Router) {
							r.Post("/discover", httpViews.DiscoverAppMCPConnection)
							r.Delete("/", httpViews.DeleteAppMCPConnection)
						})
						r.Get("/grants", httpViews.GetAppMCPGrants)
						r.Put("/grants/{agentId}/{connectionId}", httpViews.PutAppMCPGrant)
					})

					// Custom environment variables
					r.Route("/env-vars", func(r chi.Router) {
						r.Get("/", httpViews.GetAppEnvVars)
						r.Post("/", httpViews.SetAppEnvVar)
						r.Get("/missing", httpViews.GetMissingEnvVars)
						r.Route("/{evid}", func(r chi.Router) {
							r.Put("/", httpViews.UpdateAppEnvVar)
							r.Delete("/", httpViews.DeleteAppEnvVar)
						})
					})

					// Preview bundle (WebContainer-based, client-side)
					r.Get("/preview-bundle", httpViews.GetPreviewBundle)

					// Sandbox lifecycle for live Daytona previews
					r.Get("/sandbox", httpViews.GetAppSandbox)
					r.Put("/sandbox", httpViews.SetAppSandbox)
					r.Post("/sandbox/start", httpViews.StartAppSandbox)
					r.Get("/sandbox/status", httpViews.GetAppSandboxStatus)
					r.Post("/sandbox/sync", httpViews.SyncSandbox)
					r.Post("/sandbox/open-route", httpViews.OpenSandboxRoute)

					// Deploy
					r.Route("/deploy", func(r chi.Router) {
						r.Post("/validate", httpViews.ValidateAppFiles)
						r.With(rateLimiter.RateLimit(3, time.Minute, appMiddleware.UserKey)).
							Post("/web", httpViews.DeployAppWeb)
						r.Delete("/web", httpViews.UndeployAppWeb)
						r.With(rateLimiter.RateLimit(3, time.Minute, appMiddleware.UserKey)).
							Post("/mobile", httpViews.DeployAppMobile)
						r.Delete("/mobile", httpViews.UndeployAppMobile)
						r.Get("/status", httpViews.GetDeployStatus)
						r.Get("/history", httpViews.GetDeployHistory)
					})

					// <P2#22> ota rollback + promote — project-owner-authenticated
					// recovery + staged-rollout controls. Rate limit is intentionally
					// tight (10/min) since these are operator actions, not anything
					// triggered by automation.
					r.With(rateLimiter.RateLimit(10, time.Minute, appMiddleware.UserKey)).
						Post("/ota/rollback", httpViews.RollbackOTA)
					r.With(rateLimiter.RateLimit(10, time.Minute, appMiddleware.UserKey)).
						Post("/ota/promote", httpViews.PromoteOTA)

					// GitHub export
					r.Route("/github", func(r chi.Router) {
						r.Get("/", httpViews.GetGitHubExport)
						r.Post("/export", httpViews.ExportToGitHub)
						r.Post("/push", httpViews.PushToGitHub)
						r.Post("/pull", httpViews.PullFromGitHub)
						r.Delete("/", httpViews.RemoveGitHubExport)
					})

					// Mobile builds (Expo / mobile_app_native only)
					r.Route("/build", func(r chi.Router) {
						// Expo (mobile_app_native) — request shape + permission
						// gate route through BuildExpoMobile.
						r.Post("/expo", httpViews.BuildAppExpo)
					})

					// Compose-push (P2.2) — dashboard-driven push notifications
					// for mobile_app_native projects. 5/min/user rate limit
					// stops a malicious admin from spamming OneSignal's per-app
					// rate limit (T8.3 audit fix).
					r.Route("/push", func(r chi.Router) {
						r.With(rateLimiter.RateLimit(5, time.Minute, appMiddleware.UserKey)).
							Post("/compose", httpViews.ComposePush)
						r.Get("/history", httpViews.GetComposePushHistory)
					})

					// <P2#25> Subscriptions panel — backend mirror of RC state.
					// Reads only; the RC v2 webhook does the writes.
					r.Route("/subscriptions", func(r chi.Router) {
						r.Get("/summary", httpViews.GetSubscriptionsSummary)
					})

					// Mobile submissions (P1.2 — Codemagic publishing blocks).
					// POST /v2/apps/{appId}/submit/{platform} — submits the latest
					// successful Expo build to TestFlight (iOS) or the configured
					// Play track (Android).
					r.Route("/submit", func(r chi.Router) {
						r.Post("/{platform}", httpViews.SubmitAppExpo)
					})

					// Releases (P1#16) — store-side lifecycle for each submission.
					// CreateReleaseFromSubmit writes the row from submit_expo_mobile;
					// these routes expose the resulting Release lifecycle to the dashboard.
					//   GET  /releases                     — list (?platform= filter)
					//   GET  /releases/current?platform=   — current live release
					//   POST /releases/{releaseID}/halt    — pause iOS phased rollout
					//   POST /releases/rollback            — rollback live → previous
					r.Route("/releases", func(r chi.Router) {
						r.Get("/", httpViews.ListReleases)
						r.Get("/current", httpViews.GetCurrentLiveRelease)
						r.Post("/rollback", httpViews.RollbackRelease)
						r.Post("/{releaseID}/halt", httpViews.HaltPhasedRollout)
					})

					// Persistent store listing — replaces the per-wizard form
					// state so icon/title/description/screenshots survive
					// refresh, re-submission, and future multi-locale work.
					r.Route("/store-listing", func(r chi.Router) {
						r.Get("/", httpViews.GetAppStoreListing)
						r.Put("/", httpViews.UpdateAppStoreListing)
					})

					// Force-update min_build_number (P3.2) — admin/owner-only;
					// bumping the floor locks out users on older builds, so
					// non-admins are rejected at the controller layer with 403.
					r.Patch("/min-version", httpViews.UpdateMinVersion)

					r.Post("/capture-screenshot", httpViews.CaptureAppScreenshot)

					// Snapshots
					r.Route("/snapshots", func(r chi.Router) {
						r.Get("/", httpViews.GetAppSnapshots)
						r.Post("/", httpViews.CreateAppSnapshot)
						r.Post("/{sid}/restore", httpViews.RestoreAppSnapshot)
						r.Get("/{sid}/diff", httpViews.GetAppSnapshotDiff)
					})

					// User-uploaded assets
					r.Route("/assets", func(r chi.Router) {
						r.Get("/", httpViews.GetAppAssets)
						r.Post("/upload-url", httpViews.RequestAssetUploadURL)
						r.Route("/{assetId}", func(r chi.Router) {
							r.Post("/confirm", httpViews.ConfirmAssetUpload)
							r.Put("/", httpViews.RenameAppAsset)
							r.Delete("/", httpViews.DeleteAppAsset)
						})
					})

					// Export
					r.Get("/export/{platform}", httpViews.ExportApp)

					// Domains
					r.Route("/domains", func(r chi.Router) {
						r.Get("/", httpViews.GetAppDomains)
						r.Post("/", httpViews.AddAppDomain)
						r.Post("/{did}/verify", httpViews.VerifyAppDomain)
						r.Delete("/{did}", httpViews.RemoveAppDomain)
					})

					// Dashboard analytics
					r.Route("/dashboard", func(r chi.Router) {
						r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/", httpViews.CreateDashboard)
						r.Get("/", httpViews.GetDashboard)
						r.Put("/", httpViews.UpdateDashboard)
						r.Delete("/", httpViews.DeleteDashboard)

						r.Get("/panels", httpViews.GetDashboardPanels)
						r.Route("/panels/{pid}", func(r chi.Router) {
							r.Put("/", httpViews.UpdateDashboardPanel)
							r.Delete("/", httpViews.DeleteDashboardPanel)
							r.Get("/data", httpViews.GetPanelData)
							r.Patch("/data", httpViews.UpdatePanelCell)
						})

						r.Get("/data", httpViews.GetDashboardData)

						r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/chat", httpViews.ChatDashboard)

						r.Get("/schema", httpViews.GetDashboardSchema)
						r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/schema/refresh", httpViews.RefreshDashboardSchema)
					})

					// Database Manager
					r.Route("/database", func(r chi.Router) {
						r.Get("/schema", httpViews.GetDatabaseSchema)
						r.Post("/upload-url", httpViews.GetDatabaseUploadURL)
						r.Route("/tables/{table}", func(r chi.Router) {
							r.Get("/rows", httpViews.GetDatabaseTableRows)
							r.Post("/rows", httpViews.InsertDatabaseRow)
							r.Patch("/rows", httpViews.UpdateDatabaseRow)
							r.Put("/rows", httpViews.BulkUpdateDatabaseRow)
							r.Delete("/rows", httpViews.DeleteDatabaseRow)
						})
					})

					// Web analytics for deployed sites
					r.Route("/analytics", func(r chi.Router) {
						r.Get("/overview", httpViews.GetAppAnalyticsOverview)
						r.Get("/pageviews", httpViews.GetAppAnalyticsPageviews)
						r.Get("/pages", httpViews.GetAppAnalyticsPages)
						r.Get("/referrers", httpViews.GetAppAnalyticsReferrers)
						r.Get("/devices", httpViews.GetAppAnalyticsDevices)
						r.Get("/locations", httpViews.GetAppAnalyticsLocations)
						r.Get("/events", httpViews.GetAppAnalyticsEvents)
						r.Get("/realtime", httpViews.GetAppAnalyticsRealtime)
					})

					// Gateway key management
					r.Get("/gateway-key", httpViews.GetGatewayKeyInfo)
					r.Post("/gateway-key/reveal", httpViews.RevealGatewayKey)
					r.Post("/gateway-key/rotate", httpViews.RotateGatewayKey)

					// AI usage for this generated app — tokens, call count,
					// per-model breakdown for everything that flowed through
					// the Wemob AI Gateway on behalf of the project.
					r.Get("/ai-usage", httpViews.GetProjectAIUsage)

					// Ship tab — Launchpad aggregate
					r.Get("/launchpad", httpViews.GetLaunchpad)
					r.Post("/listing/draft", httpViews.DraftListing)

					// Ecommerce admin — proxies to the per-tenant Neon DB.
					// Apps without a Neon integration get 404 from each handler.
					r.Route("/store", func(r chi.Router) {
						r.Get("/", httpViews.GetAppStore)
						r.Put("/settings", httpViews.UpdateAppStoreSettings)
						r.Get("/analytics", httpViews.GetStoreAnalytics)

						r.Route("/products", func(r chi.Router) {
							r.Get("/", httpViews.ListStoreProducts)
							r.Post("/", httpViews.CreateStoreProduct)
							r.Get("/{productId}", httpViews.GetStoreProduct)
							r.Put("/{productId}", httpViews.UpdateStoreProduct)
							r.Delete("/{productId}", httpViews.DeleteStoreProduct)
						})

						r.Route("/categories", func(r chi.Router) {
							r.Get("/", httpViews.ListStoreCategories)
							r.Post("/", httpViews.CreateStoreCategory)
							r.Put("/{categoryId}", httpViews.UpdateStoreCategory)
							r.Delete("/{categoryId}", httpViews.DeleteStoreCategory)
						})

						r.Route("/discounts", func(r chi.Router) {
							r.Get("/", httpViews.ListStoreDiscounts)
							r.Post("/", httpViews.CreateStoreDiscount)
							r.Put("/{discountId}", httpViews.UpdateStoreDiscount)
							r.Delete("/{discountId}", httpViews.DeleteStoreDiscount)
						})

						r.Route("/orders", func(r chi.Router) {
							r.Get("/", httpViews.ListStoreOrders)
							r.Get("/{orderId}", httpViews.GetStoreOrder)
							r.Put("/{orderId}", httpViews.UpdateStoreOrderStatus)
						})
					})

					// Backend introspection (studio mode split)
					r.Route("/backend", func(r chi.Router) {
						r.Get("/health", httpViews.GetBackendHealth)
						r.Get("/routes", httpViews.GetBackendRoutes)
						r.Get("/logs", httpViews.GetBackendLogs)
					})
				})
			})

			r.Route("/mobile/{projectId}", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireSubscription(controller, "active", "trial", "free"))

				r.Get("/config", httpViews.GetMobileAppConfig)
				r.Put("/config", httpViews.UpdateMobileAppConfig)
			})

			// Connection validation status — requires auth, no subscription gate.
			r.Route("/connections/{kind}/{connectionId}", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Get("/latest-validation", httpViews.GetLatestConnectionValidation)
			})

			// <P1#13> build events list + SSE — exposes the granular Codemagic
			// event stream captured by /v2/reports/expo-build to the frontend so
			// publish surfaces drive their build-stage ladder off the actual
			// stream instead of string-matching on a coarse status field.
			r.Route("/tasks/{taskId}/events", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Get("/", httpViews.GetBuildEvents)
				r.Get("/stream", httpViews.StreamBuildEvents)
			})

			// Expo build status webhook — no user auth, HMAC-signed by Codemagic worker.
			// Per-build secret derived as HMAC(master, task_id); the keystore endpoints
			// at /v2/codemagic/keystore[/fetch] use the same master with HMAC(master, project_id).
			workerHMACKey := controller.SecretManager.GetSecret("WEMOB_WORKER_HMAC_KEY")
			if workerHMACKey == "" {
				slog.Error("WEMOB_WORKER_HMAC_KEY secret is required (generate with: openssl rand -hex 32, then set in Infisical)")
				os.Exit(1)
			}
			expoReportsHandler := views.NewExpoReportsHandler(controller.DB, workerHMACKey).
				WithReleaseStore(controller)
			r.Post("/reports/expo-build", expoReportsHandler.UpdateExpoBuildStatus)

			keystoreHandler := views.NewCodemagicKeystoreHandler(controller, workerHMACKey)
			r.Post("/codemagic/keystore", keystoreHandler.RegisterKeystore)
			r.Post("/codemagic/keystore/fetch", keystoreHandler.FetchKeystore)

			// P0 #8a — OTA manifest signing: codemagic fetches the public cert
			// to bake into the binary at build time. Cert is non-secret; the
			// HMAC scheme just confirms the worker's identity (rate-limit /
			// audit purposes; no secrets cross this wire).
			otaCertHandler := views.NewCodemagicOTACertHandler(controller, workerHMACKey)
			r.Post("/codemagic/ota-cert", otaCertHandler.FetchOTACert)

			// <P1#14> apple-signing route — Codemagic worker fetches the
			// auto-generated iOS distribution cert + profile + private key
			// at build time. HMAC-protected; per-project AppleSigningAssets
			// row is generated lazily on first call (or renewed if expiring).
			appleSigningHandler := views.NewCodemagicAppleSigningHandler(controller, workerHMACKey)
			r.Post("/codemagic/apple-signing", appleSigningHandler.FetchAppleSigning)

			// P2.1: self-hosted expo-updates OTA.
			//   POST /v2/apps/{appId}/ota/publish     — HMAC, worker-only
			//   GET  /v2/apps/{appId}/ota/manifest    — un-authenticated, mobile client
			//   GET  /v2/apps/{appId}/ota/assets      — un-authenticated, mobile client
			otaPublishHandler := views.NewOTAPublishHandler(controller, workerHMACKey)
			otaManifestHandler := views.NewOTAManifestHandler(controller)
			r.Post("/apps/{appId}/ota/publish", otaPublishHandler.PublishOTA)
			r.Get("/apps/{appId}/ota/manifest", otaManifestHandler.GetManifest)
			r.Get("/apps/{appId}/ota/assets", otaManifestHandler.GetAsset)

			// Admin template management
			r.Route("/admin/templates", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireAdmin())
				r.Post("/", httpViews.CreateAdminTemplate)
				r.Post("/from-app/{appId}", httpViews.CreateTemplateFromApp)
				r.Get("/", httpViews.ListAdminTemplates)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", httpViews.GetAdminTemplate)
					r.Put("/", httpViews.UpdateAdminTemplate)
					r.Delete("/", httpViews.DeleteAdminTemplate)
					r.Put("/publish", httpViews.PublishAdminTemplate)
					r.Put("/sort", httpViews.SortAdminTemplate)
				})
			})

			// Admin AI agent system management
			r.Route("/admin/agents", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireAdmin())
				r.Get("/", httpViews.GetAdminAgentSystem)
				r.Post("/skills/lint", httpViews.LintAdminAgentSkill)
			})

			// Public integration providers
			r.Get("/integrations/providers", httpViews.GetV2IntegrationProviders)

			// GitHub OAuth
			r.Post("/auth/github/start", httpViews.StartGitHubAuth)
			r.Get("/auth/github", httpViews.GitHubAuthRedirect)
			r.Get("/auth/github/callback", httpViews.GitHubAuthCallback)
			r.Delete("/auth/github", httpViews.DisconnectGitHub)
			r.Get("/auth/github/status", httpViews.GetGitHubConnectionStatus)
		})

		edgeToken := sm.GetSecret("EDGE_INTERNAL_TOKEN")
		// cert-allowed is called by Caddy's on_demand_tls.ask, which cannot
		// send custom headers — InternalOnly would always 401 it. The endpoint
		// is read-only and only reveals "is this domain registered+verified?",
		// which is also observable by trying to fetch the site itself, so
		// public access is acceptable here.
		r.Get("/internal/edge/cert-allowed", httpViews.HandleEdgeCertAllowed)
		r.Route("/internal/edge", func(r chi.Router) {
			r.HandleFunc("/proxy", httpViews.HandleEdgeProxy)
			r.Group(func(r chi.Router) {
				r.Use(appMiddleware.InternalOnly(edgeToken))
				r.Get("/upstream", httpViews.HandleEdgeUpstream)
			})
		})
		r.Route("/internal/preview", func(r chi.Router) {
			r.Use(appMiddleware.InternalOnly(edgeToken))
			r.Get("/resolve", httpViews.ResolvePreviewHost)
			r.HandleFunc("/proxy", httpViews.HandlePreviewProxy)
		})

		r.Route("/internal/health", func(r chi.Router) {
			r.Use(appMiddleware.InternalOnly(edgeToken))
			r.Get("/deploy-pipeline", httpViews.HandleDeployPipelineHealth)
		})

		r.Get("/health", httpViews.Health)
		r.Get("/sdk/wemob-bridge.js", httpViews.ServeBridgeSDK)
	})

	// Create explicit http.Server instances for graceful shutdown
	mainServer := &http.Server{
		Addr:              ":8001",
		Handler:           r,
		MaxHeaderBytes:    1 << 20, // 1 MB
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      30 * time.Minute, // long for SSE streaming
		IdleTimeout:       120 * time.Second,
	}
	// Periodically clean up expired preview codes
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := controller.DB.CleanupExpiredPreviewCodes(); err != nil {
				slog.Error("preview code cleanup failed", "error", err)
			} else if n > 0 {
				slog.Info("cleaned up expired preview codes", "count", n)
			}
		}
	}()

	// <P1#16> release status poll — once a minute, scan all releases in
	// non-terminal status and ask App Store Connect / Google Play for the
	// latest state. Replaces "Codemagic finished uploading" as the source of
	// truth for "live on store".
	releasePollCtx, cancelReleasePoll := context.WithCancel(context.Background())
	releasePoller := &controllers.ReleaseStatusPoller{
		Controller: controller,
		PollEvery:  60 * time.Second,
	}
	go releasePoller.Run(releasePollCtx)

	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		sig := <-quit
		slog.Info("Received shutdown signal", "signal", sig)

		// Cancel all running background jobs
		controller.RunningJobs.Range(func(key, value any) bool {
			if cancel, ok := value.(context.CancelFunc); ok {
				cancel()
			}
			return true
		})

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Stop the release-status poller.
		cancelReleasePoll()

		// Shutdown main server first — in-flight requests need to complete
		// (and the events they enqueue need to land in the PostHog buffer)
		// before we flush. Closing PostHog before Shutdown drops every event
		// fired during the drain window, including the final job_failed /
		// job_succeeded emits from the deferred Asynq middleware.
		if err := mainServer.Shutdown(ctx); err != nil {
			slog.Error("Main server shutdown error", "error", err)
		}

		// Flush PostHog events AFTER the server stops accepting requests so
		// the queue contains every event from the drain window.
		if controller.PostHog != nil {
			controller.PostHog.Close()
		}

		if err := langfuseHandle.Shutdown(ctx); err != nil {
			slog.Error("Langfuse OpenTelemetry shutdown error", "error", err)
		}

		// Close database connection pool
		slog.Info("closing database connection pool")
		if err := bundb.Close(); err != nil {
			slog.Error("error closing database", "error", err)
		}

		// Close Redis connection
		if redisClient != nil {
			slog.Info("closing redis connection")
			if err := redisClient.Close(); err != nil {
				slog.Error("error closing redis", "error", err)
			}
		}

		slog.Info("Servers shut down gracefully")
	}()

	// Cleanup expired tokens on startup
	if n, err := db.DeleteExpiredTokens(); err != nil {
		slog.Warn("failed to clean expired tokens", "error", err)
	} else if n > 0 {
		slog.Info("Cleaned expired tokens", "count", n)
	}

	// Main server (blocks until Shutdown is called)
	slog.Info("Main server listening on :8001")
	if err := mainServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Main server error", "error", err)
	}
}
