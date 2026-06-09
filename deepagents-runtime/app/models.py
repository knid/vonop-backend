"""Compatibility module for future deepagents runtime assets.

The runtime scaffold keeps the model factory and backend builders isolated from
request handling so future PRs can plug the real engine without touching FastAPI
endpoint wiring.
"""

from .runtime import build_model, build_workspace_backend

__all__ = ["build_model", "build_workspace_backend"]
