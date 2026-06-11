// Package bootstrap centralizes the application bootstrap (secrets, DB,
// services, controller) so cmd/main.go (API server) and cmd/worker (Asynq
// workers) share wiring.
package bootstrap

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	awsCreds "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/agentruntime"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/services"
	caddyService "github.com/knid/wemob/internal/services/caddy"
	"github.com/knid/wemob/internal/services/codemagic"
	e2bService "github.com/knid/wemob/internal/services/e2b"
	"github.com/knid/wemob/internal/services/events"
	"github.com/knid/wemob/internal/services/fly"
	"github.com/knid/wemob/internal/services/gitea"
	"github.com/knid/wemob/internal/services/giteabackup"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/mcpclient"
	"github.com/knid/wemob/internal/services/meta"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/revenuecat"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/redis/go-redis/v9"

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
	ism := secret.NewInfisicalSecretManager(
		infisicalClient.(*infisical.InfisicalClient),
		os.Getenv("INFISICAL_PROJECT_ID"),
		os.Getenv("ENVIRONMENT"),
	)
	ism.WarmCache()
	sm = ism

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

	// Fail fast if Infisical returned empty for any required DB secret.
	// Without this, a flaky Infisical fetch (the same one that silently
	// broke the worker on 2026-05-07) would build a malformed DSN and
	// every query would fail at runtime instead of crashing at boot.
	// Docker's restart: unless-stopped policy then retries the bootstrap,
	// usually picking up the right secret on the second attempt.
	if dbHost == "" || dbPort == "" || dbName == "" || dbUser == "" || dbPass == "" {
		slog.Error("bootstrap: required DB secret(s) empty — Infisical fetch failed; refusing to start",
			"db_host_set", dbHost != "",
			"db_port_set", dbPort != "",
			"db_name_set", dbName != "",
			"db_user_set", dbUser != "",
			"db_password_set", dbPass != "",
		)
		os.Exit(1)
	}

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
	maxConns := 25
	if v := os.Getenv("DB_MAX_OPEN_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConns = n
		}
	}
	sqldb.SetMaxOpenConns(maxConns)
	sqldb.SetMaxIdleConns(10)
	sqldb.SetConnMaxLifetime(10 * time.Minute)
	sqldb.SetConnMaxIdleTime(2 * time.Minute)
	bundb := bun.NewDB(sqldb, pgdialect.New())

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
	androidKeysStorage, err := storage.NewS3Storage(sm.GetSecret("AWS_REGION"), sm.GetSecret("S3_ANDROID_KEYS_BUCKET"), sm.GetSecret("AWS_ACCESS_KEY_ID"), sm.GetSecret("AWS_SECRET_ACCESS_KEY"))
	if err != nil {
		panic(fmt.Errorf("bootstrap: failed to initialize android keys S3 storage: %w", err))
	}

	// CredStore — envelope-encryption-at-rest for stored credentials.
	// Two modes:
	//   1. WEMOB_CREDENTIAL_KMS_KEY_ID set → real AWS KMS (production)
	//   2. WEMOB_CREDENTIAL_LOCAL_DEK_HEX set → local-only DEK (dev/staging)
	// Exactly one must be set. Local mode skips the KMS round-trip — real
	// AES-256-GCM stays in place but no key rotation, no IAM gating, no
	// CloudTrail audit log. Rows encrypted under local mode are NOT readable
	// by KMS mode (the persisted DEK blob is meaningless to KMS.Decrypt).
	credStore := initCredStore(sm, userApiKeysStorage)

	// Git repo service for project file storage
	repoBasePath := "/data/repos"
	if envPath := os.Getenv("REPO_BASE_PATH"); envPath != "" {
		repoBasePath = envPath
	}
	gitRepoService := gitrepo.NewService(repoBasePath)
	slog.Info("Git repo service initialized", "base_path", repoBasePath)

	bareRoot := os.Getenv("WEMOB_GIT_BARE_ROOT")
	worktreeRoot := os.Getenv("WEMOB_WORKTREE_ROOT")
	legacyRoot := os.Getenv("WEMOB_LEGACY_REPO_ROOT")
	if legacyRoot == "" {
		legacyRoot = repoBasePath
	}
	bareGitRepoService := gitrepo.NewBareService(bareRoot, worktreeRoot, legacyRoot)
	slog.Info("Deep Agents Git workspace service initialized",
		"bare_root", bareGitRepoService.BareRoot,
		"worktree_root", bareGitRepoService.WorktreeRoot,
		"legacy_root", bareGitRepoService.LegacyRoot,
	)

	var deepAgentsClient *agentruntime.Client
	if runtimeURL := strings.TrimSpace(sm.GetSecret("AGENT_RUNTIME_URL")); runtimeURL != "" {
		deepAgentsClient = agentruntime.NewClient(
			runtimeURL,
			sm.GetSecret("AGENT_RUNTIME_INTERNAL_TOKEN"),
			sm.GetSecret("AGENT_RUNTIME_ACCESS_TOKEN_SECRET"),
		)
		slog.Info("Deep Agents runtime client initialized", "runtime_url", runtimeURL)
	} else {
		slog.Warn("AGENT_RUNTIME_URL not set — Deep Agents runtime client disabled")
	}

	// Codemagic CI/CD for mobile builds (optional — nil if not configured)
	var codemagicService *codemagic.Service
	if cmToken := sm.GetSecret("CODEMAGIC_API_TOKEN"); cmToken != "" {
		codemagicService = codemagic.NewService(cmToken, sm.GetSecret("CODEMAGIC_APP_ID"))
		slog.Info("Codemagic service initialized")
	}

	// RevenueCat REST v2 — IAP auto-provisioning per tenant (P2.3).
	// Gracefully degrades when REVENUECAT_API_KEY is unset: provisioning
	// skips, mobile builds proceed with empty EXPO_PUBLIC_REVENUECAT_KEY_*
	// env vars, and the template's lib/iap.ts initIAP() already no-ops +
	// warns on missing keys. Set REVENUECAT_API_KEY in Infisical with
	// scopes project_configuration:projects:read_write +
	// project_configuration:apps:read_write before shipping IAP-enabled
	// mobile_app_native projects to production.
	var revenueCatService *revenuecat.Service
	if rcKey := sm.GetSecret("REVENUECAT_API_KEY"); rcKey != "" {
		revenueCatService = revenuecat.NewService(rcKey)
		slog.Info("RevenueCat service initialized")
	} else {
		slog.Warn("REVENUECAT_API_KEY not set — IAP auto-provisioning disabled (mobile_app_native projects will build with empty IAP keys)")
	}

	// Meta Conversions API — server-side ad-conversion events. Returns nil
	// when either secret is unset; downstream calls become no-ops.
	metaCAPI := meta.NewService(
		sm.GetSecret("META_PIXEL_ID"),
		sm.GetSecret("META_CAPI_ACCESS_TOKEN"),
		sm.GetSecret("META_CAPI_TEST_EVENT_CODE"),
	)
	if metaCAPI != nil {
		slog.Info("Meta CAPI service initialized")
	} else {
		slog.Info("Meta CAPI disabled (META_PIXEL_ID or META_CAPI_ACCESS_TOKEN unset)")
	}

	e2b := e2bService.NewService(
		sm.GetSecret("DASHBOARD_URL"),
		sm.GetSecret("E2B_PREVIEW_DOMAIN"),
		sm.GetSecret("E2B_AGENT_CONTROL_TOKEN"),
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

	// Async backup of every project commit to Gitea. Decoupled from the
	// deploy path: Fly's builder reads the local working tree directly, so
	// Gitea is purely a durability mirror in case the API container's volume
	// is lost. The hook is fire-and-forget; commits never block on it.
	if giteaService != nil && sm.GetSecret("GITEA_URL") != "" {
		backup := giteabackup.New(giteaService, gitRepoService)
		gitRepoService.SetCommitHook(backup.OnCommit)
		slog.Info("Gitea per-commit backup enabled")
	}

	// Neon service: agent-plan two-org wiring. Free org is sponsored by Neon
	// and carries free-tier projects; Paid org is metered against Wemob's
	// credits and holds Starter/Professional projects. The personal API key
	// is required for cross-org transfers on plan change. Falls back to the
	// legacy single NEON_API_KEY when only one key is set so existing
	// environments keep booting.
	var neonService *neon.Service
	var neonRegistry *neon.Registry
	{
		legacyKey := sm.GetSecret("NEON_API_KEY")
		legacyOrgID := sm.GetSecret("NEON_ORG_ID")
		freeKey := sm.GetSecret("NEON_FREE_ORG_API_KEY")
		freeOrgID := sm.GetSecret("NEON_FREE_ORG_ID")
		paidKey := sm.GetSecret("NEON_PAID_ORG_API_KEY")
		paidOrgID := sm.GetSecret("NEON_PAID_ORG_ID")
		personalKey := sm.GetSecret("NEON_PERSONAL_API_KEY")

		var freeSvc, paidSvc *neon.Service
		if freeKey != "" {
			freeSvc = neon.NewServiceWithOrg(freeKey, freeOrgID)
		}
		if paidKey != "" {
			paidSvc = neon.NewServiceWithOrg(paidKey, paidOrgID)
		}
		// Legacy fallback: single NEON_API_KEY/NEON_ORG_ID drives both tiers.
		if freeSvc == nil && paidSvc == nil && legacyKey != "" {
			legacySvc := neon.NewServiceWithOrg(legacyKey, legacyOrgID)
			freeSvc = legacySvc
			paidSvc = legacySvc
		}

		if freeSvc != nil || paidSvc != nil {
			reg, err := neon.NewRegistry(freeSvc, paidSvc, personalKey)
			if err != nil {
				slog.Error("Neon registry init failed", "error", err)
			} else {
				neonRegistry = reg
				// Back-compat: NeonService points at the paid org so legacy
				// callers (provisioner before tier wiring, app deletion) keep
				// working unchanged.
				neonService = reg.For(neon.TierPaid)
				slog.Info("Neon registry enabled",
					"free_org", freeOrgID, "paid_org", paidOrgID,
					"transfer_enabled", reg.CanTransfer())
			}
		}
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

	// Fail fast on empty Redis secrets. Asynq defaults Addr="" to localhost:6379
	// (which doesn't exist on prod), and Asynq surfaces this only as a stream of
	// "dial tcp [::1]:6379: connect: connection refused" errors after startup —
	// the worker keeps running but never processes a task. The 2026-05-07
	// silent-worker incident (worker dialing localhost for 6+ hours after a
	// deploy) was exactly this. Crash at boot instead, and let Docker
	// restart: unless-stopped retry the bootstrap with a fresh Infisical fetch.
	// REDIS_USERNAME stays optional (managed Valkey doesn't require it).
	redisAddr := sm.GetSecret("REDIS_ADDR")
	redisPassword := sm.GetSecret("REDIS_PASSWORD")
	if redisAddr == "" || redisPassword == "" {
		slog.Error("bootstrap: required Redis secret(s) empty — Infisical fetch failed; refusing to start",
			"redis_addr_set", redisAddr != "",
			"redis_password_set", redisPassword != "",
		)
		os.Exit(1)
	}

	redisOpts := asynq.RedisClientOpt{
		Addr:      redisAddr,
		Username:  sm.GetSecret("REDIS_USERNAME"),
		Password:  redisPassword,
		TLSConfig: &tls.Config{},
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:      redisOpts.Addr,
		Username:  redisOpts.Username,
		Password:  redisOpts.Password,
		TLSConfig: redisOpts.TLSConfig,
	})

	edgeIPsCSV := sm.GetSecret("EDGE_IPS")
	var edgeIPs []string
	if edgeIPsCSV != "" {
		parts := strings.Split(edgeIPsCSV, ",")
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				edgeIPs = append(edgeIPs, trimmed)
			}
		}
	}

	caddyAdmin := caddyService.NewAdmin(
		os.Getenv("CADDY_ADMIN_URL"),
		os.Getenv("CADDY_CUSTOM_DOMAINS_DIR"),
		"/etc/caddy/Caddyfile",
	)

	var resendSegmentIDs []string
	for _, id := range strings.Split(sm.GetSecret("RESEND_SEGMENT_IDS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			resendSegmentIDs = append(resendSegmentIDs, id)
		}
	}
	eventService := events.NewResendService(db, sm.GetSecret("RESEND_API_KEY"), resendSegmentIDs...)

	controller := controllers.NewController(
		db,
		make(chan struct{}, constants.MAX_CONCURRENT_BUILDS),
		services.NewSlackService(sm.GetSecret("SLACK_API_KEY")),
		eventService,
		asynq.NewClient(redisOpts),
		asynq.NewInspector(redisOpts),
		redisClient,
		assetsStorage,
		userApiKeysStorage,
		androidKeysStorage,
		credStore,
		sm,
		flyService,
		giteaService,
		neonService,
		neonRegistry,
		posthogClient,
		gitRepoService,
		bareGitRepoService,
		deepAgentsClient,
		codemagicService,
		revenueCatService,
		metaCAPI,
		remoteConfigService,
		e2b,
		edgeIPs,
		caddyAdmin,
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

// initCredStore wires CredStore against either AWS KMS (production) or a
// local static DEK (dev/staging — set WEMOB_CREDENTIAL_LOCAL_DEK_HEX to a
// 32-byte hex string from `openssl rand -hex 32`).
//
// Exactly one of WEMOB_CREDENTIAL_KMS_KEY_ID or WEMOB_CREDENTIAL_LOCAL_DEK_HEX
// must be set. KMS wins if both are present (so dev configs that get pushed
// to prod with a KMS key alongside don't silently downgrade).
//
// Panics on misconfiguration — every credential read/write path assumes
// CredStore is non-nil after bootstrap.
func initCredStore(sm secret.SecretManager, s3Storage *storage.S3Storage) *storage.CredStore {
	kmsKeyID := sm.GetSecret("WEMOB_CREDENTIAL_KMS_KEY_ID")
	localDEKHex := sm.GetSecret("WEMOB_CREDENTIAL_LOCAL_DEK_HEX")

	switch {
	case kmsKeyID != "":
		kmsRegion := sm.GetSecret("AWS_REGION")
		awsCfg, err := awsConfig.LoadDefaultConfig(context.Background(),
			awsConfig.WithRegion(kmsRegion),
			awsConfig.WithCredentialsProvider(awsCreds.NewStaticCredentialsProvider(
				sm.GetSecret("AWS_ACCESS_KEY_ID"),
				sm.GetSecret("AWS_SECRET_ACCESS_KEY"),
				"",
			)),
		)
		if err != nil {
			panic(fmt.Errorf("bootstrap: aws config for KMS: %w", err))
		}
		kmsClient := kms.NewFromConfig(awsCfg)
		kmsManager := secret.NewKMSManager(kmsClient, kmsKeyID)
		slog.Info("CredStore: AWS KMS mode", "kms_key", kmsKeyID, "bucket", s3Storage.Bucket)
		return storage.NewCredStore(s3Storage, kmsManager)

	case localDEKHex != "":
		localManager, err := secret.NewLocalDEKManager(localDEKHex)
		if err != nil {
			panic(fmt.Errorf("bootstrap: WEMOB_CREDENTIAL_LOCAL_DEK_HEX: %w", err))
		}
		slog.Warn("CredStore: LOCAL DEK mode (no KMS) — dev/staging only. " +
			"Real AES-256-GCM is in place but no key rotation, no IAM gating, " +
			"no audit log. Rows encrypted under local mode are NOT readable " +
			"by KMS mode — switching requires re-encrypting all existing rows.")
		return storage.NewCredStore(s3Storage, localManager)

	default:
		panic("bootstrap: one of WEMOB_CREDENTIAL_KMS_KEY_ID (production) or " +
			"WEMOB_CREDENTIAL_LOCAL_DEK_HEX (dev/staging — generate with " +
			"`openssl rand -hex 32`) must be set")
	}
}
