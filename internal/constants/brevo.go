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

	// Subscription & billing events
	BREVO_EVENT_SUBSCRIPTION_UPDATED       = "subscription_updated"
	BREVO_EVENT_SUBSCRIPTION_CANCELLED     = "subscription_cancelled"
	BREVO_EVENT_SUBSCRIPTION_QUOTA_REACHED = "subscription_quota_reached"
	BREVO_EVENT_SUBSCRIPTION_NEED          = "subscription_need"
	BREVO_EVENT_PAYMENT_METHOD_ATTACHED    = "payment_method_attached"
	BREVO_EVENT_PAYMENT_METHOD_DETACHED    = "payment_method_detached"
	BREVO_EVENT_PAYMENT_FAILED             = "payment_failed"
	BREVO_EVENT_PAYMENT_USER_ACTION_REQUIRED = "payment_user_action_required"
	BREVO_EVENT_UNHANDLED_EVENT            = "unhandled_event"

	// Usage alert events
	BREVO_EVENT_TRIAL_ENDING_SOON   = "trial_ending_soon"
	BREVO_EVENT_AI_CREDITS_LOW      = "ai_credits_low"
	BREVO_EVENT_AI_CREDITS_DEPLETED = "ai_credits_depleted"
)
