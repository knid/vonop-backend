# PromptKit — Design Spec

**Tagline**: "Stop deploying to change a prompt."

**One-liner**: The prompt compiler for AI-first teams. Compose, version, test, and score your prompt systems — without code deploys.

**Model**: Open-source core (Supermemory model). SDK + composition engine open source. Managed cloud platform as the business. Free tier → paid scaling.

---

## Target Audience

AI-first product companies with 10-50+ prompts in production, complex conditional logic, and multiple project types. Teams that have outgrown flat prompt templates but are managing composition complexity in raw code (Go constants, Python strings, config files).

## Problem

Existing prompt management tools (Langfuse, Braintrust, Humanloop) treat prompts as flat documents with variable interpolation. Real-world prompt systems are programs — composable blocks, conditional assembly, multi-phase pipelines, project-type variants. No tool supports this.

## Core Insight

Prompts in production are not documents. They are programs — with shared blocks, conditional branches, A/B variants, and multi-step pipelines. PromptKit is the first tool to treat them that way.

---

## Core Data Model

### Five primitives

| Concept | Description |
|---------|-------------|
| **Block** | A reusable text fragment with version history, optional `{{variables}}`, and a description. The atomic unit. |
| **Composition** | A React Flow graph that assembles blocks. Contains Block nodes, IF nodes, Merge nodes, and a Start/Output pair. This is what the SDK returns. |
| **Context Schema** | User-defined parameters a composition accepts (e.g., `projectType: enum`, `hasAuth: boolean`, `maxFiles: number`). Fully configurable per composition. Drives IF node evaluation. |
| **Variant** | An alternative version of a block, used for A/B testing via Percentage IF nodes. |
| **Environment** | dev / staging / prod with promotion workflow. SDK fetches config for its environment. |

### Node types in the composition graph

| Node | Purpose | Visual |
|------|---------|--------|
| **Start** | Entry point. Receives context. | Green circle |
| **Block** | Appends a prompt fragment to the output. | Green rounded rectangle |
| **IF Boolean** | True/false branch. Include block or skip. | Purple with "IF" badge |
| **IF Switch** | Multi-branch on enum value. Each case → different block chain. | Purple with "SW" badge |
| **IF Percentage** | A/B split. Deterministic by userId/sessionId. Powers experiments. | Purple with "%%" badge |
| **IF Expression** | Custom JS expression for full power. | Purple with "EX" badge |
| **Merge** | Rejoins branches from IF nodes. | Small purple pill |
| **Output** | Terminal node. Returns assembled text + metadata. | Red circle |

### IF node parameters

Every IF node can evaluate against:

**User-defined context** (configured in Context Schema):
- Any key the user defines: `projectType`, `hasAuth`, `userTier`, `maxFiles`, custom values

**Auto-captured metadata** (always available, underscore-prefixed):
- `_req.ip`, `_req.country`, `_req.userId`, `_req.userAgent`
- `_time.hour`, `_time.dayOfWeek`, `_time.date`, `_time.timestamp`
- `_env.name`, `_sdk.version`, `_sdk.language`

---

## Platform Architecture

### Tech Stack

| Layer | Choice | Rationale |
|-------|--------|-----------|
| Frontend | Next.js 16 App Router + TypeScript + Tailwind CSS + shadcn/ui | Single full-stack codebase, fastest to ship |
| Flow editor | @xyflow/react (React Flow) | n8n-style IF blocks, zoomable canvas, minimap |
| Code editor | Monaco Editor | Block content editing, syntax highlighting |
| Charts | Recharts | Analytics and scoring dashboards |
| Database | PostgreSQL (Neon via Vercel Marketplace) | Relational, versioning, JSON support |
| Auth | Clerk (Vercel Marketplace) | Team-based, SSO |
| Real-time | SSE (Server-Sent Events) | Config push to SDKs |
| Hosting | Vercel | Native Next.js optimization |

### SDK Config Delivery (LaunchDarkly model)

```
App startup:
  SDK → GET /v1/config/{env} → full config payload
  SDK caches in memory

Background sync (every 30s, configurable):
  SDK → SSE /v1/stream/{env} → real-time config diffs
  SDK patches local cache

On pk.compose(name, context):
  1. Find composition in local cache
  2. Walk the flow graph (Start → blocks → IF nodes → merge → output)
  3. Evaluate IF conditions against context + auto-captured metadata
  4. Assemble text from resolved block chain
  5. Return result with metadata (text, version, variantId, tokenCount, blocks)

  All local, <1ms, zero network calls.
```

