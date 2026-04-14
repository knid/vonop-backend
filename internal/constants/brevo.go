package constants

const (
	BREVO_WELCOME_EMAIL_TEMPLATE_ID        = 2
	BREVO_TICKET_RECEIVED_EMAIL_TEMPLATE_ID = 4
	BREVO_PASSWORD_RESET_EMAIL_TEMPLATE_ID = 10
	BREVO_VERIFACTION_EMAIL_TEMPLATE_ID    = 11
	BREVO_ORDER_NOTIFICATION_TEMPLATE_ID   = 12

	// User lifecycle events
	BREVO_EVENT_NEW_USER            = "new_user"
	BREVO_EVENT_NEW_LOGIN           = "new_login"
	BREVO_EVENT_USER_DELETED        = "user_deleted"
	BREVO_EVENT_USER_SETUP_FINISHED = "user_setup_finished"
	BREVO_EVENT_EMAIL_VERIFIED      = "email_verified"
	BREVO_EVENT_PASSWORD_RESET      = "password_reset"

	// Support events
	BREVO_EVENT_NEW_TICKET = "new_ticket"

	// Project events
	BREVO_EVENT_NEW_PROJECT     = "new_project"
	BREVO_EVENT_PROJECT_UPDATED = "update_project"
	BREVO_EVENT_PROJECT_DELETED = "project_deleted"
	BREVO_EVENT_APP_DRAFT_CREATED = "app_draft_created"
	BREVO_EVENT_APP_GENERATION_STARTED = "app_generation_started"
	BREVO_EVENT_APP_EXPORTED_ZIP       = "app_exported_zip"
	BREVO_EVENT_SNAPSHOT_CREATED       = "snapshot_created"
	BREVO_EVENT_SNAPSHOT_RESTORED      = "snapshot_restored"
	BREVO_EVENT_INTEGRATION_CONNECTED    = "integration_connected"
	BREVO_EVENT_INTEGRATION_DISCONNECTED = "integration_disconnected"

	// Build events (reported by workers)
	BREVO_EVENT_BUILD_STARTED_IOS    = "ios_build_started"
	BREVO_EVENT_BUILD_STARTED_ANDROID = "android_build_started"
	BREVO_EVENT_BUILD_FINISHED_IOS   = "ios_build_finished"
	BREVO_EVENT_BUILD_FINISHED_ANDROID = "android_build_finished"
	BREVO_EVENT_BUILD_ERROR_IOS      = "ios_build_error"
	BREVO_EVENT_BUILD_ERROR_ANDROID  = "android_build_error"

	// Deployment events (reported by workers)
	BREVO_EVENT_USER_APPLE_DEPLOYMENT_STARTED    = "user_apple_deployment_started"
	BREVO_EVENT_USER_APPLE_DEPLOYMENT_FINISHED   = "user_apple_deployment_finished"
	BREVO_EVENT_USER_APPLE_DEPLOYMENT_FAILED     = "user_apple_deployment_failed"
	BREVO_EVENT_USER_ANDROID_DEPLOYMENT_STARTED  = "user_android_deployment_started"
	BREVO_EVENT_USER_ANDROID_DEPLOYMENT_FINISHED = "user_android_deployment_finished"
	BREVO_EVENT_USER_ANDROID_DEPLOYMENT_FAILED   = "user_android_deployment_failed"

	// Web deployment events
	BREVO_EVENT_WEB_DEPLOYMENT_SUCCEEDED = "web_deployment_succeeded"
	BREVO_EVENT_WEB_DEPLOYMENT_FAILED    = "web_deployment_failed"
	BREVO_EVENT_WEB_UNDEPLOYED           = "web_undeployed"

	// Domain events
	BREVO_EVENT_DOMAIN_ADDED    = "domain_added"
	BREVO_EVENT_DOMAIN_VERIFIED = "domain_verified"
	BREVO_EVENT_DOMAIN_REMOVED  = "domain_removed"

	// GitHub export events
	BREVO_EVENT_GITHUB_EXPORTED = "github_exported"
	BREVO_EVENT_GITHUB_PUSHED   = "github_pushed"

	// Subscription & billing events
	BREVO_EVENT_SUBSCRIPTION_CREATED       = "subscription_created"
	BREVO_EVENT_SUBSCRIPTION_UPDATED       = "subscription_updated"
	BREVO_EVENT_SUBSCRIPTION_RENEWED       = "subscription_renewed"
	BREVO_EVENT_SUBSCRIPTION_UPGRADED      = "subscription_upgraded"
	BREVO_EVENT_SUBSCRIPTION_DOWNGRADED    = "subscription_downgraded"
	BREVO_EVENT_SUBSCRIPTION_CANCELLED     = "subscription_cancelled"
	BREVO_EVENT_SUBSCRIPTION_QUOTA_REACHED = "subscription_quota_reached"
	BREVO_EVENT_SUBSCRIPTION_NEED          = "subscription_need"
	BREVO_EVENT_PAYMENT_METHOD_ATTACHED    = "payment_method_attached"
	BREVO_EVENT_PAYMENT_METHOD_DETACHED    = "payment_method_detached"
	BREVO_EVENT_PAYMENT_SUCCEEDED          = "payment_succeeded"
	BREVO_EVENT_PAYMENT_FAILED             = "payment_failed"
	BREVO_EVENT_PAYMENT_USER_ACTION_REQUIRED = "payment_user_action_required"
	BREVO_EVENT_INVOICE_UPCOMING           = "invoice_upcoming"
	BREVO_EVENT_CHECKOUT_STARTED           = "checkout_started"
	BREVO_EVENT_CREDIT_PURCHASE_STARTED    = "credit_purchase_started"
	BREVO_EVENT_UNHANDLED_EVENT            = "unhandled_event"

	// AI Credits events
	BREVO_EVENT_AI_CREDITS_LOW        = "ai_credits_low"
	BREVO_EVENT_AI_CREDITS_DEPLETED   = "ai_credits_depleted"
	BREVO_EVENT_CREDITS_PURCHASED     = "credits_purchased"
	BREVO_EVENT_CREDITS_GRANTED       = "credits_granted"
	BREVO_EVENT_CREDITS_REFUNDED      = "credits_refunded"
	BREVO_EVENT_CREDITS_THRESHOLD_CROSSED = "credits_threshold_crossed"
	BREVO_EVENT_CREDITS_OVERDRAFT_STARTED = "credits_overdraft_started"
	BREVO_EVENT_CREDITS_GATEWAY_BLOCKED   = "credits_gateway_blocked"
	BREVO_EVENT_REFERRAL_SIGNUP_BONUS_RECEIVED         = "referral_signup_bonus_received"
	BREVO_EVENT_REFERRAL_SUBSCRIPTION_REWARD_RECEIVED  = "referral_subscription_reward_received"
	BREVO_EVENT_REFERRAL_CODE_APPLIED                  = "referral_code_applied"

	// Product surface events
	BREVO_EVENT_ORGANIZATION_CREATED         = "organization_created"
	BREVO_EVENT_ORGANIZATION_MEMBER_INVITED  = "organization_member_invited"
	BREVO_EVENT_DASHBOARD_CREATED            = "dashboard_created"
	BREVO_EVENT_STORE_CHAT_SENT              = "store_chat_sent"
)
