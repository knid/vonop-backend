# Wemob Agent Runtime

Deep Agents is the only agent runtime for Wemob. This service owns the agent
loop, checkpoints, memory, subagents, filesystem surface, sandbox execution,
human interrupts, and the native LangGraph / Deep Agents stream.

Wemob backend remains the product shell around this runtime:

- auth, orgs, users, and projects
- billing policy and credit budgets
- project metadata, assets, deployments, and product APIs
- runtime thread/run creation and access authorization

The runtime stream must stay Deep Agents-native. Do not introduce a Wemob stream
protocol, `UIMessagePart` bridge, or event mapper here.

## Local Development

```bash
cd agent-runtime
python -m venv .venv
. .venv/bin/activate
pip install -e ".[dev]"
langgraph dev
```

The graph is exported from `wemob_agent.agent:create_agent` in `langgraph.json`.

## Current Phase

This is the Phase 1 skeleton. It intentionally contains only:

- runtime configuration
- typed Wemob runtime context
- supervisor and subagent prompts
- a minimal Deep Agents graph factory

Product tools, Daytona-backed workspace execution, deterministic Git
finalization, and frontend integration are added in later phases.
