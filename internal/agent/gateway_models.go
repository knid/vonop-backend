package agent

import (
	"fmt"
	"slices"
	"strings"

	"github.com/knid/wemob/internal/constants"
)

// gatewayAllowedChatModels returns the chat-only subset of the gateway model
// catalog filtered by what the user's subscription tier can actually reach.
// Order matches the catalog so rendering is stable across runs.
//
// Returns an empty slice when deps is nil, no callback is bound, or the user's
// tier has no chat models allowed — callers decide how to present that state.
func gatewayAllowedChatModels(deps *RunDependencies) []constants.GatewayModelInfo {
	if deps == nil || deps.GatewayAllowedModels == nil {
		return nil
	}
	allowed := deps.GatewayAllowedModels()
	if len(allowed) == 0 {
		return nil
	}
	set := make(map[string]bool, len(allowed))
	for _, id := range allowed {
		set[id] = true
	}
	var out []constants.GatewayModelInfo
	for _, m := range constants.GatewayModelCatalog {
		if m.Type == "chat" && set[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// defaultGatewayChatModel picks the sensible default from an allowed set:
// claude-sonnet-4 (best balance) when present, then claude-haiku-4-5
// (cheap fallback for lower tiers), otherwise whatever comes first.
func defaultGatewayChatModel(models []constants.GatewayModelInfo) string {
	if len(models) == 0 {
		return ""
	}
	prefs := []string{
		"anthropic/claude-sonnet-4",
		"anthropic/claude-haiku-4-5",
	}
	for _, pref := range prefs {
		for _, m := range models {
			if m.ID == pref {
				return pref
			}
		}
	}
	return models[0].ID
}

// renderGatewayChatModelsBlock formats a prompt-ready bullet list of the
// allowed chat models. Each line includes capability tags (vision, etc.) so
// the LLM can pick a model that fits the feature it's building. The model
// returned by defaultGatewayChatModel is marked "(default)".
func renderGatewayChatModelsBlock(models []constants.GatewayModelInfo) string {
	if len(models) == 0 {
		return ""
	}
	def := defaultGatewayChatModel(models)
	var sb strings.Builder
	for _, m := range models {
		caps := []string{}
		if slices.Contains(m.Capabilities, "vision") {
			caps = append(caps, "vision")
		}
		tag := ""
		if len(caps) > 0 {
			tag = " [" + strings.Join(caps, ", ") + "]"
		}
		marker := ""
		if m.ID == def {
			marker = " (default)"
		}
		fmt.Fprintf(&sb, "    %s — %s%s%s\n", m.ID, m.Name, tag, marker)
	}
	return strings.TrimRight(sb.String(), "\n")
}
