package observability

const (
	PromptAgentBase                  = "wemob-agent-base"
	PromptAgentMobile                = "wemob-agent-mobile-native"
	PromptAgentWeb                   = "wemob-agent-web"
	PromptAgentWebBase               = "wemob-agent-web-base"
	PromptAgentMobileBase            = "wemob-agent-mobile-base"
	PromptAgentWebFirstGen           = "wemob-agent-web-first-gen"
	PromptAgentWebEdit               = "wemob-agent-web-edit"
	PromptAgentMobileFirstGen        = "wemob-agent-mobile-first-gen"
	PromptAgentMobileEdit            = "wemob-agent-mobile-edit"
	PromptAgentPlanningDiscipline    = "wemob-agent-planning-discipline"
	PromptAgentProjectTypeBoundary   = "wemob-agent-project-type-boundary"
	PromptAgentArchitectureSplitWeb  = "wemob-agent-architecture-split-web"
	PromptAgentArchitectureBackend   = "wemob-agent-architecture-backend-only"
	PromptAgentEcommerceAddendum     = "wemob-agent-ecommerce-addendum"
	PromptAgentAuthAddendum          = "wemob-agent-auth-addendum"
	PromptAgentDatabaseNextJS        = "wemob-agent-database-nextjs"
	PromptAgentDatabaseSplitWeb      = "wemob-agent-database-split-web"
	PromptAgentDatabaseNativeBackend = "wemob-agent-database-native-backend"
	PromptAgentDatabaseMobileLocal   = "wemob-agent-database-mobile-local"
	PromptAgentAINextJS              = "wemob-agent-ai-nextjs"
	PromptAgentAISplitWeb            = "wemob-agent-ai-split-web"
	PromptAgentMobileConfigAddendum  = "wemob-agent-mobile-config-addendum"
	PromptAgentAttachmentsAddendum   = "wemob-agent-attachments-addendum"
	PromptProjectClassify            = "wemob-project-classify"
	PromptProjectName                = "wemob-project-name"
	PromptListingDraft               = "wemob-listing-draft"
	PromptValidation                 = "wemob-typescript-validation"
)

type PromptLink struct {
	Name    string `json:"name"`
	Version int    `json:"version,omitempty"`
}
