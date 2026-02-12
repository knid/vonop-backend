package constants

// Integration provider identifiers
const (
	IntegrationSupabase = "supabase"
	IntegrationNeon     = "neon"
	IntegrationStripe   = "stripe"
	IntegrationIyzico   = "iyzico"
	IntegrationFal      = "fal"
	IntegrationUpstash  = "upstash"
	IntegrationResend   = "resend"
)

// IntegrationProviderConfig defines the configuration for each integration provider
type IntegrationProviderConfig struct {
	Provider       string            `json:"provider"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Category       string            `json:"category"` // "database", "payment", "ai", "cache", "email"
	RequiredFields []string          `json:"required_fields"`
	OptionalFields []string          `json:"optional_fields,omitempty"`
	EnvVarMapping  map[string]string `json:"env_var_mapping"` // credential key -> Vercel env var name
	Website        string            `json:"website"`
}

// IntegrationProviders is the registry of all supported integration providers
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
}
