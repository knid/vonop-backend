// Package bootstrap centralizes the application bootstrap (secrets, DB,
// services, controller) so cmd/main.go (API server), cmd/worker (Asynq
// workers), and cmd/migrate-to-fly (one-shot migration) can share wiring.
package bootstrap

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/database"
	crmServices "github.com/knid/wemob/internal/services/crm"
	e2bService "github.com/knid/wemob/internal/services/e2b"
	emailServices "github.com/knid/wemob/internal/services/email"
	"github.com/knid/wemob/internal/services"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/services/coolify"
	"github.com/knid/wemob/internal/services/fly"
	"github.com/knid/wemob/internal/services/gitea"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/mcpclient"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"

	infisical "github.com/infisical/go-sdk"
	slogbetterstack "github.com/samber/slog-betterstack"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/extra/bundebug"

	_ "github.com/lib/pq"
)

// BootstrapController returns a fully-wired Controller and the Asynq Redis
// options used to construct it. The Redis options are also needed by the
// worker binary to create an asynq.Server against the same Redis instance.
// Panics on startup errors (mirrors the original main()'s os.Exit(1)
// behavior on fatal init failures).
func BootstrapController() (*controllers.Controller, asynq.RedisClientOpt) {
	infisicalClient := infisical.NewInfisicalClient(context.Background(), infisical.Config{
		SiteUrl: "https://eu.infisical.com",
	})

	infisicalClientID := os.Getenv("INFISICAL_CLIENT_ID")
	infisicalClientSecret := os.Getenv("INFISICAL_CLIENT_SECRET")

	if infisicalClientID == "" || infisicalClientSecret == "" {
		panic(fmt.Errorf("bootstrap: Infisical client ID or secret is not set in environment variables"))
	}

	_, err := infisicalClient.Auth().UniversalAuthLogin(infisicalClientID, infisicalClientSecret)
	if err != nil {
		panic(fmt.Errorf("bootstrap: failed to authenticate with Infisical: %w", err))
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

	slog.Info("DB connection config", "host", dbHost, "port", dbPort, "db", dbName, "sslmode", dbSSL)

	sqldb := sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithDSN(dsn),
		pgdriver.WithTLSConfig(&tls.Config{InsecureSkipVerify: true}),
		pgdriver.WithDialTimeout(5*time.Second),
		pgdriver.WithReadTimeout(30*time.Second),
		pgdriver.WithWriteTimeout(10*time.Second),
	))
	sqldb.SetMaxOpenConns(25)
	sqldb.SetMaxIdleConns(10)
	sqldb.SetConnMaxLifetime(10 * time.Minute)
	sqldb.SetConnMaxIdleTime(2 * time.Minute)
	bundb := bun.NewDB(sqldb, pgdialect.New())

	if _, err := bundb.Exec("CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";"); err != nil {
		panic(fmt.Errorf("bootstrap: failed to create uuid-ossp extension: %w", err))
	}

	bundb.AddQueryHook(bundebug.NewQueryHook(
		bundebug.WithVerbose(os.Getenv("BUNDEBUG") == "2"),
		bundebug.FromEnv("BUNDEBUG"),
	))

	bundb.AddQueryHook(utils.NewQueryHook(utils.QueryHookOptions{
		Logger:       logger,
		SlowDuration: 200 * time.Millisecond,
	}))

	db := database.NewDatabase(bundb)

	assetsStorage, err := storage.NewS3Storage(sm.GetSecret("AWS_REGION"), sm.GetSecret("AWS_S3_ASSETS_BUCKET_NAME"), sm.GetSecret("AWS_ACCESS_KEY_ID"), sm.GetSecret("AWS_SECRET_ACCESS_KEY"))
	if err != nil {
		panic(fmt.Errorf("bootstrap: failed to initialize assets S3 storage: %w", err))
	}
	userApiKeysStorage, err := storage.NewS3Storage(sm.GetSecret("AWS_REGION"), sm.GetSecret("AWS_S3_USER_API_KEYS_BUCKET_NAME"), sm.GetSecret("AWS_ACCESS_KEY_ID"), sm.GetSecret("AWS_SECRET_ACCESS_KEY"))
	if err != nil {
		panic(fmt.Errorf("bootstrap: failed to initialize user API keys S3 storage: %w", err))
	}

	// Git repo service for project file storage
	repoBasePath := "/data/repos"
	if envPath := os.Getenv("REPO_BASE_PATH"); envPath != "" {
		repoBasePath = envPath
	}
	gitRepoService := gitrepo.NewService(repoBasePath)
	slog.Info("Git repo service initialized", "base_path", repoBasePath)

	// Codemagic CI/CD for mobile builds (optional — nil if not configured)
	var codemagicService *codemagic.Service
	if cmToken := sm.GetSecret("CODEMAGIC_API_TOKEN"); cmToken != "" {
		codemagicService = codemagic.NewService(cmToken, sm.GetSecret("CODEMAGIC_APP_ID"))
		slog.Info("Codemagic service initialized")
	}

	e2b := e2bService.NewService(
		sm.GetSecret("DASHBOARD_URL"),
		sm.GetSecret("E2B_PREVIEW_DOMAIN"),
	)

	coolifyService := coolify.NewService(
		sm.GetSecret("COOLIFY_API_URL"),
		sm.GetSecret("COOLIFY_API_TOKEN"),
		sm.GetSecret("COOLIFY_SERVER_UUID"),
		sm.GetSecret("COOLIFY_PROJECT_UUID"),
		sm.GetSecret("COOLIFY_ENVIRONMENT_NAME"),
		sm.GetSecret("COOLIFY_PRIVATE_KEY_UUID"),
		sm.GetSecret("COOLIFY_WEBHOOK_SECRET"),
	)
	flyService := fly.NewService(
		sm.GetSecret("FLY_API_TOKEN"),
		sm.GetSecret("FLY_ORG_SLUG"),
		sm.GetSecret("FLY_PRIMARY_REGION"),
		sm.GetSecret("FLY_WEBHOOK_SECRET"),
	)
	giteaService := gitea.NewService(
		sm.GetSecret("GITEA_URL"),
		sm.GetSecret("GITEA_API_TOKEN"),
		sm.GetSecret("GITEA_USERNAME"),
		sm.GetSecret("GITEA_ORG"),
		sm.GetSecret("GITEA_SSH_PORT"),
	)

	// Neon service (optional — only enabled when API key is set)
	var neonService *neon.Service
	if neonKey := sm.GetSecret("NEON_API_KEY"); neonKey != "" {
		neonOrgID := sm.GetSecret("NEON_ORG_ID")
		neonService = neon.NewServiceWithOrg(neonKey, neonOrgID)
		slog.Info("Neon database service enabled", "org_scoped", neonOrgID != "")
	}

	// PostHog client (optional — only enabled when API key is set)
	var posthogClient posthog.Client
	if phKey := sm.GetSecret("POSTHOG_API_KEY"); phKey != "" {
		phClient, phErr := posthog.NewWithConfig(phKey, posthog.Config{
			Endpoint:       "https://eu.i.posthog.com",
			PersonalApiKey: sm.GetSecret("POSTHOG_PERSONAL_API_KEY"),
		})
		if phErr != nil {
			slog.Warn("PostHog initialization failed", "error", phErr)
		} else {
			posthogClient = phClient
			slog.Info("PostHog analytics enabled")
		}
	}

	remoteConfigService := remoteconfig.New(posthogClient)
	slog.Info("RemoteConfig service initialized")

	redisOpts := asynq.RedisClientOpt{
		Addr:      sm.GetSecret("REDIS_ADDR"),
		Username:  sm.GetSecret("REDIS_USERNAME"),
		Password:  sm.GetSecret("REDIS_PASSWORD"),
		TLSConfig: &tls.Config{},
	}

	controller := controllers.NewController(
		db,
		make(chan struct{}, constants.MAX_CONCURRENT_BUILDS),
		services.NewSlackService(sm.GetSecret("SLACK_API_KEY")),
		emailServices.NewBrevoEmailService(sm.GetSecret("BREVO_API_KEY")),
		crmServices.NewBrevoCRMService(sm.GetSecret("BREVO_API_KEY")),
		asynq.NewClient(redisOpts),
		asynq.NewInspector(redisOpts),
		assetsStorage,
		userApiKeysStorage,
		sm,
		coolifyService,
		flyService,
		giteaService,
		neonService,
		posthogClient,
		gitRepoService,
		codemagicService,
		remoteConfigService,
		e2b,
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
				controller.MCPService.Store(svc)
				slog.Info("MCP service enabled", "tool_count", len(svc.Tools()))
			}
		}()
	}

	return controller, redisOpts
}

// BunDB opens and returns the Bun DB handle from a fully-bootstrapped
// Controller's underlying database.Database. Callers that need the raw
// *bun.DB (e.g. for schema migrations) can call this after BootstrapController.
func BunDB(c *controllers.Controller) *bun.DB {
	return c.DB.DB
}
