"""Product tools for Wemob Deep Agents.

Phase 1 intentionally exposes no product tools. Later phases add scoped tools
that read identity from `WemobContext` rather than model-supplied IDs.
"""

from __future__ import annotations

from typing import Any

from wemob_agent.context import WemobContext


def build_product_tools(context: WemobContext | None = None) -> list[Any]:
    _ = context
    return []
