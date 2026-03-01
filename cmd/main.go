package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/go-chi/httplog/v3"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/database"
	appMiddleware "github.com/knid/wemob/internal/middleware"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services"
	crmServices "github.com/knid/wemob/internal/services/crm"
	emailServices "github.com/knid/wemob/internal/services/email"
	"github.com/knid/wemob/internal/services/mcpclient"
	flyService "github.com/knid/wemob/internal/services/fly"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/services/preview"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/services/vercel"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/views"
	"github.com/markbates/goth"
	"github.com/markbates/goth/providers/google"
	"github.com/redis/go-redis/v9"

	_ "github.com/lib/pq"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/uptrace/bun/extra/bundebug"

	"github.com/stripe/stripe-go/v82"

	"log/slog"

	infisical "github.com/infisical/go-sdk"
	"github.com/posthog/posthog-go"
	slogbetterstack "github.com/samber/slog-betterstack"
)

func createSchema(db *bun.DB) error {
	models := []interface{}{
		(*models.User)(nil),
		(*models.UserNotifications)(nil),
		(*models.Token)(nil),
		(*models.StandartUserLimits)(nil),
		(*models.StandartUserSubscription)(nil),
		(*models.Invoice)(nil),
		(*models.Project)(nil),
		(*models.Build)(nil),
		(*models.Release)(nil),
		(*models.Device)(nil),
		(*models.Notification)(nil),
		(*models.Session)(nil),
		(*models.AppUsageEvent)(nil),
		(*models.AppBuildTask)(nil),
		(*models.AppReleaseTask)(nil),
		(*models.Ticket)(nil),
		(*models.NavBarItems)(nil),
		(*models.JSInjectData)(nil),
		(*models.GooglePlayCredentials)(nil),
		(*models.AppStoreCredentials)(nil),
		(*models.Deployment)(nil),
		(*models.SignIPATask)(nil),
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
		(*models.AppConversation)(nil),
		(*models.AppGeneratedFile)(nil),
		(*models.AppSnapshot)(nil),
		(*models.VercelProject)(nil),
		(*models.FlutterBuild)(nil),
		(*models.AppDomain)(nil),
		// Custom environment variables
		(*models.AppEnvVar)(nil),
		// E-commerce models
		(*models.Store)(nil),
		(*models.Product)(nil),
		(*models.ProductCategory)(nil),
		(*models.Discount)(nil),
		(*models.Cart)(nil),
		(*models.CartItem)(nil),
		(*models.Order)(nil),
		(*models.OrderItem)(nil),
		(*models.ProductReview)(nil),
		(*models.Wishlist)(nil),
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
		// Preview machines (Fly Machines)
		(*models.AppPreviewMachine)(nil),
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

	// Add columns that were introduced after initial table creation.
	// Each statement uses IF NOT EXISTS so it is safe to run repeatedly.
	alterStatements := []string{
		// AI credits fields on limits
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS monthly_ai_credits int NOT NULL DEFAULT 0`,
		// Annual billing fields on subscriptions
		`ALTER TABLE standart_user_subscriptions ADD COLUMN IF NOT EXISTS annual_price int DEFAULT 0`,
		`ALTER TABLE standart_user_subscriptions ADD COLUMN IF NOT EXISTS annual_price_id text DEFAULT ''`,
		// Snapshot file capture
		`ALTER TABLE app_snapshots ADD COLUMN IF NOT EXISTS files jsonb DEFAULT '[]'`,
		`ALTER TABLE app_snapshots ADD COLUMN IF NOT EXISTS message_id text DEFAULT ''`,
		// V2 app builder fields on limits
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS monthly_app_count int NOT NULL DEFAULT 0`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS monthly_deploy_count int NOT NULL DEFAULT 0`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_use_ecommerce boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_deploy_to_vercel boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_build_flutter boolean NOT NULL DEFAULT false`,
		// Conversation summary for older message summarization
		`ALTER TABLE app_conversations ADD COLUMN IF NOT EXISTS summary text DEFAULT ''`,
		// Data models for per-app Neon database provisioning
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS data_models jsonb DEFAULT '[]'`,
		// soft_delete column for all tables that use bun:",soft_delete,nullzero"
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE user_notifications ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE tokens ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE standart_user_subscriptions ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE builds ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE notifications ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE store_page_images ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_build_tasks ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_release_tasks ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE tickets ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE google_play_credentials ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_store_credentials ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE deployments ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE sign_ipa_tasks ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE organization_members ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE webhook_endpoints ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_integrations ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_domains ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_env_vars ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE stores ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE products ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE product_categories ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE discounts ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE carts ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_dashboards ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_dashboard_panels ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_notification_tasks ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		// Preview machine env hash for skip-restart optimization
		`ALTER TABLE app_preview_machines ADD COLUMN IF NOT EXISTS last_env_hash text DEFAULT ''`,
		// Flatten price onto products (remove variant dependency)
		`ALTER TABLE products ADD COLUMN IF NOT EXISTS price_in_cents integer NOT NULL DEFAULT 0`,
		`ALTER TABLE products ADD COLUMN IF NOT EXISTS compare_at_price integer DEFAULT 0`,
		`ALTER TABLE products ADD COLUMN IF NOT EXISTS sku text DEFAULT ''`,
		`ALTER TABLE products ADD COLUMN IF NOT EXISTS inventory integer NOT NULL DEFAULT 0`,
		`ALTER TABLE products ADD COLUMN IF NOT EXISTS track_inventory boolean NOT NULL DEFAULT false`,
		// Migrate default variant prices into products
		`UPDATE products SET
			price_in_cents = COALESCE(pv.price_in_cents, 0),
			compare_at_price = COALESCE(pv.compare_at_price, 0),
			sku = COALESCE(pv.sku, ''),
			inventory = COALESCE(pv.inventory, 0),
			track_inventory = COALESCE(pv.track_inventory, false)
		FROM (
			SELECT DISTINCT ON (product_id) product_id, price_in_cents, compare_at_price, sku, inventory, track_inventory
			FROM product_variants
			WHERE deleted_at IS NULL
			ORDER BY product_id, is_default DESC, created_at ASC
		) pv
		WHERE products.id = pv.product_id AND products.price_in_cents = 0`,
	}

	// Deduplicate before creating unique indexes (keep the latest row per group)
	dedupeStatements := []string{
		`DELETE FROM app_generated_files a USING app_generated_files b
		 WHERE a.id < b.id AND a.app_project_id = b.app_project_id AND a.platform = b.platform AND a.file_path = b.file_path`,
		`DELETE FROM app_conversations a USING app_conversations b
		 WHERE a.id < b.id AND a.app_project_id = b.app_project_id`,
	}
	for _, stmt := range dedupeStatements {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			slog.Warn("dedup statement skipped", "error", err)
		}
	}

	// Create unique indexes needed for upsert operations
	indexStatements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_app_generated_files_project_platform_path ON app_generated_files (app_project_id, platform, file_path)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_app_conversations_project ON app_conversations (app_project_id)`,
		// Web analytics indexes
		`CREATE INDEX IF NOT EXISTS idx_web_analytics_project_created ON web_analytics_events (project_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_web_analytics_project_type_created ON web_analytics_events (project_id, event_type, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_web_analytics_project_session ON web_analytics_events (project_id, session_id)`,
		// Token lookup index — covers the soft-delete filter used by GetTokenByToken
		`CREATE INDEX IF NOT EXISTS idx_tokens_string_active ON tokens (string) WHERE deleted_at IS NULL`,
	}
	alterStatements = append(alterStatements, indexStatements...)
	for _, stmt := range alterStatements {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			slog.Error("alter table failed", "statement", stmt, "error", err)
		}
	}

	// Drop legacy website builder tables (replaced by app_* tables in V2)
	dropStatements := []string{
		`DROP TABLE IF EXISTS website_form_submissions`,
		`DROP TABLE IF EXISTS website_forms`,
		`DROP TABLE IF EXISTS website_assets`,
		`DROP TABLE IF EXISTS website_components`,
		`DROP TABLE IF EXISTS website_conversations`,
		`DROP TABLE IF EXISTS website_domains`,
		`DROP TABLE IF EXISTS website_files`,
		`DROP TABLE IF EXISTS website_integrations`,
		`DROP TABLE IF EXISTS website_pages`,
		`DROP TABLE IF EXISTS website_seos`,
		`DROP TABLE IF EXISTS website_snapshots`,
		`DROP TABLE IF EXISTS website_templates`,
		`DROP TABLE IF EXISTS websites`,
		`DROP TABLE IF EXISTS ai_generation_logs`,
	}
	for _, stmt := range dropStatements {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			slog.Warn("drop legacy table failed", "statement", stmt, "error", err)
		}
	}

	return nil
}

