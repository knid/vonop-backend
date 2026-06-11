"""Declarative subagent specs.

The exact runtime type is provided by Deep Agents. Keeping these as plain dicts
matches the public docs and avoids adding a Wemob-specific subagent wrapper.
"""

from __future__ import annotations

from typing import Any

from wemob_agent.context import WemobContext


def build_subagents(context: WemobContext | None = None) -> list[dict[str, Any]]:
    _ = context
    return [
        {
            "name": "product-researcher",
            "description": "Research product, market, competitor, content, and reference material.",
            "prompt": "Return compact research briefs with citations when web sources are used.",
        },
        {
            "name": "design-director",
            "description": "Define visual direction, design systems, and UI quality constraints.",
            "prompt": "Create or update design guidance before substantial UI implementation.",
        },
        {
            "name": "frontend-builder",
            "description": "Implement web UI, pages, components, and frontend states in /workspace.",
            "prompt": "Use project files and skills. Verify responsive behavior and build constraints.",
        },
        {
            "name": "mobile-builder",
            "description": "Implement Expo Router and React Native mobile application work.",
            "prompt": "Keep mobile source under the project mobile workspace conventions.",
        },
        {
            "name": "backend-builder",
            "description": "Implement project API routes, auth integrations, AI gateway, and services.",
            "prompt": "Do not access Wemob product internals directly. Use scoped product tools.",
        },
        {
            "name": "database-agent",
            "description": "Design and inspect project database schemas and migrations.",
            "prompt": "Only operate on the project database through scoped tools and approvals.",
        },
        {
            "name": "qa-verifier",
            "description": "Run verification, build checks, and regression inspection.",
            "prompt": "Return structured pass/fail verdicts with blocking issues and warnings.",
        },
        {
            "name": "release-agent",
            "description": "Handle deploy, mobile build, preview, and release diagnostics.",
            "prompt": "Use finalized source state for deploy and keep logs compact.",
        },
    ]
