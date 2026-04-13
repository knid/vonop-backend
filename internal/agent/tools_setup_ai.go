package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knid/wemob/internal/templates"
)

// SetupAIGatewayTool is the agent-callable entry point for adding AI to a
// project via the Wemob AI Gateway. It is registered unconditionally; the
// tool internally rejects re-runs once Settings["has_ai"] is true so the model
// never accidentally rewrites the helper twice.
//
// On success the tool:
//  1. Flips Settings["has_ai"] = true via the EnableAI callback so the
//     AIAddendum is appended on subsequent runs.
//  2. Writes lib/wemob-ai.ts into the file map (chat / chatStream /
//     generateImage helpers wired to the gateway).
//  3. Returns a result describing the env vars (auto-injected),
//     available models, and how to call the helper from API routes.
type SetupAIGatewayTool struct {
	Deps *RunDependencies
}

func (SetupAIGatewayTool) Name() string { return "setup_wemob_ai_gateway" }

func (SetupAIGatewayTool) Description() string {
	return "Add AI capabilities to this project via the Wemob AI Gateway. Call this exactly once when the user asks for AI features (chat, chatbot, summarization, content generation, image generation) without naming a specific provider. The tool wires up server-side env vars, writes lib/wemob-ai.ts with chat/chatStream/generateImage helpers, and unlocks the AI addendum for the rest of the run. Do NOT call this if the user explicitly named a third-party AI provider (OpenAI, Anthropic, OpenRouter, etc.) — use the matching integration block instead. Do NOT call this for projects that do not need AI."
}

func (SetupAIGatewayTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"purpose": map[string]any{
				"type":        "string",
				"description": "Short description of what the AI is for in this project (e.g. 'support chatbot', 'blog post summarizer', 'recipe generator', 'product image generation'). Used for telemetry and to confirm intent.",
			},
		},
		"required": []string{"purpose"},
	}
}

func (s SetupAIGatewayTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Purpose string `json:"purpose"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if args.Purpose == "" {
		return "purpose is required — describe in one sentence what the AI is for in this project", true, nil
	}

	if s.Deps == nil || s.Deps.EnableAI == nil {
		return "AI gateway setup is not available in this environment", true, nil
	}

	// Reject if AI is already wired so the model uses the existing helper
	// instead of rewriting it.
	if rc.Project != nil && rc.Project.Settings != nil {
		if v, ok := rc.Project.Settings["has_ai"].(bool); ok && v {
			return "this project already has the Wemob AI Gateway wired up — import { chat, chatStream, generateImage } from '@/lib/wemob-ai' and use the existing helper instead of calling setup_wemob_ai_gateway again", true, nil
		}
	}

	if err := s.Deps.EnableAI(ctx, rc.Project); err != nil {
		return fmt.Sprintf("enabling AI failed: %v", err), true, nil
	}

	// Inject lib/wemob-ai.ts so the model can `import { chat } from
	// '@/lib/wemob-ai'` immediately within the same run.
	if libAI := templates.GetTemplate("lib/wemob-ai.ts"); libAI != "" {
		rc.FileMap.Set("lib/wemob-ai.ts", libAI)
		rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
			RunID:     rc.RunID,
			Path:      "lib/wemob-ai.ts",
			Operation: "create",
			Bytes:     len(libAI),
			Content:   libAI,
		})
	}

	result := `Wemob AI Gateway is ready for this project.

What is now true:
- lib/wemob-ai.ts has been written. Import the helpers with:
    import { chat, chatStream, generateImage } from '@/lib/wemob-ai'
- Two env vars are auto-injected at build and runtime — do NOT add them
  yourself, do NOT ask the user to paste an API key, do NOT create a
  settings screen for choosing a model:
    WEMOB_AI_KEY                  (server-side only, never expose)
    NEXT_PUBLIC_WEMOB_GATEWAY_URL (OpenAI-compatible base URL)
- Available chat models (pass as the model field, no provider prefix):
    claude-sonnet-4    — best balance of quality and speed (default)
    claude-haiku-4-5   — fastest and cheapest, great for chat replies
    claude-opus-4      — highest reasoning quality, slow and expensive
    gpt-4.1            — OpenAI flagship
    gpt-4.1-mini       — OpenAI cheap and fast
    gemini-2.5-pro     — huge context, multimodal
- Available image models (passed to generateImage):
    fal-ai/flux-schnell   — fast (default)
    fal-ai/flux-pro/v1.1  — HD
    fal-ai/flux-ultra     — premium

Rules for using it:
1. ALWAYS call from server-side code only — API routes (app/api/**/route.ts)
   or Server Components / Server Actions. NEVER import @/lib/wemob-ai from a
   "use client" file. The key WEMOB_AI_KEY must never reach the browser.
2. For streaming chat UIs, create app/api/chat/route.ts that calls
   chatStream(...) and returns the ReadableStream wrapped in a Response.
   On the client, fetch('/api/chat'), read response.body as a stream, and
   render tokens as they arrive.
3. For one-shot text generation (summarize, classify, rewrite), call chat(...)
   from a Server Action or Route Handler and return the string.
4. For image generation, call generateImage({ prompt }) from a Server Action
   or Route Handler and return the URL array. Render with next/image.
5. In every user-facing message, call this "AI" or "yapay zeka" — never
   "Wemob Gateway", "OpenRouter", "Claude", "GPT", "Anthropic", or any
   underlying model name. The user does not care what powers it.

Next steps for this run:
1. Build the AI feature the user asked for (chat UI, summarizer, image
   generator, etc.) using the helper above.
2. Always create the API route first, then wire the client to call it.
3. Show a loading state while the request is in flight and a friendly
   error toast if it fails.`

	return result, false, nil
}