func main() {
	infisicalClient := infisical.NewInfisicalClient(context.Background(), infisical.Config{
		SiteUrl: "https://eu.infisical.com",
	})

	infisicalClientID := os.Getenv("INFISICAL_CLIENT_ID")
	infisicalClientSecret := os.Getenv("INFISICAL_CLIENT_SECRET")

	if infisicalClientID == "" || infisicalClientSecret == "" {
		slog.Error("Infisical client ID or secret is not set in environment variables")
		return
	}

	_, err := infisicalClient.Auth().UniversalAuthLogin(infisicalClientID, infisicalClientSecret)
	if err != nil {
		slog.Error("Failed to authenticate with Infisical:", slog.String("error", err.Error()))
		return
	}

	var sm secret.SecretManager
	sm = secret.NewInfisicalSecretManager(
		infisicalClient.(*infisical.InfisicalClient),
		os.Getenv("INFISICAL_PROJECT_ID"),
		os.Getenv("ENVIRONMENT"),
	)

	betterstackHandler := slogbetterstack.Option{
		Token:    sm.GetSecret("BETTERSTACK_TOKEN"),
		Endpoint: sm.GetSecret("BETTERSTACK_ENDPOINT"),
	}.NewBetterstackHandler()

	var logger *slog.Logger
	if sm.GetEnv() == "dev" {
		// In dev, log to both stdout and BetterStack
		stdoutHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
		logger = slog.New(utils.NewFanoutHandler(stdoutHandler, betterstackHandler))
	} else {
		logger = slog.New(betterstackHandler)
	}

	slog.SetDefault(logger)

	dbHost := sm.GetSecret("DB_HOST")
	dbPort := sm.GetSecret("DB_PORT")
	dbName := sm.GetSecret("DB_NAME")
	dbUser := sm.GetSecret("DB_USER")
	dbPass := sm.GetSecret("DB_PASSWORD")
	dbSSL := sm.GetSecret("DB_SSLMODE")

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		dbUser, dbPass, dbHost, dbPort, dbName, dbSSL,
	)

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	sqldb.SetMaxOpenConns(50)
	sqldb.SetMaxIdleConns(25)
	sqldb.SetConnMaxLifetime(30 * time.Minute)
	sqldb.SetConnMaxIdleTime(5 * time.Minute)
	bundb := bun.NewDB(sqldb, pgdialect.New())

	bundb.Exec("CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";")

	bundb.AddQueryHook(bundebug.NewQueryHook(
		bundebug.WithVerbose(os.Getenv("BUNDEBUG") == "2"),
		bundebug.FromEnv("BUNDEBUG"),
	))

	bundb.AddQueryHook(utils.NewQueryHook(utils.QueryHookOptions{
		Logger:       logger,
		SlowDuration: 200 * time.Millisecond, // Omit to log all operations as debug
	}))

	createSchema(bundb)

	db := database.NewDatabase(bundb)
	go utils.CheckSessionStatusJob(db)
	go utils.CheckUsageAlertsJob(db, crmServices.NewBrevoCRMService(sm.GetSecret("BREVO_API_KEY")))
	vercelService := vercel.NewService(sm.GetSecret("VERCEL_API_TOKEN"), sm.GetSecret("VERCEL_TEAM_ID"))

	// Preview service (optional — only enabled when Fly secrets are set)
	var previewService *preview.Service
	if flyToken := sm.GetSecret("FLY_API_TOKEN"); flyToken != "" {
		flySvc := flyService.NewService(
			flyToken,
			sm.GetSecret("FLY_PREVIEW_APP_NAME"),
			sm.GetSecret("FLY_PREVIEW_REGION"),
			sm.GetSecret("FLY_PREVIEW_IMAGE"),
		)
		previewDomain := sm.GetSecret("FLY_PREVIEW_DOMAIN")
		if previewDomain == "" {
			previewDomain = "preview.wemob.app"
		}
		previewService = preview.NewService(flySvc, db, previewDomain)
		slog.Info("Preview service enabled (Fly Machines)", "app", sm.GetSecret("FLY_PREVIEW_APP_NAME"), "domain", previewDomain)

		// Background idle cleanup: stop machines idle >1h, destroy stopped >6h
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				previewService.StopIdleMachines(1 * time.Hour)
				previewService.DestroyStoppedMachines(6 * time.Hour)
			}
		}()
	}

	// Neon service (optional — only enabled when API key is set)
	var neonService *neon.Service
	if neonKey := sm.GetSecret("NEON_API_KEY"); neonKey != "" {
		neonService = neon.NewService(neonKey)
		slog.Info("Neon database service enabled")
	}

	// PostHog client (optional — only enabled when API key is set)
	var posthogClient posthog.Client
	if phKey := sm.GetSecret("POSTHOG_API_KEY"); phKey != "" {
		phClient, phErr := posthog.NewWithConfig(phKey, posthog.Config{
			Endpoint: "https://eu.i.posthog.com",
		})
		if phErr != nil {
			slog.Warn("PostHog initialization failed", "error", phErr)
		} else {
			posthogClient = phClient
			slog.Info("PostHog analytics enabled")
		}
	}

	controller := controllers.NewController(
		db,
		make(chan struct{}, constants.MAX_CONCURRENT_BUILDS),
		services.NewSlackService(sm.GetSecret("SLACK_API_KEY")),
		emailServices.NewBrevoEmailService(sm.GetSecret("BREVO_API_KEY")),
		crmServices.NewBrevoCRMService(sm.GetSecret("BREVO_API_KEY")),
		asynq.NewClient(asynq.RedisClientOpt{
			Addr:      sm.GetSecret("REDIS_ADDR"),
			Username:  sm.GetSecret("REDIS_USERNAME"),
			Password:  sm.GetSecret("REDIS_PASSWORD"),
			TLSConfig: &tls.Config{},
		}),
		storage.NewS3Storage(sm.GetSecret("AWS_REGION"), sm.GetSecret("AWS_S3_ASSETS_BUCKET_NAME"), sm.GetSecret("AWS_ACCESS_KEY_ID"), sm.GetSecret("AWS_SECRET_ACCESS_KEY")),
		storage.NewS3Storage(sm.GetSecret("AWS_REGION"), sm.GetSecret("AWS_S3_USER_API_KEYS_BUCKET_NAME"), sm.GetSecret("AWS_ACCESS_KEY_ID"), sm.GetSecret("AWS_SECRET_ACCESS_KEY")),
		sm,
		vercelService,
		previewService,
		neonService,
		nil, // MCP service initialized async below
		posthogClient,
	)

	// MCP service (optional — connect in background so it doesn't block server startup)
	if mcpConfig := sm.GetSecret("MCP_SERVERS_CONFIG"); mcpConfig != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			svc, err := mcpclient.NewService(ctx, mcpConfig)
			if err != nil {
				slog.Warn("MCP service initialization had errors", "error", err)
			}
			if svc != nil && svc.HasTools() {
				controller.MCPService = svc
				slog.Info("MCP service enabled", "tool_count", len(svc.Tools()))
			}
		}()
	}

	goth.UseProviders(
		google.New(sm.GetSecret("GOOGLE_CLIENT_ID"), sm.GetSecret("GOOGLE_SECRET_KEY"), "https://api.wemob.io/v1/users/auth/google/callback", "email", "profile"),
	)

	stripe.Key = sm.GetSecret("STRIPE_API_KEY")

	// Configure upstream proxy transport once at startup (prevents per-request data race)
	views.InitProxyTransport(
		sm.GetSecret("PREVIEW_PROXY_HOST"),
		sm.GetSecret("PREVIEW_PROXY_AUTH"),
	)

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
		AllowedOrigins:   []string{"https://*", "http://*", "http://127.0.0.1:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "Location"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Use(rateLimiter.RateLimit(1200, time.Minute, appMiddleware.IPKey))

	r.Use(httplog.RequestLogger(logger, &httplog.Options{
		// Level defines the verbosity of the request logs:
		// slog.LevelDebug - log all responses (incl. OPTIONS)
		// slog.LevelInfo  - log responses (excl. OPTIONS)
		// slog.LevelWarn  - log 4xx and 5xx responses only (except for 429)
		// slog.LevelError - log 5xx responses only
		Level: slog.LevelWarn,

		Schema:        httplog.SchemaOTEL,
		RecoverPanics: true,
		LogRequestBody:  func(req *http.Request) bool { return false },
		LogResponseBody: func(req *http.Request) bool { return false },
	}))

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			httplog.SetAttrs(ctx, slog.String("from", "HTTPLOG"))

			httplog.SetAttrs(ctx,
				slog.String("client.ip", r.RemoteAddr),
				slog.String("client.xff", r.Header.Get("X-Forwarded-For")),
				slog.String("client.xri", r.Header.Get("X-Real-IP")),
			)

			tokenString, err := utils.ExtractTokenFromHeader(r)
			if err == nil {
				httplog.SetAttrs(ctx, slog.String("user.token", tokenString[0:12]+"..."))
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

	r.Route("/", func(r chi.Router) {
		r.Route("/webhooks", func(r chi.Router) {
			r.Post("/stripe", httpViews.Controller.HandleWebhook)
			// Store payment webhooks
			r.Post("/store/stripe/{storeId}", httpViews.HandleStoreStripeWebhook)
			r.Post("/store/iyzico/{storeId}", httpViews.HandleStoreIyzicoWebhook)
		})
		r.Route("/v1", func(r chi.Router) {
			r.Use(rateLimiter.RateLimit(600, time.Minute, appMiddleware.UserKey))
			r.Handle("/static/*", http.StripPrefix("/v1/static/", http.FileServer(utils.NoDirFileSystem{FS: staticDir})))
			r.Route("/storage", func(r chi.Router) {
				r.Route("/credentials", func(r chi.Router) {
					r.Route("/apple", func(r chi.Router) {
						r.Post("/", httpViews.PutP8KeyToS3Presign)
						r.Post("/apns", httpViews.PutAPNsKeyToS3Presign)
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
			r.HandleFunc("/proxy", httpViews.ProxyHandler)
			r.Get("/check/{projectId}", httpViews.CheckStatus)
			r.Route("/users", func(r chi.Router) {
				r.Route("/me", func(r chi.Router) {
					r.Get("/", httpViews.GetUser)
					r.Put("/", httpViews.UpdateUser)
					r.Delete("/", httpViews.DeleteUser)

					r.Get("/usage", httpViews.GetMontlyUsage)
				})

				r.Get("/notifications", httpViews.GetUserNotifications)
				r.Get("/notifications/{userNotificationId}", httpViews.GetUserNotification)

				r.Post("/login", httpViews.UserLogin)
				r.Post("/register", httpViews.UserRegister)

				r.Get("/auth/{provider}/callback", httpViews.OAuthCallback)
				r.Get("/auth/{provider}", httpViews.OAuthBegin)
			})
			r.Get("/tasks", httpViews.GetAllTasks)
			r.Route("/projects", func(r chi.Router) {
				r.Get("/", httpViews.GetProjects)
				r.Post("/", httpViews.CreateProject)
				r.Route("/{projectId}", func(r chi.Router) {
					r.Get("/", httpViews.GetProject)
					r.Put("/", httpViews.UpdateProject)
					r.Delete("/", httpViews.DeleteProject)
					r.Route("/analytics", func(r chi.Router) {
						r.Route("/events", func(r chi.Router) {
							r.Get("/", httpViews.GetProjectEvents)
							r.Post("/", httpViews.LogAppEvent)
						})
						r.Route("/session", func(r chi.Router) {
							r.Post("/", httpViews.CreateSession)
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
					r.Route("/builds", func(r chi.Router) {
						r.Get("/", httpViews.GetProjectBuilds)
						r.Post("/", httpViews.CreateProjectBuild)
						r.Route("/{buildId}", func(r chi.Router) {
							r.Get("/", httpViews.GetProjectBuild)
						})
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
				})
				r.Route("/autofill", func(r chi.Router) {
					r.Post("/navbar", httpViews.NavBarAutoFillWithAI)
				})
			})
			r.Route("/credentials", func(r chi.Router) {
				r.Route("/google", func(r chi.Router) {
					r.Get("/", httpViews.ListGooglePlayCredentials)
					r.Post("/", httpViews.CreateGooglePlayCredential)
					r.Route("/{credentialId}", func(r chi.Router) {
						r.Get("/", httpViews.GetGooglePlayCredential)
						r.Put("/", httpViews.UpdateGooglePlayCredential)
						r.Delete("/", httpViews.DeleteGooglePlayCredential)
					})
				})
				r.Route("/appstore", func(r chi.Router) {
					r.Get("/", httpViews.ListAppStoreCredentials)
					r.Post("/", httpViews.CreateAppStoreCredential)
					r.Route("/{credentialId}", func(r chi.Router) {
						r.Get("/", httpViews.GetAppStoreCredential)
						r.Put("/", httpViews.UpdateAppStoreCredential)
						r.Delete("/", httpViews.DeleteAppStoreCredential)
					})
				})
			})
			r.Route("/deployments", func(r chi.Router) {
				r.Get("/", httpViews.GetDeployments)
				r.Get("/{deploymentId}", httpViews.GetDeployment)
				r.Put("/{deploymentId}", httpViews.UpdateDeployment)
				r.Post("/", httpViews.CreateDeployment)
				r.Route("/tools", func(r chi.Router) {
					r.Post("/generate_screenshots", httpViews.AutoGenerateScreenshots)
					r.Post("/sign_ipa", httpViews.SignUnsignedIPA)
					r.Get("/sign_ipa/{signTaskId}", httpViews.CheckSignIPATaskStatus)
				})
			})
			r.Route("/reports", func(r chi.Router) {
				r.Post("/build", httpViews.UpdateBuildTaskStatus)
				r.Post("/sign", httpViews.UpdateSignIPATaskStatus)
				r.Post("/deploy", httpViews.UpdateDeploymentStatus)
			})
			r.Route("/billing", func(r chi.Router) {
				r.Get("/plans", httpViews.GetBillingPlans)
				r.Post("/checkout", httpViews.StripeCheckout)
				r.Route("/subscription", func(r chi.Router) {
					r.Post("/portal", httpViews.StripeCustomerPortal)
					r.Get("/", httpViews.GetSubscription)
					r.Put("/", httpViews.UpdateSubscription)
					r.Delete("/", httpViews.CancelSubscription)
					r.Get("/limits", httpViews.GetLimits)
					r.Post("/end_trial", httpViews.EndTrialNowAndChargeForUser)
				})
				r.Route("/credits", func(r chi.Router) {
					r.Get("/", httpViews.GetAICreditsBalance)
					r.Get("/transactions", httpViews.GetAICreditTransactions)
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
					r.Route("/members", func(r chi.Router) {
						r.Get("/", httpViews.GetOrganizationMembers)
						r.Post("/invite", httpViews.InviteOrganizationMember)
						r.Route("/{memberId}", func(r chi.Router) {
							r.Put("/role", httpViews.UpdateMemberRole)
							r.Delete("/", httpViews.RemoveOrganizationMember)
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
				r.Get("/dashboard", httpViews.GetAdminDashboard)
			})

			r.Post("/support/ticket", httpViews.CreateTicket)

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
		})
		// V2 App Builder routes
		r.Route("/v2", func(r chi.Router) {
			r.Use(rateLimiter.RateLimit(600, time.Minute, appMiddleware.UserKey))

			r.Route("/apps", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireSubscription("active", "trial"))

				// Generation endpoints (AI rate limited)
				r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/generate/from-image", httpViews.GenerateAppFromImage)
				r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/generate/stream", httpViews.GenerateAppStream)

				// Job clarification endpoint
				r.Post("/jobs/{jobId}/clarify", httpViews.ClarifyJob)

				r.Get("/", httpViews.GetApps)

				r.Route("/{appId}", func(r chi.Router) {
					r.Get("/", httpViews.GetApp)
					r.Put("/", httpViews.UpdateApp)
					r.Delete("/", httpViews.DeleteApp)

					// Files (code-first CRUD)
					r.Get("/files/{platform}", httpViews.GetAppFiles)
					r.Post("/files", httpViews.CreateAppFile)
					r.Put("/files/{fid}", httpViews.UpdateAppFile)
					r.Delete("/files/{fid}", httpViews.DeleteAppFile)

					// Chat
					r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/chat", httpViews.ChatApp)
					r.Get("/conversation", httpViews.GetAppConversation)
					r.Delete("/conversation", httpViews.ClearAppConversation)

					// Integrations
					r.Route("/integrations", func(r chi.Router) {
						r.Get("/", httpViews.GetAppIntegrations)
						r.Post("/", httpViews.ConnectAppIntegration)
						r.Route("/{iid}", func(r chi.Router) {
							r.Put("/", httpViews.UpdateAppIntegration)
							r.Delete("/", httpViews.DisconnectAppIntegration)
						})
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

					// Preview (Docker-based live preview)
					r.Post("/preview", httpViews.StartAppPreview)
					r.Get("/preview/status", httpViews.GetAppPreviewStatus)
					r.Get("/preview/logs", httpViews.GetAppPreviewLogs)
					r.Delete("/preview", httpViews.StopAppPreview)

					// Deploy
					r.Route("/deploy", func(r chi.Router) {
						r.Post("/validate", httpViews.ValidateAppFiles)
						r.Post("/web", httpViews.DeployAppWeb)
						r.Post("/android", httpViews.DeployAppAndroid)
						r.Post("/ios", httpViews.DeployAppIOS)
						r.Get("/status", httpViews.GetDeployStatus)
						r.Get("/history", httpViews.GetDeployHistory)
					})

					// Snapshots
					r.Route("/snapshots", func(r chi.Router) {
						r.Get("/", httpViews.GetAppSnapshots)
						r.Post("/", httpViews.CreateAppSnapshot)
						r.Post("/{sid}/restore", httpViews.RestoreAppSnapshot)
						r.Delete("/{sid}", httpViews.DeleteAppSnapshot)
					})

					// Export
					r.Get("/export/{platform}", httpViews.ExportApp)

					// Domains
					r.Route("/domains", func(r chi.Router) {
						r.Get("/", httpViews.GetAppDomains)
						r.Post("/", httpViews.AddAppDomain)
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
						r.Route("/tables/{table}", func(r chi.Router) {
							r.Get("/rows", httpViews.GetDatabaseTableRows)
							r.Post("/rows", httpViews.InsertDatabaseRow)
							r.Patch("/rows", httpViews.UpdateDatabaseRow)
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

					// E-commerce store (only for ecommerce projects)
					r.Route("/store", func(r chi.Router) {
						r.Get("/", httpViews.GetAppStore)
						r.Put("/settings", httpViews.UpdateAppStoreSettings)
						r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/chat", httpViews.ChatStoreManagement)

						r.Route("/products", func(r chi.Router) {
							r.Get("/", httpViews.GetStoreProducts)
							r.Post("/", httpViews.CreateStoreProduct)
							r.Route("/{productId}", func(r chi.Router) {
								r.Get("/", httpViews.GetStoreProduct)
								r.Put("/", httpViews.UpdateStoreProduct)
								r.Delete("/", httpViews.DeleteStoreProduct)
							})
						})

						r.Route("/categories", func(r chi.Router) {
							r.Get("/", httpViews.GetStoreCategories)
							r.Post("/", httpViews.CreateStoreCategory)
							r.Route("/{categoryId}", func(r chi.Router) {
								r.Put("/", httpViews.UpdateStoreCategory)
								r.Delete("/", httpViews.DeleteStoreCategory)
							})
						})

						r.Route("/discounts", func(r chi.Router) {
							r.Get("/", httpViews.GetStoreDiscounts)
							r.Post("/", httpViews.CreateStoreDiscount)
							r.Route("/{discountId}", func(r chi.Router) {
								r.Put("/", httpViews.UpdateStoreDiscount)
								r.Delete("/", httpViews.DeleteStoreDiscount)
							})
						})

						r.Route("/orders", func(r chi.Router) {
							r.Get("/", httpViews.GetStoreOrders)
							r.Route("/{orderId}", func(r chi.Router) {
								r.Get("/", httpViews.GetStoreOrder)
								r.Put("/", httpViews.UpdateStoreOrderStatus)
							})
						})

						r.Get("/analytics", httpViews.GetStoreAnalytics)
					})
				})
			})

			// Public storefront (no auth, per-store-per-IP rate limiting)
			r.Route("/storefront/{storeId}", func(r chi.Router) {
				// Fix 3: Read endpoints — relaxed rate limit
				r.Use(rateLimiter.RateLimit(120, time.Minute, appMiddleware.StorefrontIPKey))
				r.Get("/config", httpViews.StorefrontGetStoreConfig)
				r.Get("/products", httpViews.StorefrontGetProducts)
				r.Get("/products/{slug}", httpViews.StorefrontGetProduct)
				r.Get("/categories", httpViews.StorefrontGetCategories)
				r.Get("/cart/{sessionId}", httpViews.StorefrontGetCart)
				r.Get("/orders/{orderId}", httpViews.StorefrontGetOrder)

				// Fix 3: Write endpoints — tighter rate limit
				r.With(rateLimiter.RateLimit(40, time.Minute, appMiddleware.StorefrontIPKey)).Post("/cart", httpViews.StorefrontCreateCart)
				r.With(rateLimiter.RateLimit(40, time.Minute, appMiddleware.StorefrontIPKey)).Post("/cart/{sessionId}/items", httpViews.StorefrontAddCartItem)
				r.With(rateLimiter.RateLimit(40, time.Minute, appMiddleware.StorefrontIPKey)).Put("/cart/{sessionId}/items/{itemId}", httpViews.StorefrontUpdateCartItem)
				r.With(rateLimiter.RateLimit(40, time.Minute, appMiddleware.StorefrontIPKey)).Delete("/cart/{sessionId}/items/{itemId}", httpViews.StorefrontDeleteCartItem)
				r.With(rateLimiter.RateLimit(40, time.Minute, appMiddleware.StorefrontIPKey)).Post("/cart/{sessionId}/discount", httpViews.StorefrontApplyDiscount)
				r.With(rateLimiter.RateLimit(40, time.Minute, appMiddleware.StorefrontIPKey)).Delete("/cart/{sessionId}/discount", httpViews.StorefrontRemoveDiscount)
				r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.StorefrontIPKey)).Post("/checkout", httpViews.StorefrontCheckout)
				r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.StorefrontIPKey)).Post("/checkout/confirm", httpViews.StorefrontConfirmCheckout)

				// Fix 14: Iyzico payment callback
				r.Post("/checkout/iyzico-callback", httpViews.StorefrontIyzicoCallback)

				// Reviews
				r.Get("/products/{slug}/reviews", httpViews.StorefrontGetReviews)
				r.With(rateLimiter.RateLimit(10, time.Minute, appMiddleware.StorefrontIPKey)).Post("/products/{slug}/reviews", httpViews.StorefrontSubmitReview)

				// Wishlist
				r.Get("/wishlist/{sessionId}", httpViews.StorefrontGetWishlist)
				r.With(rateLimiter.RateLimit(40, time.Minute, appMiddleware.StorefrontIPKey)).Post("/wishlist/{sessionId}", httpViews.StorefrontAddToWishlist)
				r.With(rateLimiter.RateLimit(40, time.Minute, appMiddleware.StorefrontIPKey)).Delete("/wishlist/{sessionId}/{productId}", httpViews.StorefrontRemoveFromWishlist)
			})

			// Fix 15: Stripe webhook for per-store accounts (outside storeId route)
			r.Post("/storefront/webhooks/stripe", httpViews.StorefrontStripeWebhook)

			// Public integration providers
			r.Get("/integrations/providers", httpViews.GetV2IntegrationProviders)
		})

		r.Get("/health", httpViews.Health)
	})

	// Proxy
	proxyRoute := chi.NewRouter()
	proxyRoute.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			httplog.SetAttrs(ctx, slog.String("from", "PROXYLOG"))
			httplog.SetAttrs(ctx, slog.String("proxy.path", r.URL.Path))

			urlCookie, err := r.Cookie(constants.SessionCookieName)
			if err == nil {
				httplog.SetAttrs(ctx, slog.String("proxy.url", urlCookie.Value))
			}

			tokenString, err := utils.ExtractTokenFromHeader(r)
			if err == nil {
				httplog.SetAttrs(ctx, slog.String("user.token", tokenString[0:12]+"..."))
				userInformationRes, err := httpViews.Controller.GetUser(tokenString)
				if err == nil {
					httplog.SetAttrs(ctx, slog.String("user.id", userInformationRes.ID.String()))
					httplog.SetAttrs(ctx, slog.String("user.email", userInformationRes.Email))
				}
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	proxyRoute.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*", "http://127.0.0.1:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "Location"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	proxyRoute.Use(middleware.Logger)
	proxyRoute.Use(middleware.Recoverer)
	proxyRoute.Use(middleware.Timeout(60 * time.Second))
	proxyRoute.HandleFunc("/*", httpViews.ProxyHandler)

	// Create explicit http.Server instances for graceful shutdown
	mainServer := &http.Server{Addr: ":8001", Handler: r}
	proxyServer := &http.Server{Addr: ":9001", Handler: proxyRoute}

	go func() {
		slog.Info("Proxy server listening on :9001")
		if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Proxy server error", "error", err)
		}
	}()

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

		// Flush PostHog events
		if posthogClient != nil {
			posthogClient.Close()
		}

		// Shutdown both servers
		if err := proxyServer.Shutdown(ctx); err != nil {
			slog.Error("Proxy server shutdown error", "error", err)
		}
		if err := mainServer.Shutdown(ctx); err != nil {
			slog.Error("Main server shutdown error", "error", err)
		}

		slog.Info("Servers shut down gracefully")
	}()

	// Main server (blocks until Shutdown is called)
	slog.Info("Main server listening on :8001")
	if err := mainServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Main server error", "error", err)
	}
}
