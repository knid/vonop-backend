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

## Runtime Surfaces

The container exposes two HTTP surfaces:

- `:2024` runs `langgraph dev` and is the native LangGraph / Deep Agents API.
  The frontend should connect here directly with `@langchain/react`.
- `:2025` runs `wemob_agent.internal_api:app`, a small internal control API for
  the Go backend. It creates, reads, cancels, and resumes runs by calling the
  LangGraph API. It does not consume or transform stream events.

Backend configuration should point `AGENT_RUNTIME_URL` at the internal control
API, for example `http://agent-runtime:2025`. Set `AGENT_RUNTIME_API_URL` to
the frontend-reachable LangGraph API, for example `https://agents.example.com`.

## Local Development

```bash
cd agent-runtime
python -m venv .venv
. .venv/bin/activate
pip install -e ".[dev]"
langgraph dev --host 0.0.0.0 --port 2024
uvicorn wemob_agent.internal_api:app --host 0.0.0.0 --port 2025
```

The graph is exported from `wemob_agent.agent:create_agent` in `langgraph.json`.
