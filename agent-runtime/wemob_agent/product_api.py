"""Scoped Wemob product API client for Deep Agents tools."""

from __future__ import annotations

import os
from typing import Any

from wemob_agent.config import load_config
from wemob_agent.context import WemobContext


class ProductAPIError(RuntimeError):
    pass


class ProductAPIClient:
    def __init__(self, base_url: str | None = None, token: str | None = None) -> None:
        cfg = load_config()
        self.base_url = (base_url or cfg.backend_internal_url).rstrip("/")
        self.token = token if token is not None else cfg.internal_token
        if not self.base_url:
            raise ProductAPIError("WEMOB_BACKEND_INTERNAL_URL is required for product tools")
        if not self.token:
            raise ProductAPIError("AGENT_RUNTIME_INTERNAL_TOKEN is required for product tools")

    def list_project_assets(self, ctx: WemobContext) -> dict[str, Any]:
        return self._request("GET", f"/internal/deepagents/runs/{ctx.agent_runtime_run_id}/assets")

    def generate_image(
        self,
        ctx: WemobContext,
        prompt: str,
        width: int = 1024,
        height: int = 1024,
        style: str = "",
    ) -> dict[str, Any]:
        return self._request(
            "POST",
            f"/internal/deepagents/runs/{ctx.agent_runtime_run_id}/images",
            json={
                "prompt": prompt,
                "width": width,
                "height": height,
                "style": style,
            },
            timeout=float(os.getenv("WEMOB_PRODUCT_TOOL_TIMEOUT_SECONDS", "180")),
        )

    def _request(
        self,
        method: str,
        path: str,
        *,
        json: dict[str, Any] | None = None,
        timeout: float = 30.0,
    ) -> dict[str, Any]:
        try:
            import httpx
        except ModuleNotFoundError as exc:  # pragma: no cover - production dependency
            raise ProductAPIError("httpx is required for Wemob product tools") from exc
        try:
            with httpx.Client(base_url=self.base_url, timeout=timeout) as client:
                response = client.request(
                    method,
                    path,
                    headers={"X-Internal-Token": self.token},
                    json=json,
                )
        except httpx.HTTPError as exc:
            raise ProductAPIError(f"Wemob product API request failed: {exc}") from exc
        if response.status_code < 200 or response.status_code >= 300:
            raise ProductAPIError(
                f"Wemob product API returned {response.status_code}: {response.text[:500]}"
            )
        data = response.json() if response.content else {}
        if isinstance(data, dict):
            return data
        return {"data": data}
