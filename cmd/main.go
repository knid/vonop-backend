package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
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
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/bootstrap"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/database"
	appMiddleware "github.com/knid/wemob/internal/middleware"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/pendingnudge"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/services/storepoll"
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
		(*models.Invoice)(nil),
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
		(*models.AppReleaseTask)(nil),
		(*models.Ticket)(nil),
		(*models.NavBarItems)(nil),
		(*models.JSInjectData)(nil),
		(*models.GooglePlayConnection)(nil),
		(*models.AppleConnection)(nil),
		(*models.Deployment)(nil),
		(*models.SignIPATask)(nil),
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
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS monthly_ai_credits int NOT NULL DEFAULT 0`,
		// Annual billing fields on subscriptions
		`ALTER TABLE standard_user_subscriptions ADD COLUMN IF NOT EXISTS annual_price int DEFAULT 0`,
		`ALTER TABLE standard_user_subscriptions ADD COLUMN IF NOT EXISTS annual_price_id text DEFAULT ''`,
		// app_snapshots removed — snapshots now use git repos
		// V2 app builder fields on limits
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS monthly_app_count int NOT NULL DEFAULT 0`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS monthly_deploy_count int NOT NULL DEFAULT 0`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS can_use_ecommerce boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS can_deploy_web boolean NOT NULL DEFAULT false`,
		// Conversation summary for older message summarization
		`ALTER TABLE app_conversations ADD COLUMN IF NOT EXISTS summary text DEFAULT ''`,
		// Data models for per-app Neon database provisioning
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS data_models jsonb DEFAULT '[]'`,
		// Partial streaming state (thinking text, chat text, tool calls, conversation buffer)
		// for refresh recovery of in-flight generation jobs.
		`ALTER TABLE generation_jobs ADD COLUMN IF NOT EXISTS stream_state jsonb DEFAULT '{}'::jsonb`,
		// Allow NULL price_id so empty Stripe secrets don't conflict on unique constraint
		`ALTER TABLE standard_user_subscriptions ALTER COLUMN price_id DROP NOT NULL`,
		`UPDATE standard_user_subscriptions SET price_id = NULL WHERE price_id = ''`,
		// soft_delete column for all tables that use bun:",soft_delete,nullzero"
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS twenty_person_id text`,
		// User locale preference — drives email template language selection (Resend).
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS language text NOT NULL DEFAULT 'en'`,
		`ALTER TABLE user_notifications ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE tokens ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE standard_user_subscriptions ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE projects ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE builds ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE notifications ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE store_page_images ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_build_tasks ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_build_tasks ADD COLUMN IF NOT EXISTS error_message text`,
		`ALTER TABLE app_release_tasks ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE tickets ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
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
		`ALTER TABLE app_dashboards ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_dashboard_panels ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE app_notification_tasks ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		// Fix screenshot_s3_key and sandbox_id: empty string instead of NULL
		`UPDATE app_projects SET screenshot_s3_key = '' WHERE screenshot_s3_key IS NULL`,
		`UPDATE app_projects SET sandbox_id = '' WHERE sandbox_id IS NULL`,
		`ALTER TABLE app_projects ALTER COLUMN screenshot_s3_key SET DEFAULT ''`,
		`ALTER TABLE app_projects ALTER COLUMN sandbox_id SET DEFAULT ''`,
		// Logo S3 key for AI-generated app logos
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS logo_s3_key text NOT NULL DEFAULT ''`,
		// Subscription & limits system v2
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS max_deployed_web_apps int NOT NULL DEFAULT 0`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS max_deployed_mobile_apps int NOT NULL DEFAULT 0`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS can_deploy_mobile boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS credit_rollover_months int NOT NULL DEFAULT 0`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS can_access_code boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS can_buy_domain boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS can_export_to_github boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS can_use_email_service boolean NOT NULL DEFAULT false`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS can_build_mobile boolean NOT NULL DEFAULT false`,
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
		// P2.4 — Better-Auth integration
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS auth_enabled boolean NOT NULL DEFAULT false`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS auth_providers text[] DEFAULT '{}'`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS auth_scheme text`,
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS better_auth_secret_cred_ref jsonb`,
		// P2.3 — RevenueCat IAP auto-provisioning per tenant
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS iap_enabled boolean NOT NULL DEFAULT false`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS revenuecat_project_id text`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS revenuecat_ios_app_id text`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS revenuecat_android_app_id text`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS revenuecat_public_key_ios text`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS revenuecat_public_key_android text`,
		// Force-update / min_version (P3.2)
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS min_build_number_ios bigint NOT NULL DEFAULT 0`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS min_build_number_android bigint NOT NULL DEFAULT 0`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS min_version_message text`,
		// GitHub integration fields on users
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS git_hub_access_token text`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS git_hub_username text`,
		// GitHub export limit on limits
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS can_export_to_github boolean NOT NULL DEFAULT false`,
		// Deploy resource columns on limits
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS deploy_memory_mb int NOT NULL DEFAULT 256`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS deploy_cpus text NOT NULL DEFAULT '0.25'`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS deploy_db_max_cu text NOT NULL DEFAULT '0.25'`,
		`ALTER TABLE standard_user_limits ADD COLUMN IF NOT EXISTS deploy_db_storage_mb int NOT NULL DEFAULT 512`,

		// Referral two-tier rewards
		`ALTER TABLE referrals ADD COLUMN IF NOT EXISTS subscription_reward_granted boolean NOT NULL DEFAULT false`,
		// Template marketplace: track which template a project was copied from
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS template_id uuid`,
		// Link generation jobs to their app so clients can look up the active
		// job on refresh via GET /v2/apps/{appId}/active-job (replaces the old
		// localStorage-based job-id recovery).
		`ALTER TABLE generation_jobs ADD COLUMN IF NOT EXISTS app_id uuid`,
		// Parts column: chronological ordered record of each assistant turn.
		// Replaces the brittle narration_offset approach (byte offsets into
		// the content string didn't survive the Go-bytes vs JS-UTF16 boundary).
		// New writes set this; legacy rows keep their empty default and the
		// dashboard falls back to the old content/tool_calls render path.
		`ALTER TABLE app_chat_messages ADD COLUMN IF NOT EXISTS parts jsonb NOT NULL DEFAULT '[]'`,
		// Attachments column: denormalized snapshot of asset metadata
		// per chat turn. Replaces the legacy images column, which only
		// stored URLs and dropped filename/mime/size — see
		// docs/superpowers/specs/2026-04-19-chat-attachments-design.md.
		`ALTER TABLE app_chat_messages ADD COLUMN IF NOT EXISTS attachments jsonb NOT NULL DEFAULT '[]'`,
		// Fractional gateway credit debt — accumulator for sub-credit costs
		// on Wemob AI Gateway chat traffic. Gateway calls price from upstream
		// USD and almost never land on a whole credit, so the fractional
		// remainder is tracked here until it crosses an integer.
		`ALTER TABLE ai_credits_balances ADD COLUMN IF NOT EXISTS pending_gateway double precision NOT NULL DEFAULT 0`,
		// Fly migration: add Fly fields to runtime_apps (renamed from coolify_projects).
		// The RENAME itself lives in renameStatements below so it runs unconditionally.
		`ALTER TABLE runtime_apps ADD COLUMN IF NOT EXISTS fly_app_name text NOT NULL DEFAULT ''`,
		`ALTER TABLE runtime_apps ADD COLUMN IF NOT EXISTS current_image_ref text NOT NULL DEFAULT ''`,
		`ALTER TABLE runtime_apps ADD COLUMN IF NOT EXISTS last_deployed_at timestamptz`,
		// DNS verification fields on app_domains — see Task 1.2 of the Fly migration plan.
		// dns_check_status: empty=not checked, "pending"=waiting for DNS, "verified"=ok, "failed"=gave up.
		// dns_instructions: JSON blob surfaced in the UI so users know what records to add.
		`ALTER TABLE app_domains ADD COLUMN IF NOT EXISTS dns_check_status text NOT NULL DEFAULT ''`,
		`ALTER TABLE app_domains ADD COLUMN IF NOT EXISTS dns_instructions jsonb NOT NULL DEFAULT '{}'::jsonb`,
		`CREATE INDEX IF NOT EXISTS idx_app_domains_dns_check_status ON app_domains(dns_check_status) WHERE dns_check_status = 'pending'`,
		// Org foundation: personal-workspace flag (2026-04-29-org-foundation)
		`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS is_personal boolean NOT NULL DEFAULT false`,
		// Org foundation: enforce non-null organization_id after backfill (2026-04-29-org-foundation)
		`UPDATE app_projects SET organization_id = (
			SELECT id FROM organizations
			WHERE owner_id = app_projects.user_id AND is_personal = true
			LIMIT 1
		) WHERE organization_id IS NULL`,
		`ALTER TABLE app_projects ALTER COLUMN organization_id SET NOT NULL`,
		// Org-scoped credits: add organization_id to balance + transactions, member cap (2026-04-30-org-scoped-credits)
		`ALTER TABLE ai_credits_balances ADD COLUMN IF NOT EXISTS organization_id uuid`,
		`ALTER TABLE ai_credits_balances ALTER COLUMN user_id DROP NOT NULL`,
		`ALTER TABLE ai_credit_transactions ADD COLUMN IF NOT EXISTS organization_id uuid`,
		`ALTER TABLE organization_members ADD COLUMN IF NOT EXISTS monthly_credit_cap int`,
		// Drop legacy UNIQUE constraint on user_id (was 1-balance-per-user; now 1-per-org).
		// The constraint name is auto-generated by Bun (commonly ai_credits_balances_user_id_key).
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'ai_credits_balances'::regclass AND contype = 'u' AND conname LIKE '%user_id%')
			THEN EXECUTE (
				SELECT 'ALTER TABLE ai_credits_balances DROP CONSTRAINT ' || conname
				FROM pg_constraint WHERE conrelid = 'ai_credits_balances'::regclass AND contype = 'u' AND conname LIKE '%user_id%'
				LIMIT 1
			);
			END IF;
		END $$;`,
		// Org-scoped credits: enforce after backfill (2026-04-30-org-scoped-credits)
		`UPDATE ai_credits_balances SET organization_id = (
			SELECT id FROM organizations WHERE owner_id = ai_credits_balances.user_id AND is_personal = true LIMIT 1
		) WHERE organization_id IS NULL AND user_id IS NOT NULL`,
		`UPDATE ai_credit_transactions SET organization_id = (
			SELECT id FROM organizations WHERE owner_id = ai_credit_transactions.user_id AND is_personal = true LIMIT 1
		) WHERE organization_id IS NULL`,
		`ALTER TABLE ai_credits_balances ALTER COLUMN organization_id SET NOT NULL`,
		`ALTER TABLE ai_credit_transactions ALTER COLUMN organization_id SET NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_ai_credits_balances_org ON ai_credits_balances(organization_id)`,
		// Plan 3a: org-scope connections, deployments, api keys, webhooks (2026-04-30-org-filter-list-endpoints)
		`ALTER TABLE apple_connections ADD COLUMN IF NOT EXISTS organization_id uuid`,
		`ALTER TABLE google_play_connections ADD COLUMN IF NOT EXISTS organization_id uuid`,
		`ALTER TABLE deployments ADD COLUMN IF NOT EXISTS organization_id uuid`,
		`ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS organization_id uuid`,
		`ALTER TABLE webhook_endpoints ADD COLUMN IF NOT EXISTS organization_id uuid`,
		// Plan 3a: enforce NOT NULL after backfill (2026-04-30)
		`UPDATE apple_connections t SET organization_id = (SELECT id FROM organizations WHERE owner_id = t.user_id AND is_personal = true LIMIT 1) WHERE organization_id IS NULL AND user_id IS NOT NULL`,
		`UPDATE google_play_connections t SET organization_id = (SELECT id FROM organizations WHERE owner_id = t.user_id AND is_personal = true LIMIT 1) WHERE organization_id IS NULL AND user_id IS NOT NULL`,
		`UPDATE deployments t SET organization_id = (SELECT id FROM organizations WHERE owner_id = t.user_id AND is_personal = true LIMIT 1) WHERE organization_id IS NULL AND user_id IS NOT NULL`,
		`UPDATE api_keys t SET organization_id = (SELECT id FROM organizations WHERE owner_id = t.user_id AND is_personal = true LIMIT 1) WHERE organization_id IS NULL AND user_id IS NOT NULL`,
		`UPDATE webhook_endpoints t SET organization_id = (SELECT id FROM organizations WHERE owner_id = t.user_id AND is_personal = true LIMIT 1) WHERE organization_id IS NULL AND user_id IS NOT NULL`,
		`ALTER TABLE apple_connections ALTER COLUMN organization_id SET NOT NULL`,
		`ALTER TABLE google_play_connections ALTER COLUMN organization_id SET NOT NULL`,
		`ALTER TABLE deployments ALTER COLUMN organization_id SET NOT NULL`,
		`ALTER TABLE api_keys ALTER COLUMN organization_id SET NOT NULL`,
		`ALTER TABLE webhook_endpoints ALTER COLUMN organization_id SET NOT NULL`,
		// Plan 5: billing rebind to org (2026-04-30-stripe-customer-to-org)
		`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS stripe_customer_id text NOT NULL DEFAULT ''`,
		`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS subscription_id uuid`,
		`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS subscription_status text NOT NULL DEFAULT 'inactive'`,
		`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS subscription_start_at timestamptz`,
		`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS trial_start_at timestamptz`,
		`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS trial_used boolean NOT NULL DEFAULT false`,
		// Gateway pending accumulator was keyed on user_id while balances are
		// org-scoped post Plan A — stranded fractional debt accumulated on
		// whichever row the buggy WHERE user_id = ? happened to match. Fold
		// that debt onto each user's personal-org row so nothing is lost.
		`ALTER TABLE ai_credits_balances ADD COLUMN IF NOT EXISTS gateway_pending_backfilled_v1 boolean NOT NULL DEFAULT false`,
		`UPDATE ai_credits_balances target
		 SET pending_gateway = sub.total_pending,
		     gateway_pending_backfilled_v1 = true,
		     updated_at = NOW()
		 FROM (
		     SELECT po.id AS personal_org_id, SUM(b.pending_gateway) AS total_pending
		     FROM ai_credits_balances b
		     JOIN organizations po ON po.owner_id = b.user_id
		      AND po.is_personal = true
		      AND po.deleted_at IS NULL
		     WHERE b.user_id IS NOT NULL
		       AND b.pending_gateway > 0
		     GROUP BY po.id
		     HAVING SUM(b.pending_gateway) > 0
		 ) sub
		 WHERE target.organization_id = sub.personal_org_id
		   AND NOT target.gateway_pending_backfilled_v1`,
		`UPDATE ai_credits_balances b
		 SET pending_gateway = 0,
		     gateway_pending_backfilled_v1 = true,
		     updated_at = NOW()
		 FROM organizations o
		 WHERE b.organization_id = o.id
		   AND o.is_personal = false
		   AND b.pending_gateway > 0
		   AND NOT b.gateway_pending_backfilled_v1`,
		// Domain status sync backfill: VerifyAppDomain wrote only `status` while
		// the cron wrote only `dns_check_status`, so existing rows are split.
		// Caddy's on-demand-TLS gate honors only dns_check_status — user-verified
		// domains were 502'ing. Reconcile both columns and add a marker so the
		// canary advances. See controllers/app_domain_controller.go.
		`ALTER TABLE app_domains ADD COLUMN IF NOT EXISTS status_sync_backfilled_v1 boolean NOT NULL DEFAULT false`,
		`UPDATE app_domains SET dns_check_status = 'verified' WHERE status = 'active' AND dns_check_status != 'verified' AND deleted_at IS NULL`,
		`UPDATE app_domains SET status = 'active' WHERE dns_check_status = 'verified' AND status != 'active' AND deleted_at IS NULL`,
		`UPDATE app_domains SET status_sync_backfilled_v1 = true WHERE NOT status_sync_backfilled_v1`,
		// Marker column for the integration-credentials-at-rest encryption
		// backfill. The actual rewrite happens in Go (see backfillEncryptIntegrations
		// in cmd/main.go) because Postgres can't call our AES helper without an
		// extension. Default false flips to true row-by-row as the backfill
		// encrypts each AppIntegration.
		`ALTER TABLE app_integrations ADD COLUMN IF NOT EXISTS credentials_encrypted_v1 boolean NOT NULL DEFAULT false`,
		// Structured failure classification for assistant turns. Empty on
		// success; otherwise one of the agent ErrorKind enum values used to
		// render the rich error card from history. See finaliseFailureV2.
		`ALTER TABLE app_chat_messages ADD COLUMN IF NOT EXISTS error_kind text NOT NULL DEFAULT ''`,
		// Sub-credit gateway audit: stores the fractional credit cost when a
		// gateway_chat_pending row is written (Amount=0). Nullable so legacy
		// rows and non-gateway transactions stay untouched.
		`ALTER TABLE ai_credit_transactions ADD COLUMN IF NOT EXISTS pending_amount double precision`,
		// Partial unique index: at most one non-terminal deployment per
		// (project, platform). Catches the concurrent-POST race that the
		// app-level GetBuildingAppDeployment pre-check can't cover.
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_app_deployments_in_progress
		   ON app_deployments (app_project_id, platform)
		   WHERE status IN ('queued','building','deploying')`,
		// Expo Phase 0: per-project default connections for auto-sign / auto-deploy.
		// Nil = "no default configured" — no auto-sign happens until set.
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS default_apple_connection_id uuid`,
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS default_google_connection_id uuid`,
		// P1.0: encrypted-at-rest credential refs. nullzero so legacy rows
		// without backfill stay readable; backfillEncryptCredentials populates
		// these in-process at startup.
		`ALTER TABLE apple_connections ADD COLUMN IF NOT EXISTS p8_key_cred_ref jsonb`,
		`ALTER TABLE apple_connections ADD COLUMN IF NOT EXISTS apns_p8_key_cred_ref jsonb`,
		`ALTER TABLE google_play_connections ADD COLUMN IF NOT EXISTS service_account_cred_ref jsonb`,
		// P1.2 — per-app publishing settings for Codemagic submit workflows
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS app_store_track text NOT NULL DEFAULT 'internal'`,
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS play_store_track text NOT NULL DEFAULT 'internal'`,
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS phased_release boolean NOT NULL DEFAULT false`,
		`ALTER TABLE app_projects ADD COLUMN IF NOT EXISTS submit_as_draft boolean NOT NULL DEFAULT false`,
		// P2.2 — compose-push audit log table.
		`ALTER TABLE compose_push_histories ADD COLUMN IF NOT EXISTS recipients_count integer NOT NULL DEFAULT 0`,
		// P2.1: self-hosted OTA updates feature flags.
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS ota_enabled boolean NOT NULL DEFAULT true`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS ota_release_channel text NOT NULL DEFAULT 'production'`,
		// P3.1: landing-page metadata on mobile app configs.
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS app_store_url text DEFAULT ''`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS play_store_url text DEFAULT ''`,
		`ALTER TABLE mobile_app_configs ADD COLUMN IF NOT EXISTS landing_page_published boolean NOT NULL DEFAULT false`,
		// P1#15 — locale-independent fields on the parent listing
		`ALTER TABLE app_store_listings ADD COLUMN IF NOT EXISTS copyright text NOT NULL DEFAULT ''`,
		`ALTER TABLE app_store_listings ADD COLUMN IF NOT EXISTS primary_category text NOT NULL DEFAULT ''`,
		`ALTER TABLE app_store_listings ADD COLUMN IF NOT EXISTS secondary_category text NOT NULL DEFAULT ''`,
		`ALTER TABLE app_store_listings ADD COLUMN IF NOT EXISTS eula_type text NOT NULL DEFAULT 'apple_default'`,
		`ALTER TABLE app_store_listings ADD COLUMN IF NOT EXISTS eula_url text NOT NULL DEFAULT ''`,
		// P1#15 — unique (listing_id, locale) on app_store_listing_localizations.
		// Bun's CreateTable doesn't emit composite uniques; we add it here so
		// On("CONFLICT (listing_id, locale)") in UpsertLocalization works.
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_app_store_listing_localizations_listing_locale
		   ON app_store_listing_localizations (listing_id, locale)
		   WHERE deleted_at IS NULL`,

		// <P1#16> releases — promote the legacy stub to a real release model.
		// Each ALTER is IF NOT EXISTS-idempotent. Existing rows keep their
		// project_id + version_number; new code writes to app_project_id +
		// version_code with both kept in sync at the upsert layer.
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS app_project_id uuid`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS organization_id uuid`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS platform text NOT NULL DEFAULT ''`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS track text NOT NULL DEFAULT ''`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS version_code int NOT NULL DEFAULT 0`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS marketing_version text NOT NULL DEFAULT ''`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'draft'`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS status_message text NOT NULL DEFAULT ''`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS codemagic_task_id uuid`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS build_id uuid`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS store_release_id text NOT NULL DEFAULT ''`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS phased_release_state text NOT NULL DEFAULT ''`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS rollout_fraction double precision NOT NULL DEFAULT 0`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS rolled_back_from_release_id uuid`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS submitted_at timestamptz`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS approved_at timestamptz`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS live_at timestamptz`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS rejected_at timestamptz`,
		`ALTER TABLE releases ADD COLUMN IF NOT EXISTS rejection_message text NOT NULL DEFAULT ''`,
		// Legacy notnull on project_id no longer matches the new schema —
		// app_project_id is the canonical FK going forward; keep project_id for
		// backwards compat but stop forcing inserts to populate it.
		`ALTER TABLE releases ALTER COLUMN project_id DROP NOT NULL`,
		// Composite uniqueness on (app_project_id, platform, track, version_code).
		// WHERE clause excludes soft-deleted rows so a re-submit after delete
		// gets a fresh row instead of conflicting on tombstones.
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_releases_project_platform_track_version
		   ON releases (app_project_id, platform, track, version_code)
		   WHERE deleted_at IS NULL AND app_project_id IS NOT NULL`,
		// Lookup index: webhook arrives with only a store-side id.
		`CREATE INDEX IF NOT EXISTS idx_releases_platform_store_id
		   ON releases (platform, store_release_id)
		   WHERE store_release_id <> ''`,

		// <P2#22> ota channels + rollout — promote ota_updates to channel-aware
		// rows so "preview"/"staging" publishes don't leak into the production
		// rollout, plus per-row rollout-percentage gating for staged rollouts and
		// is_active for one-click rollback. Each ALTER is IF NOT EXISTS so re-runs
		// are no-ops. Defaults preserve pre-P2#22 behavior on existing rows.
		`ALTER TABLE ota_updates ADD COLUMN IF NOT EXISTS channel text NOT NULL DEFAULT 'production'`,
		`ALTER TABLE ota_updates ADD COLUMN IF NOT EXISTS rollout_percentage smallint NOT NULL DEFAULT 100`,
		`ALTER TABLE ota_updates ADD COLUMN IF NOT EXISTS is_active boolean NOT NULL DEFAULT true`,
		`ALTER TABLE ota_updates ADD COLUMN IF NOT EXISTS rolled_back_from_update_id uuid`,
		// Manifest lookup index: covers GetLatestForChannel's most-common path.
		`CREATE INDEX IF NOT EXISTS idx_ota_updates_channel_lookup
		   ON ota_updates (app_project_id, runtime_version, platform, channel, created_at DESC)
		   WHERE is_active = true`,
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
		// Active-job lookup per app — used by GET /v2/apps/{appId}/active-job.
		`CREATE INDEX IF NOT EXISTS idx_generation_jobs_app_status ON generation_jobs (app_id, status) WHERE app_id IS NOT NULL`,
		// P2.2 — compose-push history listing index (most-recent-first per project).
		`CREATE INDEX IF NOT EXISTS idx_compose_push_histories_app_created ON compose_push_histories (app_project_id, created_at DESC)`,
		// <P2#25> mobile_subscriptions — unique key for upsert + lookup index for
		// per-project listing. WHERE deleted_at IS NULL so soft-deleted rows
		// don't block a re-purchase landing as a fresh row.
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_mobile_subscriptions_project_user_product
		   ON mobile_subscriptions (app_project_id, rc_app_user_id, product_id)
		   WHERE deleted_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_mobile_subscriptions_project_status
		   ON mobile_subscriptions (app_project_id, status) WHERE deleted_at IS NULL`,
	}
	alterStatements = append(alterStatements, indexStatements...)
	// Check if migrations have already been applied by testing for a column from the latest batch.
	// This avoids running ALTER TABLE statements on every startup, which take AccessExclusiveLock
	// and block all reads on the affected tables.
	// IMPORTANT: when adding a new ALTER TABLE above, update this canary to the newest column,
	// otherwise the new ALTER will be silently skipped on servers that already have older columns.
	// Table renames go in renameStatements below, not here — they run unconditionally.
	// Canary: ota_updates.channel — the LAST column added (<P2#22> ota
	// channels + rollout). Bumped past <P2#25>'s pg_indexes-based canary
	// (uniq_mobile_subscriptions_project_user_product) so the new
	// ota_updates ALTERs run on servers that already passed the #25 check.
	var migrationsDone bool
	row := db.QueryRowContext(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='ota_updates' AND column_name='channel')")
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

	// renameStatements runs unconditionally on every startup (outside the
	// canary-gated alterStatements). MUST be executed AFTER createSchema so
	// the target tables already exist in either their old or new form —
	// running this before createSchema would silently no-op on a fresh DB.
	// Keep all rename migrations idempotent via IF EXISTS / DO-block guards.
	//
	// Ship UX Phase 1: rename credential tables and FK columns to connections.
	// These run unconditionally (not gated by the canary) because the canary was
	// already satisfied on existing DBs before these renames were added. The
	// DO-block guards make every statement a no-op on second and subsequent runs.
	renameStatements := []string{
		`ALTER TABLE IF EXISTS app_store_credentials RENAME TO apple_connections`,
		`ALTER TABLE IF EXISTS google_play_credentials RENAME TO google_play_connections`,
		// Coolify→Fly: rename coolify_projects → runtime_apps. Guarded with a
		// DO block so it's idempotent on environments where runtime_apps was
		// already created from scratch by the new schema while coolify_projects
		// still existed. The bare ALTER ... IF EXISTS form would error with
		// "relation runtime_apps already exists" on every startup. On affected
		// environments, data was copied manually with
		// INSERT INTO runtime_apps SELECT FROM coolify_projects.
		`DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='coolify_projects')
  AND NOT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='runtime_apps') THEN
    ALTER TABLE coolify_projects RENAME TO runtime_apps;
  END IF;
