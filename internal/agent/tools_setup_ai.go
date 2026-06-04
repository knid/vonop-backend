package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/templates"
)

// mobileNativeSetupAIResultTemplate is the success payload returned for
// mobile_app_native projects. The AI gateway helper lives in backend/
// (the per-customer Next.js backend) — never in the mobile/ subtree —
// because WEMOB_AI_KEY is server-side-only and React Native binaries
// must never embed it. The mobile app calls a backend route via
// apiFetch; the backend route calls wemobAI(); the gateway proxies to
// OpenRouter / fal.ai.
const mobileNativeSetupAIResultTemplate = `Wemob AI Gateway is ready for this project.

What is now true:
- backend/lib/wemob-ai.ts has been written. It exports a preconfigured
  OpenAI client plus a small image helper:
    import { wemobAI, generateImage } from '@/lib/wemob-ai';
- The ` + "`openai`" + ` npm package has been added to backend/package.json
  (a minimal Next.js backend was also scaffolded under backend/ if it
  didn't already exist). Do NOT add openai or next/react/react-dom again.
- Two env vars are auto-injected into the deployed Fly app at deploy
  time — do NOT add them yourself, do NOT ask the user to paste an API
  key, do NOT create a settings screen for choosing a model:
    WEMOB_AI_KEY                  (server-side only, never expose)
    NEXT_PUBLIC_WEMOB_GATEWAY_URL (OpenAI-compatible base URL)
- Available chat models on the user's current plan (pass as the model
  field on wemobAI().chat.completions.create — models not listed here
  will be rejected by the gateway with HTTP 403):
%s
- Available image models on the user's current plan (passed to generateImage):
%s

How to wire AI into a mobile_app_native app:

  CRITICAL ARCHITECTURE: AI features cross two tiers. The mobile app
  (mobile/) NEVER imports backend/lib/wemob-ai. Instead it calls a
  backend HTTP route via apiFetch. The backend route calls wemobAI().

1. Backend route (backend/app/api/<feature>/route.ts):
    import { wemobAI } from '@/lib/wemob-ai';
    import { NextRequest, NextResponse } from 'next/server';
    export async function POST(req: NextRequest) {
      const { messages } = await req.json();
      const res = await wemobAI().chat.completions.create({
        model: '%s',
        messages,
      });
      return NextResponse.json({ text: res.choices[0]?.message?.content ?? '' });
    }

2. Mobile screen (mobile/app/<route>.tsx):
    import { apiFetch } from '@/lib/api';
    const data = await apiFetch<{ text: string }>('/api/<feature>', {
      method: 'POST',
      body: JSON.stringify({ messages: [{ role: 'user', content: '...' }] }),
    });

- Streaming chat — return the OpenAI stream directly from the route via
  Response constructor with a TransformStream. Mobile-side, consume the
  stream chunk-by-chunk via fetch + ReadableStream.getReader(). Don't
  use the OpenAI SDK on the mobile side — it's a server-only library.
- Tool calling / structured output — pass ` + "`tools`" + `, ` + "`tool_choice`" + `, or
  ` + "`response_format`" + ` directly to chat.completions.create on the backend.
- Vision (image input) — accept image URLs in the request body and
  pass them through as image_url content parts in the message array.
- Image generation:
    const urls = await generateImage({ prompt: '...' });
    // return urls[0] from the route; mobile renders with <Image source={{ uri: urls[0] }} />

Rules:
1. backend/lib/wemob-ai.ts MUST only be imported from backend/app/api/**/route.ts,
   Server Components, or Server Actions. NEVER export it through any
   route that returns it to the client. NEVER import it from anything
   under mobile/. WEMOB_AI_KEY must never reach the React Native binary.
2. In every user-facing message + UI string, call this "AI" or
   "yapay zeka" — never "Wemob Gateway", "OpenRouter", "Claude", "GPT",
   "Anthropic", or any underlying model name.

Next steps for this run:
1. Build the AI feature the user asked for (chat UI, summarizer, image
   generator, etc.).
2. Always create the backend route handler first, then wire the mobile
   screen to call it via apiFetch.
3. Show a loading state while the request is in flight + a friendly
   error toast (use react-native-reusables primitives) if it fails.`

