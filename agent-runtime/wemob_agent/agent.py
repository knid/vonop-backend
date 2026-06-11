"""LangGraph entrypoint for the Wemob Deep Agents runtime."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from wemob_agent.config import load_config
from wemob_agent.context import WemobContext
from wemob_agent.daytona import create_runtime_sandbox
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


def _ctx_value(context: Any, key: str, default: str) -> str:
    if context is None:
        return default
    if isinstance(context, dict):
        value = context.get(key)
    else:
        value = getattr(context, key, None)
    return str(value or default)


def _build_deepagents_backend() -> Any:
    """Build the Deep Agents filesystem backend from per-run runtime context."""

    from deepagents.backends import CompositeBackend, FilesystemBackend, StateBackend, StoreBackend

    def backend_factory(runtime: Any) -> Any:
        context = getattr(runtime, "context", None)
        runtime_config = load_config()
        if runtime_config.allow_local_agent_execution:
            workspace_backend: Any = FilesystemBackend(
                root_dir=_ctx_value(context, "host_worktree_path", _ctx_value(context, "workspace_root", "/workspace")),
                virtual_mode=True,
            )
        else:
            from langchain_daytona import DaytonaSandbox

            if isinstance(context, dict):
                wemob_context = WemobContext(**context)
            elif isinstance(context, WemobContext):
                wemob_context = context
            else:
                raise ValueError("WemobContext is required to create a Daytona-backed workspace")
            workspace_backend = DaytonaSandbox(
                sandbox=create_runtime_sandbox(wemob_context, runtime_config).sandbox
            )
        memory_backend: Any
        artifact_backend: Any
        if runtime_config.allow_local_agent_execution:
            memory_backend = FilesystemBackend(
                root_dir=_ctx_value(context, "memory_root", "/memories"),
                virtual_mode=True,
            )
            artifact_backend = FilesystemBackend(
                root_dir=_ctx_value(context, "artifacts_root", "/artifacts"),
                virtual_mode=True,
            )
        else:
            memory_backend = StoreBackend(
                namespace=lambda rt: (
                    "wemob",
                    _ctx_value(getattr(rt, "context", None), "org_id", "unknown-org"),
                    _ctx_value(getattr(rt, "context", None), "project_id", "unknown-project"),
                    "memories",
                )
            )
            artifact_backend = StateBackend()
        return CompositeBackend(
            default=StateBackend(),
            routes={
                "/workspace/": workspace_backend,
                "/memories/": memory_backend,
                "/artifacts/": artifact_backend,
            },
        )

    return backend_factory


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
        backend=_build_deepagents_backend(),
        context_schema=WemobContext,
    )