END $$`,
		// Deployment FK columns — guard with DO block to stay idempotent
		`DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='deployments' AND column_name='app_store_credentials_id') THEN
    ALTER TABLE deployments RENAME COLUMN app_store_credentials_id TO apple_connection_id;
  END IF;
END $$`,
		`DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='deployments' AND column_name='google_play_credentials_id') THEN
    ALTER TABLE deployments RENAME COLUMN google_play_credentials_id TO google_play_connection_id;
  END IF;
END $$`,
		// Ensure soft-delete columns exist on the renamed tables. Moved out
		// of alterStatements so the canary can't skip them when a
		// transitional DB had the old tables + error_message column
		// simultaneously.
		`ALTER TABLE IF EXISTS apple_connections ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE IF EXISTS google_play_connections ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE IF EXISTS runtime_apps ADD COLUMN IF NOT EXISTS deleted_at timestamptz`,
		`ALTER TABLE IF EXISTS deployments ADD COLUMN IF NOT EXISTS release_notes text`,
		`ALTER TABLE IF EXISTS deployments ADD COLUMN IF NOT EXISTS rollout_track text`,

		// Coolify cleanup (post-Fly migration). Idempotent; runs once and
		// becomes a no-op on subsequent boots.
		`ALTER TABLE IF EXISTS app_domains DROP COLUMN IF EXISTS deployment_uuid`,
		`ALTER TABLE IF EXISTS runtime_apps DROP COLUMN IF EXISTS coolify_app_uuid`,
		`DROP TABLE IF EXISTS coolify_projects`,

		// Vercel cleanup (deploy target retired well before Coolify; column
		// kept its old name through Coolify era and now matches the renamed
		// CanDeployWeb Go field).
		`DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='standard_user_limits' AND column_name='can_deploy_to_vercel')
  AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='standard_user_limits' AND column_name='can_deploy_web') THEN
    ALTER TABLE standard_user_limits RENAME COLUMN can_deploy_to_vercel TO can_deploy_web;
  END IF;
