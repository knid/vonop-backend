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

    return [
        _named_tool(read_workspace_file, "read_workspace_file"),
        _named_tool(write_workspace_file, "write_workspace_file"),
        _named_tool(list_workspace_files, "list_workspace_files"),
        _named_tool(update_project_memory, "update_project_memory"),
    ]


def _named_tool(tool: Callable[..., Any], name: str) -> Callable[..., Any]:
    tool.__name__ = name
    return tool
