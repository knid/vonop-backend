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
| `LANGFUSE_PROMPT_LABEL` | No | Prompt label fetched at runtime. Defaults to `production`; use `staging` for prompt rollout tests. |

Runtime environment is read from `ENVIRONMENT`, then `APP_ENV`, then `SecretManager.GetEnv()`, with `default` as the final fallback.

Environment variables override Infisical-backed `SecretManager` values. This applies to all `LANGFUSE_*` keys; use env vars only for local overrides, break-glass debugging, or platform-level secret injection.

## Prompt Inventory

| Langfuse prompt name | Current source file(s) | Runtime owner | Notes |
| --- | --- | --- | --- |
| `wemob-agent-web-base` | `internal/agent/prompt_compact.go` | Web builder profile | Base web builder identity and tool protocol. |
| `wemob-agent-mobile-base` | `internal/agent/prompt_compact.go` | Mobile builder profile | Base native mobile builder identity and tool protocol. |
| `wemob-agent-mode-first-gen` / `wemob-agent-mode-edit` | `internal/agent/context.go`, `internal/agent/prompt.go`, `internal/agent/prompt_mobile_native.go` | Builder mode prompts | Shared first-generation and edit-mode prompts compiled with web/mobile variables. |
| `wemob-agent-project-type-boundary` | `internal/agent/context.go` | Agent context compiler | Uses Langfuse variables for project type labels. |
| `wemob-agent-planning-discipline` | `internal/agent/context.go` | Agent context compiler | Final batching/tool-use system block. |
| `wemob-agent-architecture-addendum` | `internal/agent/prompt_addenda.go` | Web architecture addenda | Shared architecture prompt compiled with split-runtime or backend-only variables. |
| `wemob-agent-ecommerce-addendum` | `internal/agent/prompt_addenda.go` | Ecommerce projects | Storefront/schema/checkout rules. |
| `wemob-agent-auth-addendum` | `internal/agent/prompt_addenda.go` | Auth-enabled projects | Session/auth implementation constraints. |
| `wemob-agent-capability-database` | `internal/agent/prompt_addenda.go` | Database-enabled projects | Shared database prompt compiled with web, native, split-web, or on-device database variables. |
| `wemob-agent-capability-ai` | `internal/agent/prompt_addenda.go` | AI-enabled projects | Shared AI prompt compiled with runtime, allowed chat/image model lists, and default model variables. |
| `wemob-agent-mobile-config-addendum` | `internal/agent/context.go` | Native mobile projects | Uses Langfuse variables for current mobile config. |
| `wemob-agent-attachments-addendum` | `internal/agent/prompt_addenda.go` | Projects with uploaded assets | Uses a Langfuse variable for sanitized asset table rows. |
| `wemob-project-classify` | `internal/controllers/project_classify.go` | Project creation classifier | Fast LLM system prompt that classifies the initial user prompt into `ecommerce`, `app`, or `mobile_app_native`. |
| `wemob-project-name` | `internal/controllers/project_name.go` | Project naming flow | Fast LLM system prompt that derives a short English app name before slug generation. |
| `wemob-listing-draft` | `internal/constants/listing_draft_prompts.go`, `internal/controllers/listing_draft_controller.go` | Store listing draft endpoint | JSON-only listing copy prompt for title, description, and release notes. |
| `wemob-typescript-validation` | `internal/constants/ai_validation.go`, `internal/controllers/app_build_controller.go` | App file validation | JSON-only TypeScript/Next.js validation prompt used by `ValidateAppFiles`. |

Prompt fragments are fetched by name and label, compiled with Langfuse `{{variable}}` syntax where needed, and composed in Go. If Langfuse is disabled or a prompt fetch/compile fails, the runtime uses the local constants as fallbacks and logs a warning.

Trace metadata includes `langfuse.trace.metadata.prompt_parts`, a JSON list of prompt fragment names and versions used for the run. Generation observations also set the primary prompt name/version so Langfuse can group prompt performance.

## Deployment Validation Checklist

- Confirm `LANGFUSE_PUBLIC_KEY` and `LANGFUSE_SECRET_KEY` are present in Infisical for the target environment.
- Confirm no stale platform env var is overriding the intended Infisical value for any `LANGFUSE_*` key.
- If using self-hosted Langfuse or a regional cloud host, confirm either `LANGFUSE_BASE_URL` or `LANGFUSE_OTEL_TRACES_ENDPOINT` matches the target project.
- Confirm `LANGFUSE_PROMPT_LABEL` points to the intended label, usually `production`.
- Start the API and worker processes and check logs for `Langfuse OpenTelemetry exporter enabled`.
- Trigger one web builder run, one mobile builder run, one project classification/name generation path, one listing draft, and one TypeScript validation path in a non-production project.
- In Langfuse, verify traces include environment, user, organization, project, run/session identifiers, model, provider, token usage, latency, cost, and tool/generation observations where applicable.
- Verify traces include `prompt_parts` metadata and generation observations show prompt name/version.
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
