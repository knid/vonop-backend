# Langfuse Observability

This document tracks the operational setup and prompt inventory for the Wemob Langfuse integration.

## Required Secrets

Set these in Infisical for every environment that should export Langfuse traces:

| Secret | Required | Purpose |
| --- | --- | --- |
| `LANGFUSE_PUBLIC_KEY` | Yes | Langfuse project public key used for OTLP basic auth. |
| `LANGFUSE_SECRET_KEY` | Yes | Langfuse project secret key used for OTLP basic auth. |
| `LANGFUSE_BASE_URL` | No | Langfuse host used to build the OTLP traces endpoint. Defaults to `https://cloud.langfuse.com`. |
| `LANGFUSE_OTEL_TRACES_ENDPOINT` | No | Full OTLP traces endpoint. Overrides `LANGFUSE_BASE_URL` when set. |

Runtime environment is read from `ENVIRONMENT`, then `APP_ENV`, then `SecretManager.GetEnv()`, with `default` as the final fallback.

Environment variables override Infisical-backed `SecretManager` values. This applies to `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY`, `LANGFUSE_BASE_URL`, and `LANGFUSE_OTEL_TRACES_ENDPOINT`; use env vars only for local overrides, break-glass debugging, or platform-level secret injection.

## Prompt Inventory

| Langfuse prompt name | Current source file(s) | Runtime owner | Notes |
| --- | --- | --- | --- |
| `wemob-agent-base` | `internal/agent/prompt_compact.go`, `internal/agent/agent_profile.go` | Agent run loop | Base builder identity and shared tool protocol. `CompactBasePromptWeb` and `CompactBasePromptMobileNative` are selected through `AgentProfile.BasePrompt`. |
| `wemob-agent-mobile-native` | `internal/agent/prompt_compact.go`, `internal/agent/prompt_mobile_native.go`, `internal/agent/agent_profile.go` | Mobile builder profile | Native mobile base prompt plus first-generation/chat-mode addenda selected by `mobileBuilderProfile`. |
| `wemob-agent-web` | `internal/agent/prompt_compact.go`, `internal/agent/prompt.go`, `internal/agent/prompt_addenda.go`, `internal/agent/agent_profile.go` | Web builder profile | Web base prompt plus first-generation/chat-mode addenda. Additional web addenda such as split web, backend-only, ecommerce, auth, database, and AI gateway constraints are assembled from `prompt_addenda.go`. |
| `wemob-project-classify` | `internal/controllers/project_classify.go` | Project creation classifier | Fast LLM system prompt that classifies the initial user prompt into `ecommerce`, `app`, or `mobile_app_native`. |
| `wemob-project-name` | `internal/controllers/project_name.go` | Project naming flow | Fast LLM system prompt that derives a short English app name before slug generation. |
| `wemob-listing-draft` | `internal/constants/listing_draft_prompts.go`, `internal/controllers/listing_draft_controller.go` | Store listing draft endpoint | JSON-only listing copy prompt for title, description, and release notes. |
| `wemob-typescript-validation` | `internal/constants/ai_validation.go`, `internal/controllers/app_build_controller.go` | App file validation | JSON-only TypeScript/Next.js validation prompt used by `ValidateAppFiles`. |

When a prompt is moved into Langfuse prompt management, keep this table as the source inventory and update it with the Langfuse prompt version, local fallback file, and rollout status.

## Deployment Validation Checklist

- Confirm `LANGFUSE_PUBLIC_KEY` and `LANGFUSE_SECRET_KEY` are present in Infisical for the target environment.
- Confirm no stale platform env var is overriding the intended Infisical value for any `LANGFUSE_*` key.
- If using self-hosted Langfuse or a regional cloud host, confirm either `LANGFUSE_BASE_URL` or `LANGFUSE_OTEL_TRACES_ENDPOINT` matches the target project.
- Start the API and worker processes and check logs for `Langfuse OpenTelemetry exporter enabled`.
- Trigger one web builder run, one mobile builder run, one project classification/name generation path, one listing draft, and one TypeScript validation path in a non-production project.
- In Langfuse, verify traces include environment, user, organization, project, run/session identifiers, model, provider, token usage, latency, cost, and tool/generation observations where applicable.
- Verify failed LLM calls surface as error-level observations or trace status, not only application logs.
- Confirm sensitive values are not present in trace inputs, outputs, metadata, or tags.
- Confirm trace volume and ingestion rate are acceptable before enabling the integration for production traffic.

## Recommended Saved Dashboard Views

- **Builder Run Health**: filter traces where the name/action is an agent run; group by environment, agent profile, project type, provider, model, and error status.
- **LLM Cost And Tokens**: aggregate total cost, prompt tokens, completion tokens, cache creation tokens, and cache read tokens by day, model, provider, and project type.
- **Latency By Operation**: compare p50/p95 latency for agent runs, `project_classify`, `project_name`, `listing_draft`, and validation calls.
- **Prompt Version Performance**: when prompts are managed in Langfuse, group by prompt name and version to compare error rate, cost, latency, and user retry rate.
- **Tool And Generation Failures**: filter error-level observations and failed traces; group by tool name, model, agent profile, and deployment environment.
- **High-Cost Sessions**: sort sessions/runs by total cost and token count to identify runaway loops, repeated validation, or oversized prompt contexts.
- **Content Safety Review**: sample traces tagged with production environment and high user impact, excluding traces without stored content, to review prompt/input/output redaction quality.