const splitWebSetupAIResultTemplate = `Wemob AI Gateway is ready for this project.

What is now true:
- backend/lib/wemob-ai.ts has been written. It exports a preconfigured
  OpenAI client plus a small image helper:
    import { wemobAI, generateImage } from '@/lib/wemob-ai';
- The ` + "`openai`" + ` npm package has been added to backend/package.json
  (a minimal Next.js backend was also scaffolded under backend/ if it
  didn't already exist). Do NOT add openai or next/react/react-dom again.
- Two env vars are auto-injected into the deployed backend at deploy time:
    WEMOB_AI_KEY                  (server-side only, never expose)
    NEXT_PUBLIC_WEMOB_GATEWAY_URL (OpenAI-compatible base URL)
- Available chat models on the user's current plan:
%s
- Available image models on the user's current plan:
%s

How to wire AI into this split web app:

  CRITICAL ARCHITECTURE: AI features cross two tiers. The Vite frontend
  (frontend/) NEVER imports backend/lib/wemob-ai. Instead it calls a backend
  HTTP route. The backend route calls wemobAI().

1. Backend route (backend/app/api/<feature>/route.ts):
    import { wemobAI } from '@/lib/wemob-ai';
    import { NextRequest, NextResponse } from 'next/server';
    export async function POST(req: NextRequest) {
      const { messages } = await req.json();
      const res = await wemobAI().chat.completions.create({
        model: '%s',
        messages,
      });
      return NextResponse.json({ text: res.choices[0]?.message?.content ?? '' });
    }

2. Frontend component (frontend/src/...):
    import { apiFetch } from '@/lib/api';
    const data = await apiFetch<{ text: string }>('/api/<feature>', {
      method: 'POST',
      json: { messages: [{ role: 'user', content: '...' }] },
    });

- Streaming chat: use model '%s' in the backend route, return a streamed
  Response, and consume it from frontend/ with fetch + ReadableStream.

Rules:
1. backend/lib/wemob-ai.ts MUST only be imported from backend/app/api/**/route.ts
   or other backend/server files. NEVER import it from frontend/.
2. In every user-facing message + UI string, call this "AI" or
   "yapay zeka" — never "Wemob Gateway", "OpenRouter", "Claude", "GPT",
   "Anthropic", or any underlying model name.`

