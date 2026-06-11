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
    backend = build_backend(context)

    def read_workspace_file(path: str) -> str:
        """Read a UTF-8 text file from the current project workspace."""

        return backend.workspace.read_text(path)

    def write_workspace_file(path: str, content: str) -> str:
        """Write a UTF-8 text file inside the current project workspace."""

        return backend.workspace.write_text(path, content)

    def list_workspace_files() -> list[str]:
        """List files in the current project workspace."""

        return backend.workspace.list_files()

    def update_project_memory(key: str, content: str) -> str:
        """Write persistent project memory under /memories/project."""

        return backend.store.update_project_memory(key, content)

    def start_preview_server(
        framework: str,
        command: str,
        port: int,
        cwd: str = "/workspace",
        ttl_seconds: int | None = None,
    ) -> dict[str, Any]:
        """Start a project preview server and return the Wemob public preview URL."""

        if context is None:
            raise ValueError("start_preview_server requires WemobContext")
        manager = DaytonaWorkspaceManager(context)
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
        _named_tool(start_preview_server, "start_preview_server"),
    ]


def _named_tool(tool: Callable[..., Any], name: str) -> Callable[..., Any]:
    tool.__name__ = name
    return tool