END $$`,
		`ALTER TABLE IF EXISTS standard_user_limits DROP COLUMN IF EXISTS can_deploy_to_vercel`,
		`DROP TABLE IF EXISTS vercel_projects`,
	}
	for _, stmt := range renameStatements {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			slog.Warn("rename migration skipped", "statement", stmt, "error", err)
		}
	}

	// deploymentEventIndexes runs unconditionally on every boot (outside the
	// canary-gated alterStatements). CREATE INDEX IF NOT EXISTS is idempotent
	// and takes no exclusive lock on already-indexed tables.
	deploymentEventIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_deployment_events_deployment_id_occurred_at ON deployment_events (deployment_id, occurred_at)`,
	}
	for _, stmt := range deploymentEventIndexes {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			slog.Warn("deployment event index skipped", "statement", stmt, "error", err)
		}
	}

	// buildAnnotationsMigrations runs unconditionally on every boot.
	// ADD COLUMN IF NOT EXISTS and CREATE INDEX IF NOT EXISTS are both
	// idempotent, so this is safe to re-run on every startup.
	buildAnnotationsMigrations := []string{
		`ALTER TABLE app_build_tasks ADD COLUMN IF NOT EXISTS annotation_id uuid`,
		`CREATE INDEX IF NOT EXISTS idx_app_build_tasks_annotation_id ON app_build_tasks (annotation_id) WHERE annotation_id IS NOT NULL`,
	}
	for _, stmt := range buildAnnotationsMigrations {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			slog.Warn("build annotations migration skipped", "statement", stmt, "error", err)
		}
	}

	// shipMarkersMigrations creates the schema_ship_markers table used to gate
	// one-time backfills so they run exactly once per deploy cluster.
	// CREATE TABLE IF NOT EXISTS is idempotent — safe to run on every boot.
	shipMarkersMigrations := []string{
		`CREATE TABLE IF NOT EXISTS schema_ship_markers (
    name text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT current_timestamp
)`,
	}
	for _, stmt := range shipMarkersMigrations {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			slog.Warn("ship markers migration skipped", "statement", stmt, "error", err)
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
		`CREATE INDEX IF NOT EXISTS idx_app_dashboards_app_project_id ON app_dashboards(app_project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_deployments_project_id ON deployments(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_credits_balances_user_id ON ai_credits_balances(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_credit_transactions_user_id ON ai_credit_transactions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_credit_transactions_user_created ON ai_credit_transactions(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_organization_members_user_id ON organization_members(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_organization_members_organization_id ON organization_members(organization_id)`,
		`CREATE INDEX IF NOT EXISTS idx_runtime_apps_app_project_id ON runtime_apps(app_project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_user_id ON webhook_endpoints(user_id)`,
		// Agent rewrite: chat messages + agent runs queried by project, runs also by status.
		// Kept in perfIndexes (not indexStatements) so already-migrated prod DBs pick
		// them up on next restart — indexStatements is gated behind migrationsDone.
		`CREATE INDEX IF NOT EXISTS idx_app_chat_messages_project_created ON app_chat_messages (app_project_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_app_agent_runs_project_created ON app_agent_runs (app_project_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_app_agent_runs_status ON app_agent_runs (status)`,
		// Partial unique index enforces a single active run per project at the DB
		// level, catching concurrent InsertAgentRun races the app-level pre-check
		// can't cover.
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_app_agent_runs_running ON app_agent_runs (app_project_id) WHERE status = 'running'`,
		// build_events dedup index — enforces (task_id, message_hash) uniqueness
		`CREATE UNIQUE INDEX IF NOT EXISTS build_events_task_msg_uniq ON build_events (task_id, message_hash)`,
		// connection_validations: LatestConnectionValidation ORDER BY validated_at DESC
		`CREATE INDEX IF NOT EXISTS connection_validations_kind_id_at ON connection_validations (connection_kind, connection_id, validated_at DESC)`,
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
			Name:         "Free",
			Description:  "Free plan with limited features",
			MonthlyPrice: 0,
			PriceID:      "free_plan",
			Limits: models.StandardUserLimits{
				MaximumProjectCount:     1,
				MaximumCredentialCount:  0,
				MontlyBuildCount:        1,
				MontlyNotificationCount: 3,
				MonthlyAICredits:        50,
				MaxWebApps:              1,
				MaxMobileApps:           0,
				MaxDeployedWebApps:      1,
				MaxDeployedMobileApps:   0,
				CanDeployMobile:         false,
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
			Limits: models.StandardUserLimits{
				MaximumProjectCount:     20,
				MaximumCredentialCount:  2,
				MontlyBuildCount:        10,
				MontlyNotificationCount: 50,
				MonthlyAICredits:        100,
				MaxWebApps:              20,
				MaxMobileApps:           0,
				MaxDeployedWebApps:      3,
				MaxDeployedMobileApps:   1,
				CanDeployMobile:         true,
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
			Limits: models.StandardUserLimits{
				MaximumProjectCount:     50,
				MaximumCredentialCount:  10,
				MontlyBuildCount:        50,
				MontlyNotificationCount: 200,
				MonthlyAICredits:        200,
				MaxWebApps:              100,
				MaxMobileApps:           5,
				MaxDeployedWebApps:      10,
				MaxDeployedMobileApps:   3,
				CanDeployMobile:         true,
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

		limits.MaximumProjectCount = p.Limits.MaximumProjectCount
		limits.MonthlyAICredits = p.Limits.MonthlyAICredits
		limits.MaxWebApps = p.Limits.MaxWebApps
		limits.MaxMobileApps = p.Limits.MaxMobileApps
		limits.MaxDeployedWebApps = p.Limits.MaxDeployedWebApps
		limits.MaxDeployedMobileApps = p.Limits.MaxDeployedMobileApps
		limits.CanDeployMobile = p.Limits.CanDeployMobile
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

func migrateInactiveUsersToFree(bundb *bun.DB) {
	// Assign free plan to inactive users
	res, err := bundb.ExecContext(context.Background(), `
		UPDATE users SET subscription_status = 'free', subscription_id = (
			SELECT id FROM standard_user_subscriptions WHERE name = 'Free' LIMIT 1
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

// backfillEncryptIntegrations rewrites every AppIntegration row whose
// credentials_encrypted_v1 marker is still false: it walks Credentials JSON
// + EnvVars and wraps each leaf string with the enc:v1: prefix using the
// integration encryption key. Idempotent — the prefix check inside the
// utils.Encrypt* helpers means running this twice is a no-op.
//
// Deploy ordering note: this runs in-process, AFTER createSchema and BEFORE
// the HTTP server starts. Old code in a rolling deploy will keep reading
// plaintext rows until they're flipped, and the new code reads both — so
// no readers see encrypted rows before they have the decrypt path.
func backfillEncryptIntegrations(bundb *bun.DB, sm secret.SecretManager) {
	hexKey := sm.GetSecret("INTEGRATION_CREDENTIALS_ENCRYPTION_KEY")
	if hexKey == "" {
		hexKey = sm.GetSecret("GITHUB_TOKEN_ENCRYPTION_KEY")
	}
	if hexKey == "" {
		slog.Warn("backfillEncryptIntegrations: no encryption key available, skipping")
		return
	}

	ctx := context.Background()
	const batchSize = 100
	total := 0
	for {
		var rows []models.AppIntegration
		if err := bundb.NewSelect().
			Model(&rows).
			Where("credentials_encrypted_v1 = false").
			Where("deleted_at IS NULL").
			Limit(batchSize).
			Scan(ctx); err != nil {
			slog.Error("backfillEncryptIntegrations: scan failed", "error", err)
			return
		}
		if len(rows) == 0 {
			break
		}
		for i := range rows {
			r := &rows[i]
			var creds map[string]any
			if len(r.Credentials) > 0 {
				if err := json.Unmarshal(r.Credentials, &creds); err != nil {
					slog.Warn("backfillEncryptIntegrations: bad credentials JSON, marking row done",
						"id", r.ID, "error", err)
				} else {
					encCreds := utils.EncryptIntegrationMapAny(creds, hexKey)
					if b, err := json.Marshal(encCreds); err == nil {
						r.Credentials = b
					}
				}
			}
			r.EnvVars = utils.EncryptIntegrationMapStr(r.EnvVars, hexKey)
			r.CredentialsEncryptedV1 = true
			if _, err := bundb.NewUpdate().
				Model(r).
				Column("credentials", "env_vars", "credentials_encrypted_v1", "updated_at").
				Where("id = ?", r.ID).
				Exec(ctx); err != nil {
				slog.Warn("backfillEncryptIntegrations: update failed",
					"id", r.ID, "error", err)
				continue
			}
			total++
		}
		if len(rows) < batchSize {
			break
		}
	}
	if total > 0 {
		slog.Info("backfillEncryptIntegrations: encrypted rows", "count", total)
	}
}

// backfillEncryptCredentials sweeps every Apple + Google Play connection
// row whose CredRef columns are NULL: reads the legacy S3 plaintext,
// encrypts via CredStore, persists the ref. Idempotent — rows with a
// non-NULL ref are skipped. Legacy plaintext is left in place for the
// dual-read transition.
//
// Deploy ordering: runs in-process AFTER createSchema and BEFORE the HTTP
// server starts. Old code paths that haven't been switched to dual-read keep
// reading legacy plaintext; new dual-read paths prefer CredRef when present.
func backfillEncryptCredentials(bundb *bun.DB, credStore *storage.CredStore, userKeys *storage.S3Storage) {
	ctx := context.Background()
	const batchSize = 100

	// Apple: p8 + apns p8 in one pass per row. The WHERE gates on a non-empty
	// path being present so rows with no encryptable content are excluded —
	// otherwise the loop would re-fetch them forever (a row with p8 done but
	// apnsp8_key_path == "" would still match the NULL-ref check).
	//
	// Note on column name: APNSP8KeyPath has no explicit bun tag, so Bun's
	// snake_case keeps the all-caps run together → `apnsp8_key_path` (no
	// underscore between "apns" and "p8"). The new CredRef columns DO have
	// explicit tags so they use `apns_p8_key_cred_ref`.
	//
	// Pagination is cursor-based on `id`: each batch fetches rows with id > the
	// last seen id. This guarantees forward progress even if every row in a
	// batch fails to encrypt (e.g. orphaned S3 object, IAM gap) — the failed
	// rows would otherwise reappear in the next SELECT and wedge the loop.
	// Partial successes are still preserved (a row with one ref encrypted
	// keeps that ref persisted; subsequent runs pick up the missing leaf).
	totalApple := 0
	var appleCursor uuid.UUID
	for {
		var rows []models.AppleConnection
		if err := bundb.NewSelect().
			Model(&rows).
			Where("(p8_key_cred_ref IS NULL AND p8_key_path != '') OR (apns_p8_key_cred_ref IS NULL AND apnsp8_key_path != '')").
			Where("deleted_at IS NULL").
			Where("id > ?", appleCursor).
			Order("id ASC").
			Limit(batchSize).
			Scan(ctx); err != nil {
			slog.Error("backfillEncryptCredentials apple: scan failed", "err", err)
			break
		}
		if len(rows) == 0 {
			break
		}
		appleCursor = rows[len(rows)-1].ID
		for i := range rows {
			r := &rows[i]
			changed := false
			if r.P8KeyCredRef == nil && r.P8KeyPath != "" {
				if pt, err := userKeys.ReadObjectBytes(ctx, r.P8KeyPath); err == nil {
					if ref, err := credStore.Put(ctx, r.OrganizationID, "apple-p8", pt); err == nil {
						r.P8KeyCredRef = &models.CredRefJSON{
							OrgID:        ref.OrgID,
							Kind:         ref.Kind,
							BlobS3Key:    ref.BlobS3Key,
							DEKBlobS3Key: ref.DEKBlobS3Key,
						}
						changed = true
					} else {
						slog.Warn("backfill apple p8 put", "id", r.ID, "err", err)
					}
				} else {
					slog.Warn("backfill apple p8 read", "id", r.ID, "path", r.P8KeyPath, "err", err)
				}
			}
			if r.APNSP8KeyCredRef == nil && r.APNSP8KeyPath != "" {
				if pt, err := userKeys.ReadObjectBytes(ctx, r.APNSP8KeyPath); err == nil {
					if ref, err := credStore.Put(ctx, r.OrganizationID, "apple-apns", pt); err == nil {
						r.APNSP8KeyCredRef = &models.CredRefJSON{
							OrgID:        ref.OrgID,
							Kind:         ref.Kind,
							BlobS3Key:    ref.BlobS3Key,
							DEKBlobS3Key: ref.DEKBlobS3Key,
						}
						changed = true
					} else {
						slog.Warn("backfill apple apns put", "id", r.ID, "err", err)
					}
				} else {
					slog.Warn("backfill apple apns read", "id", r.ID, "path", r.APNSP8KeyPath, "err", err)
				}
			}
			if changed {
				if _, err := bundb.NewUpdate().
					Model(r).
					Column("p8_key_cred_ref", "apns_p8_key_cred_ref", "updated_at").
					Where("id = ?", r.ID).
					Exec(ctx); err != nil {
					slog.Warn("backfill apple update", "id", r.ID, "err", err)
					continue
				}
				totalApple++
			}
		}
		if len(rows) < batchSize {
			break
		}
	}
	if totalApple > 0 {
		slog.Info("backfillEncryptCredentials apple: encrypted rows", "count", totalApple)
	}

	// Google Play. Single ref column, so the gated WHERE is straightforward.
	// Same cursor-based pagination as the Apple loop above.
	totalGoogle := 0
	var googleCursor uuid.UUID
	for {
		var rows []models.GooglePlayConnection
		if err := bundb.NewSelect().
			Model(&rows).
			Where("service_account_cred_ref IS NULL AND service_account_path != ''").
			Where("deleted_at IS NULL").
			Where("id > ?", googleCursor).
			Order("id ASC").
			Limit(batchSize).
			Scan(ctx); err != nil {
			slog.Error("backfillEncryptCredentials google: scan failed", "err", err)
			break
		}
		if len(rows) == 0 {
			break
		}
		googleCursor = rows[len(rows)-1].ID
		for i := range rows {
			r := &rows[i]
			pt, err := userKeys.ReadObjectBytes(ctx, r.ServiceAccountPath)
			if err != nil {
				slog.Warn("backfill google sa read", "id", r.ID, "path", r.ServiceAccountPath, "err", err)
				continue
			}
			ref, err := credStore.Put(ctx, r.OrganizationID, "google-sa", pt)
			if err != nil {
				slog.Warn("backfill google sa put", "id", r.ID, "err", err)
				continue
			}
			r.ServiceAccountCredRef = &models.CredRefJSON{
				OrgID:        ref.OrgID,
				Kind:         ref.Kind,
				BlobS3Key:    ref.BlobS3Key,
				DEKBlobS3Key: ref.DEKBlobS3Key,
			}
			if _, err := bundb.NewUpdate().
				Model(r).
				Column("service_account_cred_ref", "updated_at").
				Where("id = ?", r.ID).
				Exec(ctx); err != nil {
				slog.Warn("backfill google update", "id", r.ID, "err", err)
				continue
			}
			totalGoogle++
		}
		if len(rows) < batchSize {
			break
		}
	}
	if totalGoogle > 0 {
		slog.Info("backfillEncryptCredentials google: encrypted rows", "count", totalGoogle)
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

	// Run the build_defaults backfill exactly once per deploy cluster.
	// schema_ship_markers records the fact so restarts don't re-scan every
	// app_project.
	var markerExists bool
	if err := bundb.NewSelect().
		ColumnExpr("EXISTS (SELECT 1 FROM schema_ship_markers WHERE name = ?)", "build_defaults_backfill_v1").
		Scan(context.Background(), &markerExists); err != nil {
		slog.Error("check build_defaults marker failed", "err", err)
	} else if !markerExists {
		if err := database.BackfillBuildDefaults(context.Background(), bundb); err != nil {
			slog.Error("backfill build_defaults failed", "err", err)
		} else {
			if _, err := bundb.NewRaw(
				"INSERT INTO schema_ship_markers (name) VALUES (?)",
				"build_defaults_backfill_v1",
			).Exec(context.Background()); err != nil {
				slog.Warn("mark build_defaults_backfill_v1 failed", "err", err)
			}
		}
	}

	sm := controller.SecretManager

	// One-shot encryption-at-rest backfill for AppIntegration credentials and
	// env vars. Runs in-process so the new decrypt code is already live when
	// the rewrite happens — no rolling-deploy ordering trap.
	backfillEncryptIntegrations(bundb, sm)

	// One-shot CredStore backfill for Apple + Google Play connection rows
	// whose CredRef columns are NULL. Idempotent — re-running is a no-op.
	backfillEncryptCredentials(bundb, controller.CredStore, controller.UserApiKeysStorage)

	seedPlans(db, sm)
	migrateInactiveUsersToFree(bundb)

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
		AllowedOrigins:   []string{"https://app.wemob.io", "https://dashboard.wemob.io", "https://wemob.io", "https://www.wemob.io", "http://localhost:3000", "http://127.0.0.1:3000", "https://*.wemob.app", "https://device1.zebu-pain.ts.net", "https://mba.zebu-pain.ts.net"},
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
					r.Put("/language", httpViews.UpdateUserLanguage)
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
			r.Route("/deployments", func(r chi.Router) {
				r.Get("/", httpViews.GetDeployments)
				r.Get("/{deploymentId}", httpViews.GetDeployment)
				r.Put("/{deploymentId}", httpViews.UpdateDeployment)
				r.Get("/{deploymentId}/events", httpViews.GetDeploymentEvents)
				r.Post("/{deploymentId}/switch-to-managed", httpViews.SwitchDeploymentToManaged)
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
			// Flat, cross-project build lookups so the UI can address a
			// build by its id without knowing the project it belongs to.
			r.Route("/builds/{buildId}", func(r chi.Router) {
				r.Get("/sign-status", httpViews.GetBuildSignStatus)
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

			// Template copy (requires auth + subscription)
			r.With(appMiddleware.RequireAuth(controller), appMiddleware.RequireSubscription(controller, "active", "trial", "free")).
				Post("/templates/{slug}/copy", httpViews.CopyTemplate)

			r.Route("/apps", func(r chi.Router) {
				r.Use(appMiddleware.RequireAuth(controller))
				r.Use(appMiddleware.RequireSubscription(controller, "active", "trial", "free"))

				// Generation endpoints (AI rate limited)
				r.With(rateLimiter.RateLimit(20, time.Minute, appMiddleware.AIUserKey)).Post("/generate/stream", httpViews.GenerateAppStream)

				r.Get("/jobs/{jobId}/stream", httpViews.StreamJob)

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
						r.With(rateLimiter.RateLimit(3, time.Minute, appMiddleware.UserKey)).
							Post("/web", httpViews.DeployAppWeb)
						r.Delete("/web", httpViews.UndeployAppWeb)
						r.With(rateLimiter.RateLimit(3, time.Minute, appMiddleware.UserKey)).
							Post("/mobile", httpViews.DeployAppMobile)
						r.Delete("/mobile", httpViews.UndeployAppMobile)
						r.Get("/status", httpViews.GetDeployStatus)
						r.Get("/history", httpViews.GetDeployHistory)
					})

					// P1 #12 mobile audit: JS-only OTA quick publish via Codemagic.
					r.With(rateLimiter.RateLimit(5, time.Minute, appMiddleware.UserKey)).
						Post("/ota/quick-publish", httpViews.QuickPublishOTA)

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
						r.Delete("/", httpViews.RemoveGitHubExport)
					})

					// Mobile builds
					r.Route("/build", func(r chi.Router) {
						// Combined endpoint: accepts {platforms:[...]} and enqueues
						// both Android and iOS in one call so the UI can ship a
						// "check both" build dialog without sequential collisions
						// with the pending-task guard.
						r.Post("/", httpViews.BuildAppMobileMulti)
						r.Post("/android", httpViews.BuildAppAndroid)
						r.Post("/ios", httpViews.BuildAppIOS)
						// Expo (mobile_app_native) — separate endpoint because the
						// request shape + permission gate route through BuildExpoMobile.
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
			expoReportsHandler := views.NewExpoReportsHandler(controller.DB, workerHMACKey)
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

			// Public integration providers
			r.Get("/integrations/providers", httpViews.GetV2IntegrationProviders)

			// GitHub OAuth
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
			r.Use(appMiddleware.InternalOnly(edgeToken))
			r.Get("/upstream", httpViews.HandleEdgeUpstream)
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

	// Phase 5c-i: poll App Store Connect + Google Play for state changes
	// on in-flight deployments, writing store_* events.
	pollerCtx, cancelPoller := context.WithCancel(context.Background())
	poller := &storepoll.Poller{
		DB:        controller.DB,
		Reader:    controller,
		PollEvery: 10 * time.Minute,
	}
	go poller.Run(pollerCtx)

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

	// Phase 5c-iii: periodically email users with pending_connection
	// deployments that they should wire up a connection (or switch to
	// managed). Max 3 nudges, 2 days apart.
	nudgeCtx, cancelNudge := context.WithCancel(context.Background())
	nudgeRunner := &pendingnudge.Runner{
		DB:           controller.DB,
		EventService: controller.EventService,
		Every:        6 * time.Hour,
	}
	go nudgeRunner.Run(nudgeCtx)

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
		if controller.PostHog != nil {
			controller.PostHog.Close()
		}

		// Stop the store-API poller.
		cancelPoller()
		cancelNudge()
		cancelReleasePoll()

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
