package constants

const (
	// User lifecycle events — names match PostHog for cross-system analysis.
	EVENT_NEW_USER                       = "user_registered"
	EVENT_NEW_LOGIN                      = "user_logged_in"
	EVENT_USER_DELETED                   = "user_deleted"
	EVENT_USER_SETUP_FINISHED            = "user_setup_finished"
	EVENT_EMAIL_VERIFIED                 = "email_verified"
	EVENT_PASSWORD_RESET                 = "password_reset"
	EVENT_PASSWORD_RESET_REQUESTED       = "password_reset_requested"     // NEW — triggers password-reset email
	EVENT_EMAIL_VERIFICATION_REQUESTED   = "email_verification_requested" // NEW — triggers verification email
	EVENT_KEYSTORE_DOWNLOAD_OTP          = "keystore_download_otp"         // triggers OTP email for keystore download gate

	// Support events — EVENT_NEW_TICKET also triggers ticket-received email.
	EVENT_NEW_TICKET = "support_ticket_created"

	// Project events
	EVENT_NEW_PROJECT                = "project_created"
	EVENT_PROJECT_UPDATED            = "project_updated"
	EVENT_PROJECT_DELETED            = "project_deleted"
	EVENT_APP_DRAFT_CREATED          = "app_draft_created"
	EVENT_APP_GENERATION_STARTED     = "app_generation_started"
	EVENT_APP_EXPORTED_ZIP           = "app_exported"
	EVENT_SNAPSHOT_CREATED           = "app_snapshot_created"
	EVENT_SNAPSHOT_RESTORED          = "app_snapshot_restored"
	EVENT_INTEGRATION_CONNECTED      = "integration_connected"
	EVENT_INTEGRATION_DISCONNECTED   = "integration_disconnected"

	// Chat / generation completion events (fired from agent)
	EVENT_APP_GENERATION_COMPLETED = "generate_app_completed"
	EVENT_APP_CHAT_SENT            = "app_chat_sent"
	EVENT_APP_CHAT_COMPLETED       = "app_chat_completed"
	EVENT_APP_CHAT_FAILED          = "app_chat_failed"

	// Build events (reported by workers)
	EVENT_BUILD_STARTED_IOS     = "ios_build_started"
	EVENT_BUILD_STARTED_ANDROID = "android_build_started"
	EVENT_BUILD_FINISHED_IOS    = "ios_build_finished"
	EVENT_BUILD_FINISHED_ANDROID = "android_build_finished"
	EVENT_BUILD_ERROR_IOS       = "ios_build_error"
	EVENT_BUILD_ERROR_ANDROID   = "android_build_error"

	// Deployment events (reported by workers)
	EVENT_USER_APPLE_DEPLOYMENT_STARTED    = "user_apple_deployment_started"
	EVENT_USER_APPLE_DEPLOYMENT_FINISHED   = "user_apple_deployment_finished"
	EVENT_USER_APPLE_DEPLOYMENT_FAILED     = "user_apple_deployment_failed"
	EVENT_USER_ANDROID_DEPLOYMENT_STARTED  = "user_android_deployment_started"
	EVENT_USER_ANDROID_DEPLOYMENT_FINISHED = "user_android_deployment_finished"
	EVENT_USER_ANDROID_DEPLOYMENT_FAILED   = "user_android_deployment_failed"
	EVENT_PENDING_CONNECTION_NUDGE         = "pending_connection_nudge"

	// Web deployment events — names match PostHog for cross-system analysis.
	EVENT_WEB_DEPLOYMENT_SUCCEEDED    = "app_deployed_web"
	EVENT_WEB_DEPLOYMENT_FAILED       = "app_deploy_failed"
	EVENT_WEB_UNDEPLOYED              = "app_undeployed_web"
	EVENT_MOBILE_DEPLOYMENT_SUCCEEDED = "app_deployed_mobile"
	EVENT_MOBILE_DEPLOYMENT_FAILED    = "app_deploy_mobile_failed"
	EVENT_MOBILE_UNDEPLOYED           = "app_undeployed_mobile"
	EVENT_OTA_PUBLISH_SUCCEEDED       = "app_ota_published"
	EVENT_OTA_PUBLISH_FAILED          = "app_ota_publish_failed"

	// Domain events
	EVENT_DOMAIN_ADDED    = "domain_added"
	EVENT_DOMAIN_VERIFIED = "domain_verified"
	EVENT_DOMAIN_REMOVED  = "domain_removed"

	// GitHub export events
	EVENT_GITHUB_EXPORTED = "github_exported"
	EVENT_GITHUB_PUSHED   = "github_pushed"

	// Subscription & billing events
	EVENT_SUBSCRIPTION_CREATED        = "subscription_created"
	EVENT_SUBSCRIPTION_UPDATED        = "subscription_updated"
	EVENT_SUBSCRIPTION_RENEWED        = "subscription_renewed"
	EVENT_SUBSCRIPTION_UPGRADED       = "subscription_upgraded"
	EVENT_SUBSCRIPTION_DOWNGRADED     = "subscription_downgraded"
	EVENT_SUBSCRIPTION_CANCELLED            = "subscription_cancelled"
	EVENT_SUBSCRIPTION_CANCELED_VOLUNTARY   = "subscription_canceled_voluntary"
	EVENT_SUBSCRIPTION_CANCELED_DUNNING     = "subscription_canceled_dunning"
	EVENT_SUBSCRIPTION_QUOTA_REACHED  = "subscription_quota_reached"
	EVENT_SUBSCRIPTION_NEED           = "subscription_need"
	EVENT_PAYMENT_METHOD_ATTACHED     = "payment_method_attached"
	EVENT_PAYMENT_METHOD_DETACHED     = "payment_method_detached"
	EVENT_PAYMENT_SUCCEEDED           = "payment_succeeded"
	EVENT_PAYMENT_FAILED              = "payment_failed"
	EVENT_PAYMENT_USER_ACTION_REQUIRED = "payment_user_action_required"
	EVENT_INVOICE_UPCOMING            = "invoice_upcoming"
	EVENT_CHECKOUT_STARTED            = "checkout_started"
	EVENT_CREDIT_PURCHASE_STARTED     = "credit_purchase_started"
	EVENT_UNHANDLED_EVENT             = "unhandled_event"

	// AI Credits events
	EVENT_AI_CREDITS_LOW                          = "ai_credits_low"
	EVENT_AI_CREDITS_DEPLETED                     = "ai_credits_depleted"
	EVENT_CREDITS_PURCHASED                       = "credits_purchased"
	EVENT_CREDITS_GRANTED                         = "credits_granted"
	EVENT_CREDITS_REFUNDED                        = "credits_refunded"
	EVENT_CREDITS_THRESHOLD_CROSSED               = "credits_threshold_crossed"
	EVENT_CREDITS_OVERDRAFT_STARTED               = "credits_overdraft_started"
	EVENT_CREDITS_GATEWAY_BLOCKED                 = "credits_gateway_blocked"
	EVENT_REFERRAL_SIGNUP_BONUS_RECEIVED          = "referral_signup_bonus_received"
	EVENT_REFERRAL_SUBSCRIPTION_REWARD_RECEIVED   = "referral_subscription_reward_received"
	EVENT_REFERRAL_CODE_APPLIED                   = "referral_code_applied"

	// Product surface events
	EVENT_ORGANIZATION_CREATED         = "organization_created"
	EVENT_ORGANIZATION_MEMBER_INVITED  = "organization_member_invited"
	EVENT_ORGANIZATION_INVITE_EMAIL    = "organization_invite_email" // NEW — triggers Resend automation that emails the invitee
	EVENT_DASHBOARD_CREATED            = "dashboard_created"
	EVENT_STORE_CHAT_SENT              = "store_chat_sent"
)
