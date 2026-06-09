from __future__ import annotations

import asyncio
import json
import logging
import urllib.error
import urllib.request
from typing import Any, Mapping

from .schemas import RunStreamRequest

logger = logging.getLogger("deepagents.tool_bridge")


class ToolBridgeError(RuntimeError):
    """Raised when the Go tool endpoint returns a non-2xx HTTP status or the
    JSON body cannot be parsed. Callers decide whether to retry (network /
    5xx) or fail fast (4xx, validation)."""

    def __init__(self, message: str, *, status: int | None = None, transient: bool = False) -> None:
        super().__init__(message)
        self.status = status
        self.transient = transient


class GoToolClient:
    """HTTP client for the Go /internal/v1/agent-runs/{run_id}/tools/{name}/execute
    endpoint. The deepagents-runtime constructs one GoToolClient per run and
    threads it through every make_wemob_tool closure."""

    def __init__(self, base_url: str, token: str, *, timeout_s: float = 60.0) -> None:
        self.base_url = (base_url or "").rstrip("/")
        self.token = (token or "").strip()
        self.timeout_s = float(timeout_s) if timeout_s and timeout_s > 0 else 60.0

    async def execute_tool(
        self,
        run_id: str,
        tool_call_id: str,
        tool_name: str,
        arguments: Mapping[str, Any],
        run_context: RunStreamRequest,
    ) -> dict[str, Any]:
        if not self.base_url:
            raise ToolBridgeError("go tool client: GO_TOOL_BASE_URL is not configured", transient=False)

        url = f"{self.base_url}/internal/v1/agent-runs/{run_id}/tools/{tool_name}/execute"
        body = {
            "job_id": run_context.job_id,
            "run_id": run_id,
            "project_id": run_context.project_id,
            "user_id": run_context.user_id,
            "tool_call_id": tool_call_id,
            "arguments": dict(arguments or {}),
        }
        data = json.dumps(body).encode("utf-8")

        attempts = 0
        last_exc: Exception | None = None
        # Only retry on transient network/5xx errors. Non-idempotent
        # tool calls (provision_database, trigger_build_after_fix, ...)
        # must not be retried automatically.
        while attempts < 2:
            attempts += 1
            try:
                return await asyncio.to_thread(self._post_once, url, data)
            except ToolBridgeError as exc:
                last_exc = exc
                if not exc.transient or attempts >= 2:
                    raise
                backoff = 0.25 * attempts
                logger.warning(
                    "go tool bridge transient failure, retrying",
                    extra={"tool": tool_name, "status": exc.status, "backoff_s": backoff},
                )
                await asyncio.sleep(backoff)
        if last_exc:
            raise last_exc
        raise ToolBridgeError("go tool client: no attempts made", transient=False)

    def _post_once(self, url: str, data: bytes) -> dict[str, Any]:
        req = urllib.request.Request(
            url,
            data=data,
            method="POST",
            headers={
                "Content-Type": "application/json",
                "Accept": "application/json",
                "Authorization": f"Bearer {self.token}" if self.token else "",
            },
        )
        try:
            with urllib.request.urlopen(req, timeout=self.timeout_s) as resp:
                raw = resp.read().decode("utf-8")
                return json.loads(raw) if raw else {}
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="ignore")
            transient = exc.code >= 500 or exc.code in (408, 425, 429)
            raise ToolBridgeError(
                f"go tool bridge HTTP {exc.code}: {detail or exc.reason}",
                status=exc.code,
                transient=transient,
            ) from exc
        except (urllib.error.URLError, TimeoutError, ConnectionError) as exc:
            raise ToolBridgeError(
                f"go tool bridge network error: {exc}",
                status=None,
                transient=True,
            ) from exc


# Result truncation cap. Go enforces a server-side cap too, but truncating
# before crossing the network keeps the model context window predictable.
MAX_TOOL_RESULT_CHARS = 16_000


def truncate_tool_result(value: Any, *, max_chars: int = MAX_TOOL_RESULT_CHARS) -> str:
    """Render a tool result to a string and truncate to a model-safe cap.

    The LLM should never see an unbounded blob returned from a sandbox tool.
    Keep the head of the result so the model has the meaningful prefix.
    """
    if value is None:
        return ""
    if isinstance(value, str):
        text = value
    elif isinstance(value, (dict, list)):
        try:
            text = json.dumps(value, ensure_ascii=False, indent=2)
        except (TypeError, ValueError):
            text = str(value)
    else:
        text = str(value)

    if max_chars <= 0 or len(text) <= max_chars:
        return text
    return text[:max_chars] + "\n... (truncated)"
