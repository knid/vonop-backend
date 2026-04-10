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
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/secret"
	e2bService "github.com/knid/wemob/internal/services/e2b"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/services/coolify"
	"github.com/knid/wemob/internal/services/gitea"
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
		// AppGeneratedFile and AppSnapshot removed — files now in git repos
		(*models.CoolifyProject)(nil),
		(*models.AppDomain)(nil),
		// Custom environment variables
		(*models.AppEnvVar)(nil),
		// User-uploaded assets (3D models, fonts, media, etc.)
		(*models.AppAsset)(nil),
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
		// Mobile app config (feature flags, plugin toggles, bridge settings)
		(*models.MobileAppConfig)(nil),
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
		// app_snapshots removed — snapshots now use git repos
		// V2 app builder fields on limits
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS monthly_app_count int NOT NULL DEFAULT 0`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS monthly_deploy_count int NOT NULL DEFAULT 0`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_use_ecommerce boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_deploy_to_vercel boolean NOT NULL DEFAULT false`,
		// Conversation summary for older message summarization
		`ALTER TABLE app_conversations ADD COLUMN IF NOT EXISTS summary text DEFAULT ''`,
		// Data models for per-app Neon database provisioning
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS data_models jsonb DEFAULT '[]'`,
		// Allow NULL price_id so empty Stripe secrets don't conflict on unique constraint
		`ALTER TABLE standart_user_subscriptions ALTER COLUMN price_id DROP NOT NULL`,
		`UPDATE standart_user_subscriptions SET price_id = NULL WHERE price_id = ''`,
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
		`ALTER TABLE app_domains ADD COLUMN IF NOT EXISTS deployment_uuid text`,
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
		// Fix screenshot_s3_key and sandbox_id: empty string instead of NULL
		`UPDATE app_projects SET screenshot_s3_key = '' WHERE screenshot_s3_key IS NULL`,
		`UPDATE app_projects SET sandbox_id = '' WHERE sandbox_id IS NULL`,
		`ALTER TABLE app_projects ALTER COLUMN screenshot_s3_key SET DEFAULT ''`,
		`ALTER TABLE app_projects ALTER COLUMN sandbox_id SET DEFAULT ''`,
		// Logo S3 key for AI-generated app logos
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS logo_s3_key text NOT NULL DEFAULT ''`,
		// Subscription & limits system v2
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS max_deployed_web_apps int NOT NULL DEFAULT 0`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS credit_rollover_months int NOT NULL DEFAULT 0`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_access_code boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_buy_domain boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_export_to_github boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_use_email_service boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_build_mobile boolean NOT NULL DEFAULT false`,
		// Mobile app config: new capability fields
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS permissions text[] DEFAULT '{}'`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS url_scheme text DEFAULT ''`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS universal_link_domain text DEFAULT ''`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS offline_enabled boolean NOT NULL DEFAULT false`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS caching_strategy text DEFAULT 'network-first'`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS biometric_enabled boolean NOT NULL DEFAULT false`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS dark_mode_enabled boolean NOT NULL DEFAULT true`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS rating_enabled boolean NOT NULL DEFAULT false`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS rating_sessions int NOT NULL DEFAULT 5`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS update_check_enabled boolean NOT NULL DEFAULT false`,
		// GitHub integration fields on users
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS git_hub_access_token text`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS git_hub_username text`,
		// GitHub export limit on limits
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS can_export_to_github boolean NOT NULL DEFAULT false`,
		// Deploy resource columns on limits
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS deploy_memory_mb int NOT NULL DEFAULT 256`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS deploy_cpus text NOT NULL DEFAULT '0.25'`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS deploy_db_max_cu text NOT NULL DEFAULT '0.25'`,
		`ALTER TABLE standart_user_limits ADD COLUMN IF NOT EXISTS deploy_db_storage_mb int NOT NULL DEFAULT 512`,

		// Referral two-tier rewards
		`ALTER TABLE referrals ADD COLUMN IF NOT EXISTS subscription_reward_granted boolean NOT NULL DEFAULT false`,
		// Template marketplace: track which template a project was copied from
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS template_id uuid`,
	}

	// Deduplicate before creating unique indexes (keep the latest row per group)
	dedupeStatements := []string{
		// app_generated_files removed — files now in git repos
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
		// app_generated_files index removed — files now in git repos
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_app_conversations_project ON app_conversations (app_project_id)`,
		// Web analytics indexes
		`CREATE INDEX IF NOT EXISTS idx_web_analytics_project_created ON web_analytics_events (project_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_web_analytics_project_type_created ON web_analytics_events (project_id, event_type, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_web_analytics_project_session ON web_analytics_events (project_id, session_id)`,
		// Token lookup index — covers the soft-delete filter used by GetTokenByToken
		`CREATE INDEX IF NOT EXISTS idx_tokens_string_active ON tokens (string) WHERE deleted_at IS NULL`,
		// GitHub export lookup index
		`CREATE INDEX IF NOT EXISTS idx_app_github_exports_app_project_id ON app_github_exports(app_project_id)`,
	}
	alterStatements = append(alterStatements, indexStatements...)
	// Check if migrations have already been applied by testing for a column from the latest batch.
	// This avoids running ALTER TABLE statements on every startup, which take AccessExclusiveLock
	// and block all reads on the affected tables.
	var migrationsDone bool
	row := db.QueryRowContext(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='app_projects' AND column_name='template_id')")
	if err := row.Scan(&migrationsDone); err != nil {
		migrationsDone = false
	}

	if !migrationsDone {
		slog.Info("Running schema migrations (first time or new columns needed)")
		db.ExecContext(context.Background(), "SET lock_timeout = '3s'")
		for _, stmt := range alterStatements {
			if _, err := db.ExecContext(context.Background(), stmt); err != nil {
				slog.Warn("alter table skipped", "statement", stmt, "error", err)
			}
		}
		db.ExecContext(context.Background(), "SET lock_timeout = '0'")
	} else {
		slog.Info("Schema migrations already applied, skipping")
	}

	if !migrationsDone {
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
	}

	// Performance indexes (CREATE INDEX IF NOT EXISTS is cheap — no exclusive lock)
	perfIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_users_customer_id ON users(customer_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_project_id ON sessions(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_project_status ON sessions(project_id, status) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_project_created ON sessions(project_id, created_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_status_active ON sessions(status) WHERE status = 'active' AND deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_app_usage_events_session_id ON app_usage_events(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_app_usage_events_session_created ON app_usage_events(session_id, created_at DESC) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_products_store_id ON products(store_id)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_store_id ON orders(store_id)`,
		`CREATE INDEX IF NOT EXISTS idx_carts_store_id_session_id ON carts(store_id, session_id)`,
		// app_snapshots index removed — snapshots now use git repos
		`CREATE INDEX IF NOT EXISTS idx_notifications_project_id ON notifications(project_id)`,
		// Foreign key indexes for tables frequently queried by user_id or project_id
		`CREATE INDEX IF NOT EXISTS idx_app_projects_user_id ON app_projects(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_app_projects_organization_id ON app_projects(organization_id)`,
		`CREATE INDEX IF NOT EXISTS idx_builds_project_id ON builds(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tokens_user_id ON tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_user_id ON projects(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_releases_project_id ON releases(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_app_conversations_app_project_id ON app_conversations(app_project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_app_deployments_app_project_id ON app_deployments(app_project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_app_domains_app_project_id ON app_domains(app_project_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_app_domains_domain_unique ON app_domains(domain) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_app_env_vars_app_project_id ON app_env_vars(app_project_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_app_env_vars_project_key ON app_env_vars(app_project_id, key) WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_app_integrations_app_project_id ON app_integrations(app_project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_stores_app_project_id ON stores(app_project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_app_dashboards_app_project_id ON app_dashboards(app_project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_deployments_project_id ON deployments(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_credits_balances_user_id ON ai_credits_balances(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_credit_transactions_user_id ON ai_credit_transactions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_credit_transactions_user_created ON ai_credit_transactions(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_organization_members_user_id ON organization_members(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_organization_members_organization_id ON organization_members(organization_id)`,
		`CREATE INDEX IF NOT EXISTS idx_coolify_projects_app_project_id ON coolify_projects(app_project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_user_id ON webhook_endpoints(user_id)`,
	}
	for _, stmt := range perfIndexes {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			slog.Warn("create index failed", "statement", stmt, "error", err)
		}
	}

	return nil
}

func seedPlans(db *database.Database, sm secret.SecretManager) {
	type planDef struct {
		Name         string
		Description  string
		MonthlyPrice int
		PriceID      string
		AnnualPrice  int
		AnnualPriceID string
		Limits       models.StandartUserLimits
	}

	plans := []planDef{
		{
			Name:         "Free",
			Description:  "Free plan with limited features",
			MonthlyPrice: 0,
			PriceID:      "free_plan",
			Limits: models.StandartUserLimits{
				MaximumProjectCount:     1,
				MaximumCredentialCount:  0,
				MontlyBuildCount:        1,
				MontlyNotificationCount: 3,
				MonthlyAICredits:        50,
				MaxWebApps:              1,
				MaxMobileApps:           0,
				MaxDeployedWebApps:      1,
				DeployMemoryMB:          256,
				DeployCPUs:              "0.25",
				DeployDBMaxCU:           "0.25",
				DeployDBStorageMB:       512,
				CreditRolloverMonths:    0,
				CanBuildToAndroid:       true,
				CanBuildToIOS:           true,
				CanBuildMobile:          false,
				CanUseAnalytics:         false,
				CanAccessCode:           false,
				CanBuyDomain:            false,
				CanUseEmailService:      false,
				CanUseEcommerce:         true,
				CanExportToGitHub:       false,
				CanDeployWeb:            true,
				CanRemoveBrandLogo:      false,
			},
		},
		{
			Name:          "Starter",
			Description:   "Starter plan for growing projects",
			MonthlyPrice:  2000,
			PriceID:       sm.GetSecret("STRIPE_STARTER_PRICE_ID"),
			AnnualPrice:   19200,
			AnnualPriceID: sm.GetSecret("STRIPE_STARTER_ANNUAL_PRICE_ID"),
			Limits: models.StandartUserLimits{
				MaximumProjectCount:     20,
				MaximumCredentialCount:  2,
				MontlyBuildCount:        10,
				MontlyNotificationCount: 50,
				MonthlyAICredits:        100,
				MaxWebApps:              20,
				MaxMobileApps:           0,
				MaxDeployedWebApps:      3,
				DeployMemoryMB:          512,
				DeployCPUs:              "0.5",
				DeployDBMaxCU:           "0.5",
				DeployDBStorageMB:       2048,
				CreditRolloverMonths:    1,
				CanBuildToAndroid:       true,
				CanBuildToIOS:           true,
				CanBuildMobile:          true,
				CanUseAnalytics:         true,
				CanAccessCode:           true,
				CanBuyDomain:            true,
				CanUseEmailService:      false,
				CanUseEcommerce:         true,
				CanExportToGitHub:       true,
				CanDeployWeb:            true,
				CanRemoveBrandLogo:      true,
			},
		},
		{
			Name:          "Professional",
			Description:   "Professional plan for power users",
			MonthlyPrice:  4000,
			PriceID:       sm.GetSecret("STRIPE_PROFESSIONAL_PRICE_ID"),
			AnnualPrice:   38400,
			AnnualPriceID: sm.GetSecret("STRIPE_PROFESSIONAL_ANNUAL_PRICE_ID"),
			Limits: models.StandartUserLimits{
				MaximumProjectCount:     50,
				MaximumCredentialCount:  10,
				MontlyBuildCount:        50,
				MontlyNotificationCount: 200,
				MonthlyAICredits:        200,
				MaxWebApps:              100,
				MaxMobileApps:           5,
				MaxDeployedWebApps:      10,
				DeployMemoryMB:          1024,
				DeployCPUs:              "1",
				DeployDBMaxCU:           "1",
				DeployDBStorageMB:       10240,
				CreditRolloverMonths:    1,
				CanBuildToAndroid:       true,
				CanBuildToIOS:           true,
				CanBuildMobile:          true,
				CanUseAnalytics:         true,
				CanAccessCode:           true,
				CanBuyDomain:            true,
				CanUseEmailService:      true,
				CanUseEcommerce:         true,
				CanExportToGitHub:       true,
				CanDeployWeb:            true,
				CanRemoveBrandLogo:      true,
			},
		},
	}

	for _, p := range plans {
		// First check including soft-deleted rows to prevent unique constraint violations
		existing, err := db.GetSubscriptionByNameIncludeDeleted(p.Name)
		if err != nil {
			// Plan truly doesn't exist — create limits then subscription
			limits := p.Limits
			createdLimits, err := db.CreateStandartUserLimits(&limits)
			if err != nil {
				slog.Error("failed to create limits for plan", "plan", p.Name, "error", err)
				continue
			}
			sub := &models.StandartUserSubscription{
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
		limits, err := db.GetStandartUserLimitsById(existing.LimitsID.String())
		if err != nil {
			slog.Error("failed to get limits for plan", "plan", p.Name, "error", err)
			continue
		}

		limits.MaximumProjectCount = p.Limits.MaximumProjectCount
		limits.MonthlyAICredits = p.Limits.MonthlyAICredits
		limits.MaxWebApps = p.Limits.MaxWebApps
		limits.MaxMobileApps = p.Limits.MaxMobileApps
		limits.MaxDeployedWebApps = p.Limits.MaxDeployedWebApps
		limits.DeployMemoryMB = p.Limits.DeployMemoryMB
		limits.DeployCPUs = p.Limits.DeployCPUs
		limits.DeployDBMaxCU = p.Limits.DeployDBMaxCU
		limits.DeployDBStorageMB = p.Limits.DeployDBStorageMB
		limits.CreditRolloverMonths = p.Limits.CreditRolloverMonths
		limits.CanBuildToAndroid = p.Limits.CanBuildToAndroid
		limits.CanBuildToIOS = p.Limits.CanBuildToIOS
		limits.CanBuildMobile = p.Limits.CanBuildMobile
		limits.CanUseAnalytics = p.Limits.CanUseAnalytics
		limits.CanAccessCode = p.Limits.CanAccessCode
		limits.CanBuyDomain = p.Limits.CanBuyDomain
		limits.CanUseEmailService = p.Limits.CanUseEmailService
		limits.CanUseEcommerce = p.Limits.CanUseEcommerce
		limits.CanDeployWeb = p.Limits.CanDeployWeb
		limits.CanRemoveBrandLogo = p.Limits.CanRemoveBrandLogo
		limits.MaximumCredentialCount = p.Limits.MaximumCredentialCount
		limits.MontlyBuildCount = p.Limits.MontlyBuildCount
		limits.MontlyNotificationCount = p.Limits.MontlyNotificationCount
		limits.CanExportToGitHub = p.Limits.CanExportToGitHub

		if _, err := db.UpdateStandartUserLimits(limits); err != nil {
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

func migrateInactiveUsersToFree(bundb *bun.DB) {
	// Assign free plan to inactive users
	res, err := bundb.ExecContext(context.Background(), `
		UPDATE users SET subscription_status = 'free', subscription_id = (
			SELECT id FROM standart_user_subscriptions WHERE name = 'Free' LIMIT 1
		) WHERE subscription_status = 'inactive'
		AND (subscription_id IS NULL OR subscription_id = '00000000-0000-0000-0000-000000000000')
		AND deleted_at IS NULL
	`)
	if err != nil {
		slog.Warn("migrate inactive users to free failed", "error", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("migrated inactive users to free plan", "count", n)
	}

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
		slog.Error("Failed to create uuid-ossp extension", "error", err)
		os.Exit(1)
	}

	bundb.AddQueryHook(bundebug.NewQueryHook(
		bundebug.WithVerbose(os.Getenv("BUNDEBUG") == "2"),
		bundebug.FromEnv("BUNDEBUG"),
	))

	bundb.AddQueryHook(utils.NewQueryHook(utils.QueryHookOptions{
		Logger:       logger,
		SlowDuration: 200 * time.Millisecond, // Omit to log all operations as debug
	}))

	if err := createSchema(bundb); err != nil {
		slog.Error("Failed to create schema", "error", err)
		os.Exit(1)
	}

	db := database.NewDatabase(bundb)

	seedPlans(db, sm)
	migrateInactiveUsersToFree(bundb)

	go utils.CheckSessionStatusJob(db)
	go utils.CheckUsageAlertsJob(db, crmServices.NewBrevoCRMService(sm.GetSecret("BREVO_API_KEY")))
	coolifyService := coolify.NewService(
		sm.GetSecret("COOLIFY_API_URL"),
		sm.GetSecret("COOLIFY_API_TOKEN"),
		sm.GetSecret("COOLIFY_SERVER_UUID"),
		sm.GetSecret("COOLIFY_PROJECT_UUID"),
		sm.GetSecret("COOLIFY_ENVIRONMENT_NAME"),
		sm.GetSecret("COOLIFY_PRIVATE_KEY_UUID"),
		sm.GetSecret("COOLIFY_WEBHOOK_SECRET"),
	)
	// Register Coolify webhook for deployment status updates (non-blocking)
	go func() {
		webhookURL := constants.WEMOB_API_BASE_URL + "/webhooks/coolify?secret=" + coolifyService.WebhookSecret
		if err := coolifyService.RegisterWebhook(webhookURL); err != nil {
			slog.Warn("Failed to register Coolify webhook — falling back to polling", "error", err)
		}
	}()
	giteaService := gitea.NewService(
		sm.GetSecret("GITEA_URL"),
		sm.GetSecret("GITEA_API_TOKEN"),
		sm.GetSecret("GITEA_USERNAME"),
		sm.GetSecret("GITEA_ORG"),
		sm.GetSecret("GITEA_SSH_PORT"),
	)

	// GitHub OAuth secrets (required for GitHub export feature)
	// GITHUB_CLIENT_ID — OAuth App client ID
	// GITHUB_CLIENT_SECRET — OAuth App client secret
	// GITHUB_REDIRECT_URI — OAuth callback URL (e.g. https://api.wemob.io/v2/auth/github/callback)
	// GITHUB_TOKEN_ENCRYPTION_KEY — 64-char hex string for AES-256-GCM token encryption

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

	assetsStorage, err := storage.NewS3Storage(sm.GetSecret("AWS_REGION"), sm.GetSecret("AWS_S3_ASSETS_BUCKET_NAME"), sm.GetSecret("AWS_ACCESS_KEY_ID"), sm.GetSecret("AWS_SECRET_ACCESS_KEY"))
	if err != nil {
		slog.Error("Failed to initialize assets S3 storage", "error", err)
		os.Exit(1)
	}
	userApiKeysStorage, err := storage.NewS3Storage(sm.GetSecret("AWS_REGION"), sm.GetSecret("AWS_S3_USER_API_KEYS_BUCKET_NAME"), sm.GetSecret("AWS_ACCESS_KEY_ID"), sm.GetSecret("AWS_SECRET_ACCESS_KEY"))
	if err != nil {
		slog.Error("Failed to initialize user API keys S3 storage", "error", err)
		os.Exit(1)
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
		assetsStorage,
		userApiKeysStorage,
		sm,
		coolifyService,
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

	// Backfill v1 Projects for deployed mobile_app AppProjects that don't have one yet
	go controller.BackfillV1ProjectsForMobileApps()

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
		AllowedOrigins:   []string{"https://app.wemob.io", "https://dashboard.wemob.io", "https://wemob.io", "https://www.wemob.io", "http://localhost:3000", "http://127.0.0.1:3000", "https://*.wemob.app", "https://*.vercel.app", "https://device1.zebu-pain.ts.net"},
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
		Level: slog.LevelInfo,

		Schema:        httplog.SchemaOTEL,
		RecoverPanics: true,
		LogRequestBody:  func(req *http.Request) bool { return false },
		LogResponseBody: func(req *http.Request) bool { return false },
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

	r.Route("/", func(r chi.Router) {
		r.Route("/webhooks", func(r chi.Router) {
			r.Post("/stripe", httpViews.Controller.HandleWebhook)
			// Store payment webhooks
			r.Post("/store/stripe/{storeId}", httpViews.HandleStoreStripeWebhook)
			r.Post("/store/iyzico/{storeId}", httpViews.HandleStoreIyzicoWebhook)
			// Coolify deployment webhook
			r.Post("/coolify", httpViews.HandleCoolifyWebhook)
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
				r.Post("/auth/google/mobile", httpViews.GoogleMobileLogin)
			})
			r.Get("/tasks", httpViews.GetAllTasks)
			r.Post("/tasks/{taskId}/cancel", httpViews.CancelBuildTask)
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
				})
				r.Route("/credits", func(r chi.Router) {
					r.Get("/", httpViews.GetAICreditsBalance)
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

			// Public template marketplace (no auth)
			r.Route("/templates", func(r chi.Router) {
				r.Get("/", httpViews.ListPublishedTemplates)
				r.Get("/{slug}", httpViews.GetPublishedTemplate)
			})

			// Template copy (requires auth + subscription)
			r.With(appMiddleware.RequireAuth(controller), appMiddleware.RequireSubscription("active", "trial", "free")).
				Post("/templates/{slug}/copy", httpViews.CopyTemplate)

			r.Route("/apps", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireSubscription("active", "trial", "free"))

				// Generation endpoints (AI rate limited)
				r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/generate/from-image", httpViews.GenerateAppFromImage)
				r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/generate/stream", httpViews.GenerateAppStream)

				// Job clarification endpoint
				r.Post("/jobs/{jobId}/clarify", httpViews.ClarifyJob)
				r.Post("/jobs/{jobId}/reply", httpViews.ReplyJob)
			r.Get("/jobs/{jobId}/stream", httpViews.StreamJob)

				r.Get("/", httpViews.GetApps)

				r.Route("/{appId}", func(r chi.Router) {
					r.Get("/", httpViews.GetApp)
					r.Get("/init", httpViews.GetAppInit)
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

					// Preview bundle (WebContainer-based, client-side)
					r.Get("/preview-bundle", httpViews.GetPreviewBundle)

					// Sandbox ID persistence for E2B preview
					r.Get("/sandbox", httpViews.GetAppSandbox)
					r.Put("/sandbox", httpViews.SetAppSandbox)
					// Sandbox lifecycle for mobile preview
					r.Post("/sandbox/start", httpViews.StartAppSandbox)
					r.Get("/sandbox/status", httpViews.GetAppSandboxStatus)

					// Deploy
					r.Route("/deploy", func(r chi.Router) {
						r.Post("/validate", httpViews.ValidateAppFiles)
						r.Post("/web", httpViews.DeployAppWeb)
						r.Delete("/web", httpViews.UndeployAppWeb)
						r.Get("/status", httpViews.GetDeployStatus)
						r.Get("/history", httpViews.GetDeployHistory)
					})

					// GitHub export
					r.Route("/github", func(r chi.Router) {
						r.Get("/", httpViews.GetGitHubExport)
						r.Post("/export", httpViews.ExportToGitHub)
						r.Post("/push", httpViews.PushToGitHub)
						r.Delete("/", httpViews.RemoveGitHubExport)
					})

					// Mobile builds
					r.Route("/build", func(r chi.Router) {
						r.Post("/android", httpViews.BuildAppAndroid)
						r.Post("/ios", httpViews.BuildAppIOS)
					})

					r.Post("/convert-to-mobile", httpViews.ConvertToMobile)
					r.Post("/mobile-optimize", httpViews.MobileOptimize)
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

					// Gateway key management
					r.Get("/gateway-key", httpViews.GetGatewayKeyInfo)
					r.Post("/gateway-key/reveal", httpViews.RevealGatewayKey)
					r.Post("/gateway-key/rotate", httpViews.RotateGatewayKey)
				})
			})

			// Mobile intent classification
			r.With(appMiddleware.RequireAuth(controller), appMiddleware.RequireSubscription("active", "trial", "free"),
				rateLimiter.RateLimit(30, time.Minute, appMiddleware.AIUserKey)).
				Post("/mobile/classify-intent", httpViews.ClassifyMobileIntent)

			r.Route("/mobile/{projectId}", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireSubscription("active", "trial", "free"))

				r.Get("/config", httpViews.GetMobileAppConfig)
				r.Put("/config", httpViews.UpdateMobileAppConfig)
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

			// Public integration providers
			r.Get("/integrations/providers", httpViews.GetV2IntegrationProviders)

			// GitHub OAuth
			r.Get("/auth/github", httpViews.GitHubAuthRedirect)
			r.Get("/auth/github/callback", httpViews.GitHubAuthCallback)
			r.Delete("/auth/github", httpViews.DisconnectGitHub)
			r.Get("/auth/github/status", httpViews.GetGitHubConnectionStatus)
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

		// Shutdown main server
		if err := mainServer.Shutdown(ctx); err != nil {
			slog.Error("Main server shutdown error", "error", err)
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
