package observability

const (
	PromptAgentBase                   = "wemob-agent-base"
	PromptAgentMobile                 = "wemob-agent-mobile-native"
	PromptAgentWeb                    = "wemob-agent-web"
	PromptAgentWebBase                = "wemob-agent-web-base"
	PromptAgentMobileBase             = "wemob-agent-mobile-base"
	PromptAgentWebFirstGen            = "wemob-agent-web-first-gen"
	PromptAgentWebChat                = "wemob-agent-web-chat"
	PromptAgentMobileFirstGen         = "wemob-agent-mobile-first-gen"
	PromptAgentMobileChat             = "wemob-agent-mobile-chat"
	PromptAgentPlanningDiscipline     = "wemob-agent-planning-discipline"
	PromptAgentProjectTypeBoundary    = "wemob-agent-project-type-boundary"
	PromptAgentSplitWebAddendum       = "wemob-agent-split-web-addendum"
	PromptAgentBackendOnlyAddendum    = "wemob-agent-backend-only-addendum"
	PromptAgentEcommerceAddendum      = "wemob-agent-ecommerce-addendum"
	PromptAgentAuthAddendum           = "wemob-agent-auth-addendum"
	PromptAgentDatabaseAddendum       = "wemob-agent-database-addendum"
	PromptAgentDatabaseNativeAddendum = "wemob-agent-database-native-addendum"
	PromptAgentDatabaseSplitAddendum  = "wemob-agent-database-split-web-addendum"
	PromptAgentLocalDatabaseAddendum  = "wemob-agent-local-database-addendum"
	PromptAgentAIAddendum             = "wemob-agent-ai-addendum"
	PromptAgentAISplitAddendum        = "wemob-agent-ai-split-web-addendum"
	PromptAgentMobileConfigAddendum   = "wemob-agent-mobile-config-addendum"
	PromptAgentAttachmentsAddendum    = "wemob-agent-attachments-addendum"
	PromptProjectClassify             = "wemob-project-classify"
	PromptProjectName                 = "wemob-project-name"
	PromptListingDraft                = "wemob-listing-draft"
	PromptValidation                  = "wemob-typescript-validation"
)

type PromptLink struct {
	Name    string `json:"name"`
	Version int    `json:"version,omitempty"`
}
