# Deep Agents Production Runtime

This architecture follows the implementation plan in
`docs/plans/wemob_deepagents_daytona_production_implementation_plan_updated.md`.

## Boundary

Deep Agents owns:

- agent loop and planning
- subagents
- checkpoints and resumability
- long-term memory
- virtual filesystem and context compression
- sandbox execution
- human interrupts and approvals
- native LangGraph / Deep Agents streaming

Wemob owns:

- authentication and authorization
- users, organizations, projects, and billing policy
- project metadata, assets, deployments, and product APIs
- runtime thread/run creation and frontend access authorization
- Git finalization policy and product status records

## Streaming

Wemob must not introduce a custom stream protocol. The frontend uses the
LangChain frontend SDK directly:

```ts
import { useStream } from "@langchain/react";
```

The UI renders Deep Agents projections such as `stream.messages`,
`stream.subagents`, `stream.values`, tool-call state, and interrupts. If Go
proxies the runtime for auth or same-origin reasons, the proxy must preserve
the Deep Agents / LangGraph stream payload without semantic transformation.

## Runtime Package

The Python runtime lives in `agent-runtime/`.

Current Phase 1 contents:

- `langgraph.json`
- `pyproject.toml`
- Dockerfile
- `wemob_agent.agent:create_agent`
- typed `WemobContext`
- supervisor prompt
- initial subagent specs

Later phases add Daytona workspace execution, product tools, deterministic Git
finalization, frontend integration, and removal of the old Go agent runtime.
