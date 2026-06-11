"""Product tools for Wemob Deep Agents.

Tools read product identity and filesystem roots from `WemobContext`, not from
model-supplied IDs. Deep Agents still owns the graph, subagent, and stream
contract.
"""

from __future__ import annotations

from collections.abc import Callable
from typing import Any

from wemob_agent.backends import build_backend
from wemob_agent.context import WemobContext
from wemob_agent.daytona import DaytonaWorkspaceManager, PreviewServerRequest


def build_product_tools(context: WemobContext | None = None) -> list[Any]:
    def active_context() -> WemobContext | None:
        if context is not None:
            return context
        try:
            from langgraph.config import get_runtime
        except Exception:
            return None
        try:
            runtime = get_runtime()
        except Exception:
            return None
        raw = getattr(runtime, "context", None)
        if isinstance(raw, WemobContext):
            return raw
        if isinstance(raw, dict):
            try:
                return WemobContext(**raw)
            except TypeError:
                return None
        return raw if isinstance(raw, WemobContext) else None

    def active_backend():
        return build_backend(active_context())

    def read_workspace_file(path: str) -> str:
        """Read a UTF-8 text file from the current project workspace."""

        return active_backend().workspace.read_text(path)

    def write_workspace_file(path: str, content: str) -> str:
        """Write a UTF-8 text file inside the current project workspace."""

        return active_backend().workspace.write_text(path, content)

    def list_workspace_files() -> list[str]:
        """List files in the current project workspace."""

        return active_backend().workspace.list_files()

    def update_project_memory(key: str, content: str) -> str:
        """Write persistent project memory under /memories/project."""

        return active_backend().store.update_project_memory(key, content)

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
        raise RuntimeError(
            "list_project_assets is not wired to a Wemob product API yet; "
            f"project_id={ctx.project_id}"
        )

    def generate_image(prompt: str, width: int = 1024, height: int = 1024, style: str = "") -> dict[str, Any]:
        """Request image generation for the current project."""

        ctx = active_context()
        if ctx is None:
            raise ValueError("generate_image requires WemobContext")
        raise RuntimeError(
            "generate_image is not wired to a Wemob product API yet; "
            f"project_id={ctx.project_id}, size={width}x{height}, style={style}, prompt={prompt[:80]}"
        )

    def create_artifact(name: str, content: str, content_type: str = "text/plain") -> dict[str, str]:
        """Create a text artifact outside model context under /artifacts."""

        return active_backend().artifacts.create_text_artifact(name, content, content_type)

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
        manager = DaytonaWorkspaceManager(ctx)
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
        _named_tool(read_workspace_file, "read_workspace_file"),
        _named_tool(write_workspace_file, "write_workspace_file"),
        _named_tool(list_workspace_files, "list_workspace_files"),
        _named_tool(update_project_memory, "update_project_memory"),
        _named_tool(get_project_context, "get_project_context"),
        _named_tool(list_project_assets, "list_project_assets"),
        _named_tool(generate_image, "generate_image"),
        _named_tool(create_artifact, "create_artifact"),
        _named_tool(start_preview_server, "start_preview_server"),
    ]


def _named_tool(tool: Callable[..., Any], name: str) -> Callable[..., Any]:
    tool.__name__ = name
    return tool
