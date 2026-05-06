package models

import (
	"time"

	"github.com/google/uuid"
)

type AppTheme struct {
	ColorScheme     map[string]any `json:"color_scheme"`
	DarkColorScheme map[string]any `json:"dark_color_scheme,omitempty"`
	Typography      map[string]any `json:"typography,omitempty"`
	Spacing         map[string]any `json:"spacing,omitempty"`
	BorderRadius    map[string]any `json:"border_radius,omitempty"`
}

type AppProject struct {
	ID              uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID          uuid.UUID      `bun:",notnull,type:uuid"`
	User            *User          `bun:"rel:belongs-to,join:user_id=id"`
	OrganizationID  uuid.UUID      `bun:",notnull,type:uuid"`
	Organization    *Organization  `bun:"rel:belongs-to,join:organization_id=id"`
	Name            string         `bun:",notnull"`
	Slug            string         `bun:",notnull,unique"`
	Prompt          string         `bun:",notnull,type:text"`
	ProjectType     string         `bun:",notnull,default:'app'"` // one of: ProjectType{App,Ecommerce,MobileAppNative}
	// AST field removed — files now stored in git repos
	Theme           []byte         `bun:",type:jsonb,default:'{}'"` // stores AppTheme as JSON
	DataModels      []byte         `bun:",type:jsonb,default:'[]'"` // stores data model definitions
	Settings        map[string]any `bun:",type:jsonb,default:'{}'"`
	ScreenshotS3Key string         `bun:",notnull,default:''"`
	LogoS3Key       string         `bun:",notnull,default:''"`
	SandboxID       string         `bun:",notnull,default:''"`
	Status          string         `bun:",notnull,default:'draft'"` // draft, generating, ready, deployed
	TemplateID      *uuid.UUID     `bun:",type:uuid,nullzero"` // source template if created from a template
	// DefaultAppleConnectionID is the Apple connection preferred for auto-signing iOS builds
	// for this project. Nil means "none configured" — no auto-sign will happen.
	DefaultAppleConnectionID  *uuid.UUID `bun:",type:uuid,nullzero"`
	// DefaultGoogleConnectionID is the Google Play connection preferred for auto-deployment
	// for this project. Nil means "none configured".
	DefaultGoogleConnectionID *uuid.UUID `bun:",type:uuid,nullzero"`
	// AppStoreTrack is the TestFlight track for iOS submissions.
	// One of "internal" | "external" | "production". Empty = "internal" default.
	AppStoreTrack string `bun:",notnull,default:'internal'"`
	// PlayStoreTrack is the Play Console track for Android submissions.
	// One of "internal" | "alpha" | "beta" | "production". Empty = "internal" default.
	PlayStoreTrack string `bun:",notnull,default:'internal'"`
	// PhasedRelease enables Apple's phased rollout (1%/2%/5%/10%/20%/50%/100% over 7 days).
	// Only meaningful for production track; ignored for TestFlight tracks.
	PhasedRelease bool `bun:",notnull,default:false"`
	// SubmitAsDraft submits without releasing — uploads the build but doesn't push to TestFlight/Play.
	// Useful for staged manual review.
	SubmitAsDraft bool `bun:",notnull,default:false"`
	// Region pins the Fly machine region for deploys. Empty = inherit
	// FLY_PRIMARY_REGION. Only honored when the org's plan has
	// CanChooseRegion=true (Pro+); the deploy controller falls back to the
	// primary region otherwise. See web_deploy_controller.go:resolveDeployRegion.
	Region string `bun:",notnull,default:''"`
	// BetterAuthSecretCredRef is the encrypted-at-rest reference to this
	// project's per-customer BETTER_AUTH_SECRET (32 random bytes hex-encoded,
	// envelope-encrypted via CredStore). Generated lazily on the first build
	// or deploy when MobileAppConfig.AuthEnabled is true. Never returned to
	// the agent or the user — deploy-time concern only.
	BetterAuthSecretCredRef *CredRefJSON `bun:"better_auth_secret_cred_ref,type:jsonb,nullzero"`
	CreatedAt       time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt       time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt       time.Time      `bun:",soft_delete,nullzero"`
}

// ProjectType values used in AppProject.ProjectType.
const (
	ProjectTypeApp             = "app"
	ProjectTypeEcommerce       = "ecommerce"
	ProjectTypeMobileAppNative = "mobile_app_native"
)
