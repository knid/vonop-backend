"""Product tools for Wemob Deep Agents.

Tools read product identity and filesystem roots from `WemobContext`, not from
model-supplied IDs. Deep Agents still owns the graph, subagent, and stream
contract.
"""

from __future__ import annotations

from collections.abc import Callable
from typing import Any

from wemob_agent.context import WemobContext
from wemob_agent.daytona import (
    DaytonaWorkspaceManager,
    PreviewServerRequest,
    connect_runtime_sandbox,
)
from wemob_agent.product_api import ProductAPIClient


def runtime_context() -> WemobContext | None:
    try:
        from langgraph.runtime import get_runtime
    except Exception:
        try:
            from langgraph.config import get_runtime
        except Exception:
            return None
    try:
        runtime = get_runtime()
    except Exception:
        return None
    raw = getattr(runtime, "context", None)
    return coerce_context(raw)


def coerce_context(raw: Any) -> WemobContext | None:
    if isinstance(raw, WemobContext):
        return raw
    if isinstance(raw, dict):
        try:
            return WemobContext(**raw)
        except TypeError:
            return None
    return None


def build_product_tools(context: WemobContext | None = None) -> list[Any]:
    def active_context() -> WemobContext | None:
        if context is not None:
            return context
        return runtime_context()

    def product_api() -> ProductAPIClient:
        return ProductAPIClient()

    def get_project_context() -> dict[str, Any]:
        """Return scoped Wemob project context for this run."""

        ctx = active_context()
        if ctx is None:
            raise ValueError("get_project_context requires WemobContext")
        return {
            "user_id": ctx.user_id,
            "org_id": ctx.org_id,
            "project_id": ctx.project_id,
            "job_id": ctx.job_id,
            "thread_id": ctx.thread_id,
            "mode": ctx.mode,
            "project_type": ctx.project_type,
            "frontend_runtime": ctx.frontend_runtime,
            "base_commit": ctx.base_commit,
            "plan_name": ctx.plan_name,
            "workspace_root": ctx.workspace_root,
            "memory_root": ctx.memory_root,
            "artifacts_root": ctx.artifacts_root,
        }

    def list_project_assets() -> dict[str, Any]:
        """List product assets available to this project."""

        ctx = active_context()
        if ctx is None:
            raise ValueError("list_project_assets requires WemobContext")
        return product_api().list_project_assets(ctx)

    def generate_image(
        prompt: str,
        width: int = 1024,
        height: int = 1024,
        style: str = "",
    ) -> dict[str, Any]:
        """Request image generation for the current project."""

        ctx = active_context()
        if ctx is None:
            raise ValueError("generate_image requires WemobContext")
        return product_api().generate_image(ctx, prompt, width, height, style)

    def start_preview_server(
        framework: str,
        command: str,
        port: int,
        cwd: str = "/workspace",
        ttl_seconds: int | None = None,
    ) -> dict[str, Any]:
        """Start a project preview server and return the Wemob public preview URL."""

        ctx = active_context()
        if ctx is None:
            raise ValueError("start_preview_server requires WemobContext")
        if not ctx.sandbox_id:
            raise RuntimeError("start_preview_server requires an active Daytona sandbox")
        manager = DaytonaWorkspaceManager(
            ctx,
            sandbox=connect_runtime_sandbox(ctx.sandbox_id).provider,
        )
        result = manager.start_preview_server(
            PreviewServerRequest(
                framework=framework,
                command=command,
                port=port,
                cwd=cwd,
                ttl_seconds=ttl_seconds,
            )
        )
        return {
            "preview_slug": result.preview_slug,
            "public_preview_url": result.public_preview_url,
            "sandbox_id": result.sandbox_id,
            "port": result.port,
            "command": result.command,
            "cwd": result.cwd,
        }

    return [
        _named_tool(get_project_context, "get_project_context"),
        _named_tool(list_project_assets, "list_project_assets"),
        _named_tool(generate_image, "generate_image"),
        _named_tool(start_preview_server, "start_preview_server"),
    ]


def _named_tool(tool: Callable[..., Any], name: str) -> Callable[..., Any]:
    tool.__name__ = name
    return tool
