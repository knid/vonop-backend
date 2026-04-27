package constants

// Error codes returned by the API. These are stable, machine-readable
// identifiers used by the frontend to look up a localized message.
// Codes are UPPER_SNAKE_CASE and never renamed once shipped — to retire
// a code, add a new one and keep the old one as an alias on the frontend
// for at least one release.
//
// Each code may carry typed `details` documented next to its constant.
const (
	// Auth
	ErrAuthRequired             = "AUTH_REQUIRED"
	ErrAuthInvalidToken         = "AUTH_INVALID_TOKEN"
	ErrAuthInvalidCredentials   = "AUTH_INVALID_CREDENTIALS"
	ErrAuthEmailTaken           = "AUTH_EMAIL_TAKEN"
	ErrAuthVerificationRequired = "AUTH_VERIFICATION_REQUIRED"
	ErrAuthVerificationInvalid  = "AUTH_VERIFICATION_INVALID"
	ErrAuthResetTokenInvalid    = "AUTH_RESET_TOKEN_INVALID"
	ErrAuthResetTokenExpired    = "AUTH_RESET_TOKEN_EXPIRED"
	ErrAuthOAuthFailed          = "AUTH_OAUTH_FAILED"
	ErrAuthOAuthStateInvalid    = "AUTH_OAUTH_STATE_INVALID"
	ErrAdminRequired            = "ADMIN_REQUIRED"
	ErrInsufficientScope        = "INSUFFICIENT_SCOPE"
	ErrForbidden                = "FORBIDDEN"

	// Validation. Details: { field, value, rule, param? } (or { errors: [...] } for multi-field)
	ErrValidationRequired      = "VALIDATION_REQUIRED"
	ErrValidationInvalidEmail  = "VALIDATION_INVALID_EMAIL"
	ErrValidationInvalidURL    = "VALIDATION_INVALID_URL"
	ErrValidationInvalidFormat = "VALIDATION_INVALID_FORMAT"
	ErrValidationTooShort      = "VALIDATION_TOO_SHORT"
	ErrValidationTooLong       = "VALIDATION_TOO_LONG"
	ErrValidationOutOfRange    = "VALIDATION_OUT_OF_RANGE"
	ErrValidationBody          = "VALIDATION_BODY"

	// App
	ErrAppNotFound         = "APP_NOT_FOUND"
	ErrAppForbidden        = "APP_FORBIDDEN"
	ErrAppNameTaken        = "APP_NAME_TAKEN"
	ErrAppLimitReached     = "APP_LIMIT_REACHED" // details: { limit, used }
	ErrProjectNotFound     = "PROJECT_NOT_FOUND"
	ErrProjectLimitReached = "PROJECT_LIMIT_REACHED" // details: { limit, used, plan_name }

	// Build
	ErrBuildNotFound     = "BUILD_NOT_FOUND"
	ErrBuildLimitReached = "BUILD_LIMIT_REACHED" // details: { limit, used }
	ErrBuildInProgress   = "BUILD_IN_PROGRESS"
	ErrBuildFailed       = "BUILD_FAILED"

	// Deploy
	ErrDeployNotFound     = "DEPLOY_NOT_FOUND"
	ErrDeployLimitReached = "DEPLOY_LIMIT_REACHED" // details: { limit, used }
	ErrDeployInProgress   = "DEPLOY_IN_PROGRESS"
	ErrDeployFailed       = "DEPLOY_FAILED"

	// Domain
	ErrDomainTaken     = "DOMAIN_TAKEN"
	ErrDomainInvalid   = "DOMAIN_INVALID"
	ErrDomainDNSFailed = "DOMAIN_DNS_FAILED"
	ErrDomainNotFound  = "DOMAIN_NOT_FOUND"

	// Asset / upload
	ErrAssetNotFound     = "ASSET_NOT_FOUND"
	ErrAssetTooLarge     = "ASSET_TOO_LARGE" // details: { limit_bytes, size_bytes }
	ErrAssetInvalidType  = "ASSET_INVALID_TYPE"
	ErrAssetUploadFailed = "ASSET_UPLOAD_FAILED"

	// Store / ecommerce
	ErrStoreListingNotFound = "STORE_LISTING_NOT_FOUND"
	ErrStoreProductInvalid  = "STORE_PRODUCT_INVALID"
	ErrStoreOrderNotFound   = "STORE_ORDER_NOT_FOUND"

	// Database (per-tenant Neon)
	ErrDatabaseNotFound      = "DATABASE_NOT_FOUND"
	ErrDatabaseQueryFailed   = "DATABASE_QUERY_FAILED"
	ErrDatabaseTableNotFound = "DATABASE_TABLE_NOT_FOUND"

	// GitHub
	ErrGitHubAuthExpired        = "GITHUB_AUTH_EXPIRED"
	ErrGitHubRepoExists         = "GITHUB_REPO_EXISTS"
	ErrGitHubRepoNotFound       = "GITHUB_REPO_NOT_FOUND"
	ErrGitHubPushRejected       = "GITHUB_PUSH_REJECTED"
	ErrGitHubPushTimeout        = "GITHUB_PUSH_TIMEOUT"
	ErrGitHubExportNotAvailable = "GITHUB_EXPORT_NOT_AVAILABLE"

	// Billing / paywall
	ErrSubscriptionRequired  = "SUBSCRIPTION_REQUIRED"
	ErrAICreditsDepleted     = "AI_CREDITS_DEPLETED" // details: { remaining, required }
	ErrQuotaReached          = "QUOTA_REACHED"
	ErrFeatureNotAvailable   = "FEATURE_NOT_AVAILABLE"
	ErrBillingCheckoutFailed = "BILLING_CHECKOUT_FAILED"
	ErrBillingPortalFailed   = "BILLING_PORTAL_FAILED"

	// Notifications
	ErrNotificationLimitReached = "NOTIFICATION_LIMIT_REACHED"

	// Generic / cross-cutting
	ErrRateLimited          = "RATE_LIMITED" // details: { retry_after_seconds }
	ErrNotFound             = "NOT_FOUND"
	ErrConflict             = "CONFLICT"
	ErrGenerationInProgress = "GENERATION_IN_PROGRESS"
	ErrStreamingUnsupported = "STREAMING_UNSUPPORTED"

	// Internal — frontend always shows generic localized message
	ErrInternal         = "INTERNAL_ERROR"
	ErrInternalStorage  = "INTERNAL_STORAGE_UNAVAILABLE"
	ErrInternalDB       = "INTERNAL_DB_FAILURE"
	ErrInternalUpstream = "INTERNAL_UPSTREAM_FAILURE"
)

