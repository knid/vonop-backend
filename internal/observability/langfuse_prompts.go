package observability

const (
	PromptAgentBase       = "wemob-agent-base"
	PromptAgentMobile     = "wemob-agent-mobile-native"
	PromptAgentWeb        = "wemob-agent-web"
	PromptProjectClassify = "wemob-project-classify"
	PromptProjectName     = "wemob-project-name"
	PromptListingDraft    = "wemob-listing-draft"
	PromptValidation      = "wemob-typescript-validation"
)

type PromptLink struct {
	Name    string
	Version int
}
