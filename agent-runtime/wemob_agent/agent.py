"""LangGraph entrypoint for the Wemob Deep Agents runtime."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from wemob_agent.config import load_config
from wemob_agent.context import WemobContext
from wemob_agent.subagents import build_subagents
from wemob_agent.tools import build_product_tools


def _load_supervisor_prompt() -> str:
    return (Path(__file__).parent / "prompts" / "supervisor.md").read_text(encoding="utf-8")


def _context_from_config(config: Any) -> WemobContext | None:
    if not isinstance(config, dict):
        return None
    configurable = config.get("configurable") or {}
    raw = configurable.get("wemob_context") or configurable.get("context")
    if isinstance(raw, WemobContext):
        return raw
    if isinstance(raw, dict):
        try:
            return WemobContext(**raw)
        except TypeError:
            return None
    return None


def create_agent(config: Any = None) -> Any:
    """Create the Wemob Deep Agent graph.

    This function is intentionally small. Deep Agents owns the runtime and
    stream contract; Wemob only supplies product context, tools, and prompts.
    """

    from deepagents import create_deep_agent

    runtime_config = load_config()
    context = _context_from_config(config)

    return create_deep_agent(
        model=runtime_config.default_model,
        system_prompt=_load_supervisor_prompt(),
        tools=build_product_tools(context),
        subagents=build_subagents(context),
        context_schema=WemobContext,
    )
