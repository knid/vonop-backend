"""Tests for PR 8 tool bridge: GoToolClient, make_wemob_tool, build_wemob_tools,
truncate_tool_result. These do not require fastapi or deepagents to be
installed, so they can run in a minimal env (pydantic + pytest)."""

from __future__ import annotations

import asyncio
import json

from app.schemas import RunStreamRequest
from app.tool_bridge import (
    GoToolClient,
    MAX_TOOL_RESULT_CHARS,
    ToolBridgeError,
    truncate_tool_result,
)
from app.wemob_tools import build_wemob_tools, make_wemob_tool


def _make_request(run_id: str = "run-1", project_id: str = "p-1") -> RunStreamRequest:
    return RunStreamRequest(
        schema_version="2026-06-09",
        job_id="job-1",
        run_id=run_id,
        trace_id="trace-1",
        user_id="user-1",
        organization_id="org-1",
        project_id=project_id,
        project_type="app",
        frontend_runtime="nextjs",
        is_first_generation=True,
        message="x",
        repo={
            "mount_base": "/mnt/wemob-repos",
            "project_id": project_id,
            "virtual_root": "/workspace",
        },
        model={"provider": "anthropic", "model": "test-model"},
    )


def test_truncate_tool_result_caps_long_output() -> None:
    long = "x" * 50_000
    out = truncate_tool_result(long, max_chars=100)
    assert len(out) <= 200
    assert out.startswith("x" * 100)
    assert "truncated" in out


def test_truncate_tool_result_handles_dict_input() -> None:
    payload = {"files": ["a.tsx", "b.tsx"], "ok": True}
    text = truncate_tool_result(payload, max_chars=10_000)
    assert "a.tsx" in text
    assert "ok" in text


def test_truncate_tool_result_passthrough_when_short() -> None:
    assert truncate_tool_result("hi", max_chars=10) == "hi"
    assert truncate_tool_result("", max_chars=10) == ""


def test_make_wemob_tool_emits_start_and_result_events() -> None:
    captured: list[dict] = []

    def _emit(raw: bytes) -> None:
        captured.append(json.loads(raw.decode("utf-8")))

    request = _make_request()

    class _StubClient(GoToolClient):
        def __init__(self) -> None:  # type: ignore[no-super-call]
            pass

        async def execute_tool(self, **kwargs):  # type: ignore[override]
            return {
                "result": "ok: 12 lines",
                "is_error": False,
                "summary": "ok: 12 lines",
                "duration_ms": 7,
            }

    tool = make_wemob_tool(
        {
            "name": "sandbox_status",
            "description": "Return sandbox health.",
            "schema": {"type": "object", "properties": {}},
            "risk_level": "low",
        },
        run_context=request,
        client=_StubClient(),
        emit=_emit,
    )
    result = asyncio.run(tool())
    assert "ok: 12 lines" in result

    types = [c.get("type") for c in captured]
    assert "tool_call_start" in types
    assert "tool_call_result" in types
    start = next(c for c in captured if c["type"] == "tool_call_start")
    result_evt = next(c for c in captured if c["type"] == "tool_call_result")
    assert start["data"]["tool_name"] == "sandbox_status"
    assert result_evt["data"]["is_error"] is False
    assert result_evt["data"]["tool_call_id"] == start["data"]["tool_call_id"]


def test_make_wemob_tool_handles_bridge_error() -> None:
    captured: list[dict] = []

    def _emit(raw: bytes) -> None:
        captured.append(json.loads(raw.decode("utf-8")))

    request = _make_request()

    class _BoomClient(GoToolClient):
        def __init__(self) -> None:  # type: ignore[no-super-call]
            pass

        async def execute_tool(self, **kwargs):  # type: ignore[override]
            raise ToolBridgeError("simulated failure", status=500, transient=True)

    tool = make_wemob_tool(
        {"name": "sandbox_status", "description": "x", "schema": {}, "risk_level": "low"},
        run_context=request,
        client=_BoomClient(),
        emit=_emit,
    )
    result = asyncio.run(tool())
    assert "simulated failure" in result
    types = [c.get("type") for c in captured]
    assert "tool_call_result" in types
    result_evt = next(c for c in captured if c["type"] == "tool_call_result")
    assert result_evt["data"]["is_error"] is True


def test_build_wemob_tools_skips_invalid_specs() -> None:
    request = _make_request()
    tools = build_wemob_tools(
        [
            {"name": "sandbox_status", "description": "x", "schema": {}, "risk_level": "low"},
            {"description": "missing name"},
            {"name": "sandbox_read_logs", "description": "y", "schema": {}, "risk_level": "low"},
        ],
        run_context=request,
        client=GoToolClient(base_url="", token=""),
    )
    assert [getattr(t, "__name__", "") for t in tools] == [
        "sandbox_status",
        "sandbox_read_logs",
    ]


def test_build_wemob_tools_empty_specs_returns_empty() -> None:
    request = _make_request()
    assert build_wemob_tools([], run_context=request, client=GoToolClient(base_url="", token="")) == []
    assert build_wemob_tools(None, run_context=request, client=GoToolClient(base_url="", token="")) == []


def test_tool_bridge_handles_missing_base_url() -> None:
    request = _make_request()
    tool = make_wemob_tool(
        {"name": "sandbox_status", "description": "x", "schema": {}, "risk_level": "low"},
        run_context=request,
        client=GoToolClient(base_url="", token=""),
    )
    result = asyncio.run(tool())
    # The model should see a string it can react to, not an exception.
    assert "tool bridge" in result.lower() or "go tool client" in result.lower()


def test_max_tool_result_chars_is_documented() -> None:
    # If we change the cap, the plan's server-side cap in the Go bridge
    # (16_000) must be updated in lockstep. Keep them in sync via a test.
    assert MAX_TOOL_RESULT_CHARS == 16_000
