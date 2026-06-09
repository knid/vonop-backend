from __future__ import annotations

import logging
import time
import uuid
from typing import Any, Awaitable, Callable, Mapping

from .schemas import RunStreamRequest
from .streaming import ndjson_event
from .tool_bridge import GoToolClient, MAX_TOOL_RESULT_CHARS, ToolBridgeError, truncate_tool_result

logger = logging.getLogger("deepagents.wemob_tools")

# WemobTool is the shape deepagents expects: a callable that takes
# arbitrary kwargs and returns a string. We emit tool_call_start and
# tool_call_result events into the run's NDJSON stream from inside the
# wrapper so the timeline stays consistent.
WemobTool = Callable[..., Awaitable[str]]


def make_wemob_tool(
    spec: Mapping[str, Any],
    *,
    run_context: RunStreamRequest,
    client: GoToolClient,
    emit: Callable[[bytes], None] | None = None,
) -> WemobTool:
    """Construct one Python tool per Wemob ToolSpec.

    The returned callable is what the deepagents model sees as a first-class
    tool: its name, description, and JSON-schema arguments come from the spec.
    Internally it POSTs to the Go /internal/v1/agent-runs/{run_id}/tools/
    {name}/execute endpoint, which validates, runs the Wemob-side
    ToolExecutor, and returns the same shape the native Go loop would have
    produced.

    This factory must NOT collapse to a single generic `wemob_tool` wrapper.
    The plan (§5.1) is explicit: the LLM sees one named function per
    manifest item so it can pick the right one without parsing JSON.
    """
    if not spec:
        raise ValueError("make_wemob_tool: spec is required")
    name = str(spec.get("name") or "").strip()
    if not name:
        raise ValueError("make_wemob_tool: spec.name is required")

    description = str(spec.get("description") or f"Call the {name} Wemob tool.")
    schema = spec.get("schema") or {"type": "object", "properties": {}}
    risk_level = str(spec.get("risk_level") or "")

    tool_call_id = uuid.uuid4().hex

    async def _impl(**kwargs: Any) -> str:
        started = time.monotonic()
        call_id = f"{tool_call_id}-{uuid.uuid4().hex[:8]}"
        # Emit start so the SSE client gets a tool_call_start frame with the
        # python-generated id. The Go bridge does not emit its own
        # tool_call_start so there is no double-emit.
        if emit is not None:
            emit(
                ndjson_event(
                    "tool_call_start",
                    run_id=run_context.run_id,
                    seq=0,
                    job_id=run_context.job_id,
                    data={
                        "tool_call_id": call_id,
                        "tool_name": name,
                        "input_preview": _coerce_preview(kwargs),
                        "risk_level": risk_level,
                    },
                )
            )

        is_error = False
        result_text = ""
        try:
            response = await client.execute_tool(
                run_id=run_context.run_id,
                tool_call_id=call_id,
                tool_name=name,
                arguments=kwargs,
                run_context=run_context,
            )
            result_text = truncate_tool_result(response.get("result", ""), max_chars=MAX_TOOL_RESULT_CHARS)
            is_error = bool(response.get("is_error", False))
            duration_ms = int(response.get("duration_ms") or ((time.monotonic() - started) * 1000))
        except ToolBridgeError as exc:
            is_error = True
            result_text = truncate_tool_result(f"tool bridge error: {exc}")
            duration_ms = int((time.monotonic() - started) * 1000)
            logger.warning(
                "wemob tool bridge call failed",
                extra={"tool": name, "transient": exc.transient, "status": exc.status},
            )
        except Exception as exc:  # last-resort guard so the model gets a string
            is_error = True
            result_text = truncate_tool_result(f"unexpected tool error: {exc}")
            duration_ms = int((time.monotonic() - started) * 1000)
            logger.exception("wemob tool unexpected failure", extra={"tool": name})

        if emit is not None:
            emit(
                ndjson_event(
                    "tool_call_result",
                    run_id=run_context.run_id,
                    seq=0,
                    job_id=run_context.job_id,
                    data={
                        "tool_call_id": call_id,
                        "tool_name": name,
                        "status": "ok" if not is_error else "error",
                        "summary": _first_line(result_text),
                        "is_error": is_error,
                        "duration_ms": duration_ms,
                    },
                )
            )

        return result_text

    # Stash the metadata on the function so deepagents (or any consumer) can
    # recover the spec without re-passing it. `_wemob_tool_spec` is a
    # private attribute; downstream code should use the spec the runtime
    # provided.
    setattr(_impl, "_wemob_tool_spec", dict(spec))
    setattr(_impl, "__name__", name)
    setattr(_impl, "__doc__", description)
    _impl.tool_schema = schema  # type: ignore[attr-defined]
    return _impl


def _coerce_preview(value: Any) -> dict[str, Any]:
    if value is None:
        return {}
    if isinstance(value, Mapping):
        return {str(k): _preview_value(v) for k, v in value.items()}
    return {"value": _preview_value(value)}


def _preview_value(value: Any) -> Any:
    if isinstance(value, (str, int, float, bool)) or value is None:
        return value
    if isinstance(value, (list, dict)):
        try:
            rendered = json.dumps(value, ensure_ascii=False)
        except (TypeError, ValueError):
            rendered = str(value)
        if len(rendered) > 1024:
            return rendered[:1024] + "...(truncated)"
        return rendered
    return str(value)


def _first_line(value: str) -> str:
    if not value:
        return ""
    for i, ch in enumerate(value):
        if ch in "\r\n":
            return value[:i].strip()
    return value.strip()


def build_wemob_tools(
    specs: list[Mapping[str, Any]] | None,
    *,
    run_context: RunStreamRequest,
    client: GoToolClient,
    emit: Callable[[bytes], None] | None = None,
) -> list[WemobTool]:
    """Construct one WemobTool per spec. Empty list when specs is nil/empty.

    The list is in spec order so the model sees the same registration order
    the Go side does.
    """
    out: list[WemobTool] = []
    for spec in specs or ():
        try:
            out.append(make_wemob_tool(spec, run_context=run_context, client=client, emit=emit))
        except ValueError as exc:
            logger.warning("skipping invalid wemob tool spec: %s", exc)
    return out