// DefaultMessages provides the English fallback string for each code.
// Used by JSONErrorCode when no per-call message is supplied.
var DefaultMessages = map[string]string{
	ErrAuthRequired:             "Authentication required.",
	ErrAuthInvalidToken:         "Invalid or expired token.",
	ErrAuthInvalidCredentials:   "Invalid email or password.",
	ErrAuthEmailTaken:           "An account with this email already exists.",
	ErrAuthVerificationRequired: "Please verify your email before continuing.",
	ErrAuthVerificationInvalid:  "Verification link is invalid or expired.",
	ErrAuthResetTokenInvalid:    "Password reset link is invalid.",
	ErrAuthResetTokenExpired:    "Password reset link has expired.",
	ErrAuthOAuthFailed:          "OAuth sign-in failed.",
	ErrAuthOAuthStateInvalid:    "Invalid or expired OAuth state.",
	ErrAdminRequired:            "Admin access required.",
	ErrInsufficientScope:        "API key does not have the required scope.",
	ErrForbidden:                "Access denied.",

	ErrValidationRequired:      "This field is required.",
	ErrValidationInvalidEmail:  "Please enter a valid email address.",
	ErrValidationInvalidURL:    "Please enter a valid URL.",
	ErrValidationInvalidFormat: "Invalid format.",
	ErrValidationTooShort:      "Value is too short.",
	ErrValidationTooLong:       "Value is too long.",
	ErrValidationOutOfRange:    "Value is out of range.",
	ErrValidationBody:          "Could not parse request body.",

	ErrAppNotFound:         "App not found.",
	ErrAppForbidden:        "You don't have access to this app.",
	ErrAppNameTaken:        "An app with this name already exists.",
	ErrAppLimitReached:     "App limit reached on your plan.",
	ErrProjectNotFound:     "Project not found.",
	ErrProjectLimitReached: "You've reached your project limit on this plan.",

	ErrBuildNotFound:     "Build not found.",
	ErrBuildLimitReached: "Build limit reached on your plan.",
	ErrBuildInProgress:   "A build is already in progress.",
	ErrBuildFailed:       "Build failed.",

	ErrDeployNotFound:     "Deployment not found.",
	ErrDeployLimitReached: "Deploy limit reached on your plan.",
	ErrDeployInProgress:   "A deployment is already in progress.",
	ErrDeployFailed:       "Deployment failed.",

	ErrDomainTaken:     "This domain is already in use.",
	ErrDomainInvalid:   "Invalid domain.",
	ErrDomainDNSFailed: "DNS verification failed.",
	ErrDomainNotFound:  "Domain not found.",

	ErrAssetNotFound:     "Asset not found.",
	ErrAssetTooLarge:     "File too large.",
	ErrAssetInvalidType:  "Unsupported file type.",
	ErrAssetUploadFailed: "Upload failed.",

	ErrStoreListingNotFound: "Store listing not found.",
	ErrStoreProductInvalid:  "Invalid product.",
	ErrStoreOrderNotFound:   "Order not found.",

	ErrDatabaseNotFound:      "Database not found.",
	ErrDatabaseQueryFailed:   "Database query failed.",
	ErrDatabaseTableNotFound: "Table not found.",

	ErrGitHubAuthExpired:        "GitHub authentication expired. Please reconnect.",
	ErrGitHubRepoExists:         "A repository with this name already exists.",
	ErrGitHubRepoNotFound:       "GitHub repository not found.",
	ErrGitHubPushRejected:       "GitHub push was rejected.",
	ErrGitHubPushTimeout:        "GitHub push timed out.",
	ErrGitHubExportNotAvailable: "GitHub export is not available on your plan.",

	ErrSubscriptionRequired:  "An active subscription is required.",
	ErrAICreditsDepleted:     "You're out of AI credits.",
	ErrQuotaReached:          "Plan quota reached.",
	ErrFeatureNotAvailable:   "This feature is not available on your plan.",
	ErrBillingCheckoutFailed: "Could not start checkout. Please try again.",
	ErrBillingPortalFailed:   "Could not open the billing portal. Please try again.",

	ErrNotificationLimitReached: "Notification limit reached.",

	ErrRateLimited:          "Too many requests. Please try again later.",
	ErrNotFound:             "Not found.",
	ErrConflict:             "Conflict.",
	ErrGenerationInProgress: "A generation is already in progress.",
	ErrStreamingUnsupported: "Streaming is not supported on this connection.",

	ErrInternal:         "Something went wrong. Please try again.",
	ErrInternalStorage:  "Storage is temporarily unavailable.",
	ErrInternalDB:       "Database is temporarily unavailable.",
	ErrInternalUpstream: "Upstream service is temporarily unavailable.",
}
