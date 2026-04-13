package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/utils"

	_ "github.com/lib/pq"
)

// --- Conversation types ---

// ConversationReply is a natural language reply from the user during the pre-build conversation.
type ConversationReply struct {
	Message string `json:"message"`
}

// --- Clarification types (deprecated — use ConversationReply instead) ---

// ClarificationQuestion is a single question the LLM generates to gather context.
type ClarificationQuestion struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Type     string   `json:"type"`     // "text" or "choice"
	Options  []string `json:"options"`  // non-empty when Type == "choice"
	Required bool     `json:"required"`
}

// ClarificationAnswer is a single user answer to a clarification question.
type ClarificationAnswer struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
}

// ClarificationResult is sent through the pending channel from the clarify endpoint.
type ClarificationResult struct {
	Answers []ClarificationAnswer
	Skip    bool
}

type appFileStructure struct {
	Name            string             `json:"name"`
	Slug            string             `json:"slug"`
	NeedsAuth       bool               `json:"needs_auth,omitempty"`
	ColorScheme     map[string]any     `json:"colorScheme"`
	DarkColorScheme map[string]any     `json:"darkColorScheme,omitempty"`
	Typography      map[string]any     `json:"typography,omitempty"`
	Files           []appFilePlanEntry `json:"files"`
	DataModels      []models.DataModel `json:"data_models,omitempty"`
	RequiredEnvVars []RequiredEnvVar   `json:"required_env_vars,omitempty"`
}

type appFilePlanEntry struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// PlanAppFileStructure calls the LLM to generate a file structure plan from a prompt (non-streaming).
func (c *Controller) PlanAppFileStructure(ctx context.Context, prompt string, projectType string) (*appFileStructure, error) {
	provider := c.llmProvider()
	model := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL")
	if model == "" {
		model = provider.DefaultModel()
	}

	maxFiles := c.RemoteConfig.GetInt(remoteconfig.AIMaxStructureFiles, remoteconfig.Global)
	systemPrompt := constants.NextjsStructurePrompt(maxFiles)
	if projectType == "ecommerce" {
		systemPrompt = constants.NEXTJS_ECOMMERCE_STRUCTURE_PROMPT
	} else if projectType == "mobile_app" {
		systemPrompt = constants.NextjsMobileStructurePrompt(maxFiles)
	}

	// Try up to 2 times — structure planning is critical for app quality
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		attemptModel := model
		if attempt > 0 {
			// Retry with fast model as fallback — it's cheaper and may succeed
			// where the main model hit rate limits
			attemptModel = provider.FastModel()
			slog.Info("Retrying structure planning with fast model",
				"attempt", attempt+1, "model", attemptModel)
		}

		// Wrap prompt with anti-conversation guard: when the prompt contains
		// pre-build conversation history, the model sometimes continues chatting
		// instead of producing JSON. This explicit instruction prevents that.
		planPrompt := prompt
		if strings.Contains(prompt, "Pre-build conversation") {
			planPrompt = "CRITICAL: You are in STRUCTURED JSON OUTPUT mode. The conversation below is context only — do NOT continue the conversation. Output ONLY the JSON object as specified in the system prompt.\n\n" + prompt
		}
		structureJSON, err := utils.GetJSONFromLLMWithMaxTokens(ctx, attemptModel, systemPrompt, planPrompt, provider, 32768)
		if err != nil {
			slog.Error("failed to generate app structure",
				"error", err, "attempt", attempt+1, "model", attemptModel)
			lastErr = err
			continue
		}

		var structure appFileStructure
		if err := json.Unmarshal([]byte(structureJSON), &structure); err != nil {
			cleaned := utils.ExtractJSON(structureJSON)
			if err2 := json.Unmarshal([]byte(cleaned), &structure); err2 != nil {
				// Attempt to repair truncated JSON (e.g. LLM hit token limit mid-output)
				repaired := repairTruncatedStructureJSON(cleaned)
				if err3 := json.Unmarshal([]byte(repaired), &structure); err3 != nil {
					slog.Error("failed to parse app structure",
						"error", err3, "attempt", attempt+1, "json_length", len(structureJSON))
					lastErr = fmt.Errorf("failed to parse app structure: %w", err3)
					continue
				}
				slog.Info("repaired truncated structure JSON",
					"attempt", attempt+1, "files", len(structure.Files))
			}
		}

		if len(structure.Files) == 0 {
			lastErr = fmt.Errorf("structure planning returned no files")
			continue
		}

		return &structure, nil
	}

	return nil, fmt.Errorf("structure planning failed after retries: %w", lastErr)
}

// repairTruncatedStructureJSON attempts to fix JSON that was truncated mid-output
// (e.g. when the LLM hits the max_tokens limit). It closes open strings, arrays,
// and objects so the JSON can be parsed — we get a partial but usable structure.
func repairTruncatedStructureJSON(s string) string {
	if len(s) == 0 {
		return s
	}

	// Strip any trailing incomplete key-value pair (e.g. `"path": "app/some`)
	// by finding the last complete JSON element
	result := []byte(s)

	// Remove trailing characters that are clearly mid-value
	for len(result) > 0 {
		last := result[len(result)-1]
		if last == ',' || last == ' ' || last == '\n' || last == '\r' || last == '\t' {
			result = result[:len(result)-1]
		} else {
			break
		}
	}

	// If we're mid-string, close it
	inString := false
	escaped := false
	for _, ch := range result {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
		}
	}
	if inString {
		result = append(result, '"')
	}

	// Count open brackets/braces and close them
	openBraces := 0
	openBrackets := 0
	inStr := false
	esc := false
	for _, ch := range result {
		if esc {
			esc = false
			continue
		}
		if ch == '\\' && inStr {
			esc = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch ch {
		case '{':
			openBraces++
		case '}':
			openBraces--
		case '[':
			openBrackets++
		case ']':
			openBrackets--
		}
	}

	// Remove trailing comma before we close
	trimmed := bytes.TrimRight(result, " \t\n\r,")
	result = trimmed

	for openBrackets > 0 {
		result = append(result, ']')
		openBrackets--
	}
	for openBraces > 0 {
		result = append(result, '}')
		openBraces--
	}

	return string(result)
}

// ApplyStructureToProject saves the structure plan output (file plan, data models,
// needs_auth, required_env_vars) to the project so it can be injected into the
// generation context. Call this from views after PlanAppFileStructure.
func (c *Controller) ApplyStructureToProject(project *models.AppProject, structure *appFileStructure) {
	if project.Settings == nil {
		project.Settings = make(map[string]any)
	}

	// Save file plan
	if len(structure.Files) > 0 {
		project.Settings["file_plan"] = structure.Files
	}

	// Save data models
	if len(structure.DataModels) > 0 {
		if dmJSON, err := models.MarshalDataModels(structure.DataModels); err != nil {
			slog.Warn("invalid data models from structure plan, skipping", "error", err)
		} else {
			project.DataModels = dmJSON
		}
	}

	// Save needs_auth
	if structure.NeedsAuth {
		project.Settings["needs_auth"] = true
	}

	// Save required env vars
	if len(structure.RequiredEnvVars) > 0 {
		project.Settings["required_env_vars"] = structure.RequiredEnvVars
	}
}
