package constants

// Integration provider identifiers
const (
	IntegrationSupabase     = "supabase"
	IntegrationNeon         = "neon"
	IntegrationStripe       = "stripe"
	IntegrationIyzico       = "iyzico"
	IntegrationFal          = "fal"
	IntegrationUpstash      = "upstash"
	IntegrationResend       = "resend"
	IntegrationFirebase     = "firebase"
	IntegrationCloudinary   = "cloudinary"
	IntegrationGoogleSheets = "google_sheets"
	IntegrationOpenRouter   = "openrouter"
)

// IntegrationProviderConfig defines the configuration for each integration provider
type IntegrationProviderConfig struct {
	Provider       string            `json:"provider"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Category       string            `json:"category"` // "database", "payment", "ai", "cache", "email", "storage", "data"
	RequiredFields []string          `json:"required_fields"`
	OptionalFields []string          `json:"optional_fields,omitempty"`
	EnvVarMapping  map[string]string `json:"env_var_mapping"` // credential key -> env var name
	Website        string            `json:"website"`
}

// IntegrationProviders is the single source of truth for all supported integration providers.
// Both the API provider list endpoint and the validation/env-var logic read from this.
var IntegrationProviders = []IntegrationProviderConfig{
	{
		Provider:       IntegrationSupabase,
		Name:           "Supabase",
		Description:    "Open-source Firebase alternative with PostgreSQL database, authentication, and realtime.",
		Category:       "database",
		RequiredFields: []string{"url", "anon_key"},
		EnvVarMapping: map[string]string{
			"url":      "NEXT_PUBLIC_SUPABASE_URL",
			"anon_key": "NEXT_PUBLIC_SUPABASE_ANON_KEY",
		},
		Website: "https://supabase.com",
	},
	{
		Provider:       IntegrationNeon,
		Name:           "Neon Database",
		Description:    "Serverless PostgreSQL with branching and auto-scaling.",
		Category:       "database",
		RequiredFields: []string{"connection_string"},
		EnvVarMapping: map[string]string{
			"connection_string": "DATABASE_URL",
		},
		Website: "https://neon.tech",
	},
	{
		Provider:       IntegrationFirebase,
		Name:           "Firebase",
		Description:    "Database, authentication, and storage by Google.",
		Category:       "database",
		RequiredFields: []string{"api_key", "auth_domain", "project_id"},
		OptionalFields: []string{"storage_bucket", "messaging_sender_id", "app_id"},
		EnvVarMapping: map[string]string{
			"api_key":              "NEXT_PUBLIC_FIREBASE_API_KEY",
			"auth_domain":         "NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN",
			"project_id":          "NEXT_PUBLIC_FIREBASE_PROJECT_ID",
			"storage_bucket":      "NEXT_PUBLIC_FIREBASE_STORAGE_BUCKET",
			"messaging_sender_id": "NEXT_PUBLIC_FIREBASE_MESSAGING_SENDER_ID",
			"app_id":              "NEXT_PUBLIC_FIREBASE_APP_ID",
		},
		Website: "https://firebase.google.com",
	},
	{
		Provider:       IntegrationStripe,
		Name:           "Stripe",
		Description:    "Payment processing platform for online businesses.",
		Category:       "payment",
		RequiredFields: []string{"publishable_key", "secret_key"},
		OptionalFields: []string{"webhook_secret"},
		EnvVarMapping: map[string]string{
			"publishable_key": "NEXT_PUBLIC_STRIPE_PK",
			"secret_key":      "STRIPE_SK",
			"webhook_secret":  "STRIPE_WEBHOOK_SECRET",
		},
		Website: "https://stripe.com",
	},
	{
		Provider:       IntegrationIyzico,
		Name:           "Iyzico",
		Description:    "Turkish payment gateway for local and international payments.",
		Category:       "payment",
		RequiredFields: []string{"api_key", "secret_key"},
		EnvVarMapping: map[string]string{
			"api_key":    "IYZICO_API_KEY",
			"secret_key": "IYZICO_SECRET_KEY",
		},
		Website: "https://www.iyzico.com",
	},
	{
		Provider:       IntegrationFal,
		Name:           "Fal.ai",
		Description:    "AI model inference platform for image generation and processing.",
		Category:       "ai",
		RequiredFields: []string{"api_key"},
		EnvVarMapping: map[string]string{
			"api_key": "FAL_KEY",
		},
		Website: "https://fal.ai",
	},
	{
		Provider:       IntegrationOpenRouter,
		Name:           "OpenRouter",
		Description:    "Unified API for 200+ AI models including GPT-4, Claude, Llama, Mistral, and more.",
		Category:       "ai",
		RequiredFields: []string{"api_key"},
		EnvVarMapping: map[string]string{
			"api_key": "OPENROUTER_API_KEY",
		},
		Website: "https://openrouter.ai",
	},
	{
		Provider:       IntegrationUpstash,
		Name:           "Upstash",
		Description:    "Serverless Redis and Kafka for caching and messaging.",
		Category:       "cache",
		RequiredFields: []string{"rest_url", "rest_token"},
		EnvVarMapping: map[string]string{
			"rest_url":   "UPSTASH_REDIS_REST_URL",
			"rest_token": "UPSTASH_REDIS_REST_TOKEN",
		},
		Website: "https://upstash.com",
	},
	{
		Provider:       IntegrationResend,
		Name:           "Resend",
		Description:    "Email API for developers with React Email support.",
		Category:       "email",
		RequiredFields: []string{"api_key"},
		EnvVarMapping: map[string]string{
			"api_key": "RESEND_API_KEY",
		},
		Website: "https://resend.com",
	},
	{
		Provider:       IntegrationCloudinary,
		Name:           "Cloudinary",
		Description:    "Image and video optimization, upload, and transformation.",
		Category:       "storage",
		RequiredFields: []string{"cloud_name", "upload_preset"},
		OptionalFields: []string{"api_key", "api_secret"},
		EnvVarMapping: map[string]string{
			"cloud_name":    "NEXT_PUBLIC_CLOUDINARY_CLOUD_NAME",
			"upload_preset": "NEXT_PUBLIC_CLOUDINARY_UPLOAD_PRESET",
			"api_key":       "CLOUDINARY_API_KEY",
			"api_secret":    "CLOUDINARY_API_SECRET",
		},
		Website: "https://cloudinary.com",
	},
	{
		Provider:       IntegrationGoogleSheets,
		Name:           "Google Sheets",
		Description:    "Use Google Sheets as a simple database for your website content.",
		Category:       "data",
		RequiredFields: []string{"api_key", "sheet_id"},
		EnvVarMapping: map[string]string{
			"api_key":  "GOOGLE_SHEETS_API_KEY",
			"sheet_id": "NEXT_PUBLIC_GOOGLE_SHEET_ID",
		},
		Website: "https://sheets.google.com",
	},
}