// setupAIResultTemplate is the success payload the tool returns. The chat
// model list and the default model ID are injected at call time so the LLM
// only ever sees models the user's tier can actually reach. Before this was
// a static block including google/gemini-2.5-pro, which hit a 403 for every
// non-business customer at runtime.
const setupAIResultTemplate = `Wemob AI Gateway is ready for this project.

What is now true:
- lib/wemob-ai.ts has been written. It exports a preconfigured OpenAI
  client plus a small image helper:
    import { wemobAI, generateImage } from '@/lib/wemob-ai';
- The ` + "`openai`" + ` npm package has been added to package.json. Do NOT
  add it again. Do NOT add any other AI SDK (anthropic, google-ai, etc.).
- Two env vars are auto-injected at build and runtime — do NOT add them
  yourself, do NOT ask the user to paste an API key, do NOT create a
  settings screen for choosing a model:
    WEMOB_AI_KEY                  (server-side only, never expose)
    NEXT_PUBLIC_WEMOB_GATEWAY_URL (OpenAI-compatible base URL)
- Available chat models on the user's current plan (pass as the model field
  on wemobAI().chat.completions.create — models not listed here will be
  rejected by the gateway with HTTP 403):
%s
- Available image models on the user's current plan (passed to generateImage):
%s

How to call it:
- One-shot text (summarize, classify, rewrite):
    const res = await wemobAI().chat.completions.create({
      model: '%s',
      messages: [{ role: 'user', content: '...' }],
    });
    const text = res.choices[0]?.message?.content ?? '';
- Streaming chat UI — use the OpenAI SDK's native streaming:
    const stream = await wemobAI().chat.completions.create({
      model: '%s',
      messages,
      stream: true,
    });
    for await (const chunk of stream) { ... }
  In an API route, return a streamed Response built from the async
  iterator (the standard OpenAI + Next.js pattern).
- Tool calling / structured output — pass ` + "`tools`" + `, ` + "`tool_choice`" + `, or
  ` + "`response_format`" + ` directly to chat.completions.create. They are
  forwarded to the upstream untouched.
- Vision (image input) — models tagged [vision] above accept image_url
  content parts exactly like the OpenAI SDK. Send images as data URLs or
  public URLs; never as raw base64 without the data: prefix.
- Image generation:
    const urls = await generateImage({ prompt: '...' });
    // render urls[0] with next/image

Rules:
1. ALWAYS call from server-side code only — API routes (app/api/**/route.ts),
   Server Components, or Server Actions. NEVER import @/lib/wemob-ai from a
   "use client" file. The key WEMOB_AI_KEY must never reach the browser.
2. In every user-facing message, call this "AI" or "yapay zeka" — never
   "Wemob Gateway", "OpenRouter", "Claude", "GPT", "Anthropic", or any
   underlying model name. The user does not care what powers it.

Next steps for this run:
1. Build the AI feature the user asked for (chat UI, summarizer, image
   generator, etc.) using the helper above.
2. Always create the API route first, then wire the client to call it.
3. Show a loading state while the request is in flight and a friendly
   error toast if it fails.`

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
			return "this project already has the Wemob AI Gateway wired up — import { wemobAI, generateImage } from '@/lib/wemob-ai' and use the existing helper instead of calling setup_wemob_ai_gateway again", true, nil
		}
	}

	// Resolve allowed chat models for this user's tier BEFORE enabling AI
	// on the project, so we can refuse early on a tier that has no chat
	// access without leaving the project in a half-wired state.
	allowedChat := gatewayAllowedChatModels(s.Deps)
	if len(allowedChat) == 0 {
		return "no chat models are available on the user's current plan — the Wemob AI Gateway cannot be wired up until they upgrade", true, nil
	}

	// Resolve the template body up front. EnableAI must come LAST — if we
	// flip Settings["has_ai"]=true and then discover the template is
	// missing, the project is permanently stuck: every subsequent run
	// sees "already wired" and skips setup, but lib/wemob-ai.ts never
	// exists so every `@/lib/wemob-ai` import fails the build. Failing
	// closed here keeps the project recoverable.
	libAI := templates.GetTemplate("lib/wemob-ai.ts")
	if libAI == "" {
		return "Wemob AI Gateway template is not available in this build — cannot wire up AI right now; please retry or contact support if this persists", true, nil
	}

	// Branch on project type: mobile_app_native projects host the AI
	// helper in their backend/ subtree (the per-customer Next.js backend
	// that will deploy to Fly), not at the project root. The mobile
	// binary itself NEVER imports the helper — WEMOB_AI_KEY is
	// server-side-only and must never reach a React Native build.
	isMobileNative := rc.Project != nil && rc.Project.ProjectType == models.ProjectTypeMobileAppNative
	isSplitWeb := rc.Project != nil && (rc.Project.UsesSplitWebRuntime() || rc.Project.FrontendRuntime == models.FrontendRuntimeNone)
	libAIPath := "lib/wemob-ai.ts"
	pkgJSONPath := "package.json"
	resultTemplate := setupAIResultTemplate
	if isMobileNative || isSplitWeb {
		libAIPath = "backend/lib/wemob-ai.ts"
		pkgJSONPath = "backend/package.json"
		resultTemplate = mobileNativeSetupAIResultTemplate
		if isSplitWeb {
			resultTemplate = splitWebSetupAIResultTemplate
		}

		// Scaffold a minimal Next.js backend if the agent hasn't created
		// one yet. Without backend/package.json, addDependenciesToPackageJSONAt
		// would no-op and the project would be undeployable. The deploy
		// worker auto-writes backend/Dockerfile if missing (per P1.1.D),
		// so we only need the bare-minimum Next.js source structure here.
		if _, ok := rc.FileMap.Get(pkgJSONPath); !ok {
			scaffoldMinimalBackend(rc)
		}
	}

	// Inject lib/wemob-ai.ts so the model can
	// `import { wemobAI } from '@/lib/wemob-ai'` immediately within the
	// same run.
	rc.FileMap.Set(libAIPath, libAI)
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID:     rc.RunID,
		Path:      libAIPath,
		Operation: "create",
		Bytes:     len(libAI),
		Content:   libAI,
	})

	// lib/wemob-ai.ts imports the `openai` npm package. The LLM never
	// wrote that import so it won't add the dep itself — inject it here
	// exactly like provision_database does for neon/drizzle.
	addDependenciesToPackageJSONAt(rc, pkgJSONPath, map[string]string{
		"openai": "^4.104.0",
	})

	// Flip the flag only after the file + dep are in place. On failure
	// here the file is still committed at run end, so a retry can see
	// the file and either the flag flips on the retry or the model
	// imports the existing helper without a second setup call.
	if err := s.Deps.EnableAI(ctx, rc.Project); err != nil {
		return fmt.Sprintf("enabling AI failed: %v", err), true, nil
	}

	modelList := renderGatewayChatModelsBlock(allowedChat)
	imageModelList := renderGatewayImageModelsBlock(gatewayAllowedImageModels(s.Deps))
	defaultModel := defaultGatewayChatModel(allowedChat)
	result := fmt.Sprintf(resultTemplate, modelList, imageModelList, defaultModel, defaultModel)

	return result, false, nil
}
