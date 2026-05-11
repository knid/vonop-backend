package constants

// EventName is the typed alias for every event name we send to PostHog
// (analytics) or Resend (email automations). The compiler stops random string
// literals from sneaking into TrackUserEvent / SendEvent — every name must be
// a constant declared below. A CI script (scripts/check-event-literals.sh)
// catches the rare miss where a literal is cast at the call site.
type EventName string

const (
	// User lifecycle
	EVENT_NEW_USER                     EventName = "user_registered"
	EVENT_NEW_LOGIN                    EventName = "user_logged_in"
	EVENT_USER_OAUTH_LOGGED_IN         EventName = "user_oauth_logged_in"
	EVENT_USER_DELETED                 EventName = "user_deleted"
	EVENT_USER_SETUP_FINISHED          EventName = "user_setup_finished"
	EVENT_EMAIL_VERIFIED               EventName = "email_verified"
	EVENT_PASSWORD_RESET               EventName = "password_reset"
	EVENT_PASSWORD_RESET_REQUESTED     EventName = "password_reset_requested"
	EVENT_EMAIL_VERIFICATION_REQUESTED EventName = "email_verification_requested"
	EVENT_KEYSTORE_DOWNLOAD_OTP        EventName = "keystore_download_otp"

	// Support
	EVENT_NEW_TICKET EventName = "support_ticket_created"

	// Project / app
	EVENT_PROJECT_DELETED          EventName = "project_deleted"
	EVENT_APP_DRAFT_CREATED        EventName = "app_draft_created"
	EVENT_APP_DELETED              EventName = "app_deleted"
	EVENT_APP_UPDATED              EventName = "app_updated"
	EVENT_APP_GENERATION_STARTED   EventName = "app_generation_started"
	EVENT_APP_EXPORTED_ZIP         EventName = "app_exported"
	EVENT_APP_FILE_CREATED         EventName = "app_file_created"
	EVENT_APP_FILE_UPDATED         EventName = "app_file_updated"
	EVENT_APP_FILE_DELETED         EventName = "app_file_deleted"
	EVENT_SNAPSHOT_CREATED         EventName = "app_snapshot_created"
	EVENT_SNAPSHOT_RESTORED        EventName = "app_snapshot_restored"
	EVENT_INTEGRATION_CONNECTED    EventName = "integration_connected"
	EVENT_INTEGRATION_DISCONNECTED EventName = "integration_disconnected"
	EVENT_QUOTA_REACHED            EventName = "quota_reached"

	// Chat / generation completion (fired from agent). The initial generation
	// and follow-up chat turns are intentionally split into two events each
	// (started/completed/failed) so the activation funnel and the per-turn
	// reliability dashboard can measure them independently — conflating them
	// makes "first-app success rate" indistinguishable from steady-state chat
	// reliability.
	EVENT_APP_GENERATION_COMPLETED EventName = "generate_app_completed"
	EVENT_APP_GENERATION_FAILED    EventName = "generate_app_failed"
	EVENT_APP_CHAT_SENT            EventName = "app_chat_sent"
	EVENT_APP_CHAT_COMPLETED       EventName = "app_chat_completed"
	EVENT_APP_CHAT_FAILED          EventName = "app_chat_failed"

	// Web deployment
	EVENT_APP_DEPLOY_ENQUEUED      EventName = "app_deploy_enqueued"
	EVENT_WEB_DEPLOYMENT_SUCCEEDED EventName = "app_deployed_web"
	EVENT_WEB_DEPLOYMENT_FAILED    EventName = "app_deploy_failed"
	EVENT_WEB_UNDEPLOYED           EventName = "app_undeployed_web"

	// Mobile deployment / OTA / release
	EVENT_MOBILE_UNDEPLOYED     EventName = "app_undeployed_mobile"
	EVENT_APP_DEPLOYED_EXPO_OTA EventName = "app_deployed_expo_ota"
	EVENT_OTA_PROMOTED          EventName = "app_ota_promoted"
	EVENT_OTA_ROLLED_BACK       EventName = "app_ota_rolled_back"
	EVENT_RELEASE_LIFECYCLE     EventName = "release_lifecycle"
	EVENT_RELEASE_CREATE_FAILED EventName = "release_create_failed"

	// Dynamic-suffix prefixes — call site appends the platform (e.g. "ios").
	// These are constants so the prefix lives in one place; the assembled
	// event name is still passed through TrackUserEvent / SendEvent typed.
	EVENT_PREFIX_APP_BUILT_EXPO EventName = "app_built_expo_"
	EVENT_PREFIX_APP_SUBMITTED  EventName = "app_submitted_"

	// Domain
	EVENT_DOMAIN_ADDED    EventName = "domain_added"
	EVENT_DOMAIN_VERIFIED EventName = "domain_verified"
	EVENT_DOMAIN_REMOVED  EventName = "domain_removed"

	// GitHub export
	EVENT_GITHUB_EXPORTED EventName = "github_exported"
	EVENT_GITHUB_PUSHED   EventName = "github_pushed"

	// Subscription & billing
	EVENT_SUBSCRIPTION_CREATED            EventName = "subscription_created"
	EVENT_SUBSCRIPTION_UPDATED            EventName = "subscription_updated"
	EVENT_SUBSCRIPTION_RENEWED            EventName = "subscription_renewed"
	EVENT_SUBSCRIPTION_UPGRADED           EventName = "subscription_upgraded"
	EVENT_SUBSCRIPTION_DOWNGRADED         EventName = "subscription_downgraded"
	EVENT_SUBSCRIPTION_CANCELED           EventName = "subscription_canceled" // US spelling — matches Stripe + PostHog
	EVENT_SUBSCRIPTION_CANCELED_VOLUNTARY EventName = "subscription_canceled_voluntary"
	EVENT_SUBSCRIPTION_CANCELED_DUNNING   EventName = "subscription_canceled_dunning"
	EVENT_SUBSCRIPTION_QUOTA_REACHED      EventName = "subscription_quota_reached"
	EVENT_SUBSCRIPTION_NEED               EventName = "subscription_need"

	EVENT_PAYMENT_METHOD_ATTACHED      EventName = "payment_method_attached"
	EVENT_PAYMENT_METHOD_DETACHED      EventName = "payment_method_detached"
	EVENT_PAYMENT_SUCCEEDED            EventName = "payment_succeeded"
	EVENT_PAYMENT_FAILED               EventName = "payment_failed"
	EVENT_PAYMENT_USER_ACTION_REQUIRED EventName = "payment_action_required"
	EVENT_INVOICE_UPCOMING             EventName = "invoice_upcoming"
	EVENT_CHECKOUT_STARTED             EventName = "checkout_started"
	EVENT_CREDIT_PURCHASE_STARTED      EventName = "credit_purchase_started"
	EVENT_UNHANDLED_EVENT              EventName = "unhandled_event"

	// AI Credits
	EVENT_AI_CREDITS_LOW                        EventName = "ai_credits_low"
	EVENT_AI_CREDITS_DEPLETED                   EventName = "ai_credits_depleted"
	EVENT_AI_CREDITS_DEDUCTED                   EventName = "ai_credits_deducted"
	EVENT_CREDITS_PURCHASED                     EventName = "credits_purchased"
	EVENT_CREDITS_GRANTED                       EventName = "credits_granted"
	EVENT_CREDITS_REFUNDED                      EventName = "credits_refunded"
	EVENT_CREDITS_THRESHOLD_CROSSED             EventName = "credits_threshold_crossed"
	EVENT_CREDITS_OVERDRAFT_STARTED             EventName = "credits_overdraft_started"
	EVENT_CREDITS_GATEWAY_BLOCKED               EventName = "credits_gateway_blocked"
	EVENT_REFERRAL_SIGNUP_BONUS_RECEIVED        EventName = "referral_signup_bonus_received"
	EVENT_REFERRAL_SUBSCRIPTION_REWARD_RECEIVED EventName = "referral_subscription_reward_received"
	EVENT_REFERRAL_CODE_APPLIED                 EventName = "referral_code_applied"

	// Product surface
	EVENT_ORGANIZATION_CREATED        EventName = "organization_created"
	EVENT_ORGANIZATION_MEMBER_INVITED EventName = "organization_member_invited"
	EVENT_ORGANIZATION_INVITE_EMAIL   EventName = "organization_invite_email"
	EVENT_DASHBOARD_CREATED           EventName = "dashboard_created"

	// Asynq job lifecycle (fired from middleware/asynq_posthog.go)
	EVENT_JOB_STARTED   EventName = "job_started"
	EVENT_JOB_SUCCEEDED EventName = "job_succeeded"
	EVENT_JOB_FAILED    EventName = "job_failed"

	// PostHog standard LLM Analytics events. Reserved names; PostHog auto-
	// derives traces / cost / latency dashboards from them. Kept as constants
	// so the CI grep doesn't flag the call sites.
	EVENT_AI_GENERATION EventName = "$ai_generation"
	EVENT_AI_TRACE      EventName = "$ai_trace"
)
