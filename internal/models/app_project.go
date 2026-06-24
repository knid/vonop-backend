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
	ID              uuid.UUID     `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID          uuid.UUID     `bun:",notnull,type:uuid"`
	User            *User         `bun:"rel:belongs-to,join:user_id=id"`
	OrganizationID  uuid.UUID     `bun:",notnull,type:uuid"`
	Organization    *Organization `bun:"rel:belongs-to,join:organization_id=id"`
	Name            string        `bun:",notnull"`
	Slug            string        `bun:",notnull,unique"`
	Prompt          string        `bun:",notnull,type:text"`
	ProjectType     string        `bun:",notnull,default:'app'"` // one of: ProjectType{App,Ecommerce,MobileAppNative}
	FrontendRuntime string        `bun:",notnull,default:'nextjs_legacy'"`
	FrontendHosting string        `bun:",notnull,default:'fly'"`
	BackendEnabled  bool          `bun:",notnull,default:false"`
	BackendRuntime  string        `bun:",notnull,default:'none'"`
	// AST field removed — files now stored in git repos
	Theme           []byte         `bun:",type:jsonb,default:'{}'"` // stores AppTheme as JSON
	DataModels      []byte         `bun:",type:jsonb,default:'[]'"` // stores data model definitions
	Settings        map[string]any `bun:",type:jsonb,default:'{}'"`
	ScreenshotS3Key string         `bun:",notnull,default:''"`
	LogoS3Key       string         `bun:",notnull,default:''"`
	SandboxID       string         `bun:",notnull,default:''"`
	Status          string         `bun:",notnull,default:'draft'"` // draft, generating, ready, deployed
	TemplateID      *uuid.UUID     `bun:",type:uuid,nullzero"`      // source template if created from a template
	// Region pins the Fly machine region for deploys. Empty = inherit
	// FLY_PRIMARY_REGION. Only honored when the org's plan has
	// CanChooseRegion=true (Pro+); the deploy controller falls back to the
	// primary region otherwise. See web_deploy_controller.go:resolveDeployRegion.
	Region string `bun:",notnull,default:''"`
	// BetterAuthSecretCredRef is the encrypted-at-rest reference to this
	// project's per-customer BETTER_AUTH_SECRET (32 random bytes hex-encoded,
	// envelope-encrypted via CredStore). Generated lazily on the first build
	// or deploy. Never returned to the agent or the user.
	BetterAuthSecretCredRef *CredRefJSON `bun:"better_auth_secret_cred_ref,type:jsonb,nullzero"`
	CreatedAt               time.Time    `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt               time.Time    `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt               time.Time    `bun:",soft_delete,nullzero"`
}

// ProjectType values used in AppProject.ProjectType.
const (
	ProjectTypeApp             = "app"
	ProjectTypeEcommerce       = "ecommerce"
	ProjectTypeMobileAppNative = "mobile_app_native"
)

const (
	FrontendRuntimeNextJSLegacy = "nextjs_legacy"
	FrontendRuntimeViteReact    = "vite_react"
	FrontendRuntimeExpoNative   = "expo_native"
	FrontendRuntimeNone         = "none"

	FrontendHostingFly       = "fly"
	FrontendHostingS3CDN     = "s3_cdn"
	FrontendHostingNone      = "none"

	BackendRuntimeNone      = "none"
	BackendRuntimeNextJSAPI = "nextjs_api"
)

func (p *AppProject) UsesSplitWebRuntime() bool {
	return p != nil &&
		p.ProjectType == ProjectTypeApp &&
		p.FrontendRuntime == FrontendRuntimeViteReact
}

func (p *AppProject) UsesBackendSubtree() bool {
	return p != nil &&
		p.BackendEnabled &&
		p.BackendRuntime == BackendRuntimeNextJSAPI
}