---

## UI/UX Design

### Aesthetic

- **Dark mode only** (zinc-950 background, zinc-900 cards, zinc-800 borders)
- **Purple accent** (#7c3aed) for brand and primary actions
- **Green** for success, prod environment, always-on blocks
- **Amber** for experiments, A/B badges, warnings
- **Geist Sans** for UI text, **Geist Mono** for code/tokens/scores
- **Keyboard-first**: Cmd+K command palette, Cmd+E search blocks, Cmd+P open composition
- **High information density**: every pixel earns its place
- Inspired by Linear, Vercel Dashboard, n8n

### Pages

| Page | Purpose |
|------|---------|
| **Dashboard** | Overview stats (compositions, blocks, assemblies/24h, avg score), active experiments with confidence levels, recent changes feed, composition quick-access grid |
| **Compositions** | List/grid of all compositions with block count, IF nodes, version, score, throughput |
| **Composition Editor** | React Flow canvas with the full n8n-style flow graph. Block nodes, IF nodes, Merge nodes. Side panel for block content editing (Monaco). Live preview panel showing assembled prompt for test context. Context schema editor. |
| **Blocks** | Library of all reusable blocks. Search, filter by tag. Click to edit content. Version history. Usage count (which compositions use this block). |
| **Pipelines** | Multi-composition pipeline view. Shows how compositions chain (decision → conversation → structure → builder → validation). Also React Flow. |
| **Experiments** | Active A/B tests. Each shows: variant names, traffic split, sample size, duration, scores per variant, p-value, confidence level, winner indicator. |
| **Scoring** | Evaluation results per composition. Score trends over time. Breakdown by auto-eval scorer. Custom scorer management. |
| **Analytics** | Cost tracking, token usage trends, assemblies over time, latency percentiles, usage by composition, usage by context value. |
| **Logs** | Individual assembly log. Each entry: timestamp, composition, resolved blocks, context, variant picked, scores. Searchable, filterable. |
| **Settings** | API keys, team members, environments, SDK setup guide, billing. |

### Composition Editor Detail

The editor is the core product experience. It has three areas:

1. **Canvas** (center, 70% width) — React Flow graph. Users drag nodes from a palette, connect them, configure IF conditions by clicking nodes. Zoomable, pannable, minimap in corner.

2. **Block Editor** (right panel, 30% width) — When a block node is selected, shows Monaco editor with the block's content. Syntax highlighting, token count, variable highlighting. Version history dropdown.

3. **Preview Bar** (bottom) — Collapsible panel showing the assembled prompt for the current test context. Context inputs (dropdowns/toggles for each schema param). Updates live as the user changes the flow or context values. Shows which blocks were included, which were skipped, total token count.

---

## SDK Design

### TypeScript (primary)

```typescript
import { PromptKit } from '@promptkit/sdk'

// Initialize once at startup
const pk = new PromptKit({
  apiKey: 'pk_live_...',
  environment: 'prod'
})

// Compose (local, <1ms)
const result = await pk.compose('builder', {
  projectType: 'ecommerce',
  hasAuth: true,
  outputMode: 'chat',
  _request: {
    ip: req.headers['x-forwarded-for'],
    userId: session.userId
  }
})

result.text       // assembled prompt string
result.version    // "v14"
result.variantId  // "design-philosophy:v15"
result.tokenCount // 14200
result.blocks     // ["role", "design-philosophy:v15", "framework-rules", ...]
result.id         // unique assembly ID for tracking

// Track output for auto-scoring (one line)
await pk.track(result.id, {
  input: userPrompt,
  output: llmResponse,
  model: 'claude-sonnet-4.6',
  latencyMs: 3200
})

// Optional: manual app-level metrics
await pk.score(result.id, {
  buildSuccess: true,
  errorCount: 0
})
```

### Python

```python
from promptkit import PromptKit

pk = PromptKit(api_key="pk_live_...", environment="prod")

result = pk.compose("builder", {
    "project_type": "ecommerce",
    "has_auth": True
})

pk.track(result.id, input=user_prompt, output=llm_response,
         model="claude-sonnet-4.6", latency_ms=3200)
```

### REST API (for any language)

```
POST /v1/compose
{
  "composition": "builder",
  "context": { "projectType": "ecommerce", "hasAuth": true }
}
→ { "id": "asm_xxx", "text": "...", "version": "v14", ... }

POST /v1/track
{
  "assemblyId": "asm_xxx",
  "input": "...",
  "output": "...",
  "model": "claude-sonnet-4.6"
}
```

---

## Scoring System

Three layers, from zero-effort to custom:

### Layer 1: Automatic (always on, zero config)

| Metric | Method | Cost |
|--------|--------|------|
| Assembly success | Did `pk.compose()` complete? | Free |
| Token efficiency | Total tokens in assembled prompt, trend tracking | Free |
| Assembly latency | Time in ms (target <1ms) | Free |
| Response received | Did the LLM respond or error? (from `pk.track()`) | Free |
| Schema compliance | Valid JSON output when expected? | Free |

### Layer 2: Auto-Eval (opt-in per composition, sample-based)

LLM-as-judge scorers that run automatically on a configurable % of tracked outputs:

| Scorer | What it measures | Method |
|--------|-----------------|--------|
| Instruction following | Did output follow the prompt's instructions? | LLM-as-judge with prompt as rubric |
| Output quality | Coherence, completeness, professionalism | G-Eval rubric |
| Factuality | Are claims grounded in context? | LLM-as-judge |
| Relevance | Is output relevant to input? | LLM-as-judge |
| Hallucination | Made-up information? | QAG scoring |
| Structured output | JSON validity, schema match | Deterministic (no LLM) |
| Toxicity / Safety | Safe and appropriate? | LLM-as-judge |

Users enable/disable scorers per composition. Sample rate configurable (default 10-20%).

### Layer 3: Custom Scorers (user-defined)

- **Code scorers** — JS function returning 0-10. Deterministic, free.
- **LLM-as-judge scorers** — Custom judging prompt with `{{input}}`/`{{output}}` variables.
- **Composite scorers** — Weighted combination of other scorers.

### Scoring → Experiments pipeline

Every score is tagged with composition version, active variant, and context. The Experiments dashboard automatically aggregates scores per variant and computes statistical significance (p-value, confidence interval). When confidence threshold is met, the platform suggests promoting the winner.

---

## A/B Testing (Experiments)

Experiments are Percentage IF nodes with scoring attached. No separate "experiment" concept.

1. User adds a Percentage IF node in the flow editor: `50% → block-v2, 50% → block-v1`
2. SDK deterministically assigns users to variants (hash of `_req.userId` or `_req.sessionId`)
3. Scores flow in via `pk.track()` → auto-eval scorers run
4. Platform computes statistical significance
5. Dashboard shows: winning variant, sample size, confidence level, score distributions
6. At configurable confidence threshold (default 95%), platform suggests promoting winner

---

## Versioning & Environments

- Every block edit creates an immutable version (v1, v2, v3...)
- Every composition graph save creates a composition version
- Environments: dev → staging → prod
- Promotion workflow: edit in dev, test in staging, promote to prod
- Rollback: one click to revert any composition to a previous version
- SDK fetches config for its configured environment only

---

## MVP Scope

### Phase 1: Visual Composer + SDK (launch)

- Composition editor with React Flow (Block, IF Boolean, IF Switch, Merge, Start/Output nodes)
- Block library with content editing (Monaco)
- Context Schema editor (user-defined parameters)
- TypeScript SDK with `pk.compose()` and LaunchDarkly-style sync
- Environments (dev/staging/prod) with promotion
- Version history and rollback
- Dashboard with composition list and basic stats
- REST API
- Clerk auth, team workspaces

### Phase 2: Scoring + Experiments

- `pk.track()` API for sending input/output pairs
- Layer 1 automatic metrics (always on)
- Layer 2 auto-eval scorers (LLM-as-judge, configurable per composition)
- Percentage IF nodes for A/B testing
- Experiments dashboard with statistical significance
- Score trends and analytics

### Phase 3: Advanced

- IF Expression nodes (custom JS expressions)
- Pipeline view (multi-composition chains)
- Python SDK
- Custom scorers (code + LLM-as-judge + composite)
- Cost tracking and token analytics
- Logs explorer
- Webhook integrations (Slack notifications for experiment results, score drops)
- CLI tool for importing existing prompts from code

### Phase 4: Scale

- Edge caching for SDK configs (Vercel Edge Config)
- Self-hosting option (enterprise)
- SSO/SAML
- Audit logs
- API rate limiting and usage tiers
- Public block registry (share blocks across teams)
