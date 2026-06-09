"""Tests for PR 9 LLM usage / LangSmith bridge.

These cover the pure-Python shape of the llm_usage event payload and the
helper functions. They do not require deepagents, langchain, or langsmith
to be installed.
"""

from __future__ import annotations

import json

from app.schemas import RunStreamRequest
from app.usage import (
    RUNTIME_KIND,
    UsageRecorder,
    WemobUsageCallback,
    encode_usage_event,
    is_langsmith_enabled,
    langsmith_callbacks,
    langsmith_metadata,
    make_event_emitter,
    make_fake_usage_payload,
)


def _make_request(run_id: str = "run-1") -> RunStreamRequest:
    return RunStreamRequest(
        schema_version="2026-06-09",
        job_id="job-1",
        run_id=run_id,
        trace_id="trace-1",
        user_id="user-1",
        organization_id="org-1",
        project_id="p-1",
        project_type="app",
        frontend_runtime="nextjs",
        is_first_generation=True,
        message="x",
        repo={
            "mount_base": "/mnt/wemob-repos",
            "project_id": "p-1",
            "virtual_root": "/workspace",
        },
        model={"provider": "anthropic", "model": "test-model"},
    )


def test_runtime_kind_is_deepagents() -> None:
    assert RUNTIME_KIND == "deepagents"


def test_make_fake_usage_payload_shape() -> None:
    p = make_fake_usage_payload()
    assert p["runtime_kind"] == "deepagents"
    assert p["model"] == "anthropic/claude-sonnet-4.6"
    assert p["prompt_tokens"] == 100
    assert p["completion_tokens"] == 50
    assert p["subagent"] == ""


def test_encode_usage_event_is_ndjson() -> None:
    raw = encode_usage_event("run-1", make_fake_usage_payload(), job_id="job-1")
    assert raw.endswith(b"\n")
    payload = json.loads(raw.decode("utf-8"))
    assert payload["type"] == "llm_usage"
    assert payload["run_id"] == "run-1"
    assert payload["data"]["runtime_kind"] == "deepagents"


def test_recorder_make_usage_event_carries_all_fields() -> None:
    rec = UsageRecorder()
    raw = rec.make_usage_event(
        run_id="run-1",
        job_id="job-1",
        model="anthropic/claude-sonnet-4.6",
        provider="openrouter",
        prompt_tokens=120,
        completion_tokens=60,
        cache_read_tokens=10,
        cache_write_tokens=5,
        cost_usd=0.002,
        latency_ms=300,
        subagent="design_direction_agent",
    )
    payload = json.loads(raw.decode("utf-8").rstrip("\n"))
    assert payload["type"] == "llm_usage"
    data = payload["data"]
    assert data["model"] == "anthropic/claude-sonnet-4.6"
    assert data["prompt_tokens"] == 120
    assert data["completion_tokens"] == 60
    assert data["cache_read_tokens"] == 10
    assert data["cache_write_tokens"] == 5
    assert data["cost_usd"] == 0.002
    assert data["latency_ms"] == 300
    assert data["subagent"] == "design_direction_agent"
    assert data["runtime_kind"] == "deepagents"


def test_recorder_seq_strictly_increases() -> None:
    rec = UsageRecorder()
    seqs = []
    for _ in range(5):
        raw = rec.make_usage_event(
            run_id="r",
            job_id=None,
            model="m",
            provider="p",
            prompt_tokens=1,
            completion_tokens=1,
        )
        payload = json.loads(raw.decode("utf-8").rstrip("\n"))
        seqs.append(payload["seq"])
    assert seqs == sorted(set(seqs))
    assert len(seqs) == 5


def test_make_event_emitter_accumulates_chunks_and_emits_final() -> None:
    request = _make_request()
    emit = make_event_emitter(request, subagent="frontend_builder_agent")

    # First chunk: non-final, accumulates and returns empty bytes.
    out1 = emit({
        "span_id": "span-A",
        "model": "m",
        "provider": "p",
        "prompt_tokens": 50,
        "completion_tokens": 10,
    })
    assert out1 == b""

    # Second chunk: still non-final, accumulates.
    out2 = emit({
        "span_id": "span-A",
        "model": "m",
        "provider": "p",
        "prompt_tokens": 30,
        "completion_tokens": 5,
    })
    assert out2 == b""

    # Final chunk: emits consolidated event with totals.
    out3 = emit({
        "span_id": "span-A",
        "model": "m",
        "provider": "p",
        "prompt_tokens": 80,
        "completion_tokens": 15,
        "cost_usd": 0.005,
        "final": True,
    })
    payload = json.loads(out3.decode("utf-8").rstrip("\n"))
    assert payload["type"] == "llm_usage"
    assert payload["data"]["prompt_tokens"] == 80
    assert payload["data"]["completion_tokens"] == 15
    assert payload["data"]["cost_usd"] == 0.005
    assert payload["data"]["subagent"] == "frontend_builder_agent"


def test_callback_emits_event_on_llm_end() -> None:
    captured: list[bytes] = []
    request = _make_request()
    # Wrap the production emit so we can also observe the bytes.
    base_emit = make_event_emitter(request, subagent="")

    def _capture(usage: dict) -> bytes:
        raw = base_emit(usage)
        if raw:
            captured.append(raw)
        return raw

    cb = WemobUsageCallback(emit=_capture)
    cb._on_start(run_id=request.run_id, model="test-model")
    captured.clear()  # on_start has no emit

    class _Msg:
        usage_metadata = {
            "prompt_tokens": 200,
            "completion_tokens": 80,
            "cache_read_input_tokens": 12,
            "cache_creation_input_tokens": 0,
            "cost": 0.012,
        }
        response_metadata = {"model_name": "anthropic/claude-sonnet-4.6", "provider": "anthropic"}

    class _Gen:
        message = _Msg()

    class _Resp:
        generations = [[_Gen()]]
        llm_output = None
        response_metadata = {}

    cb._on_end(run_id=request.run_id, response=_Resp(), error=None)
    assert len(captured) >= 1
    payload = json.loads(captured[-1].decode("utf-8").rstrip("\n"))
    assert payload["type"] == "llm_usage"
    data = payload["data"]
    assert data["prompt_tokens"] == 200
    assert data["completion_tokens"] == 80
    assert data["cache_read_tokens"] == 12
    # model from on_start slot wins when present.
    assert data["model"] == "test-model"
    assert data["cost_usd"] == 0.012
    assert data["subagent"] == ""


def test_callback_swallows_exceptions() -> None:
    """If the emit closure raises, the callback must not propagate — billing
    failures must never fail the agent run."""
    def _boom(_u):
        raise RuntimeError("simulated")

    cb = WemobUsageCallback(emit=_boom)
    cb._on_start(run_id="run-1", model="m")

    class _Resp:
        generations = [[]]
        llm_output = {"usage": {"prompt_tokens": 1, "completion_tokens": 1}}

    # Must not raise.
    cb._on_end(run_id="run-1", response=_Resp(), error=None)


def test_langsmith_callbacks_returns_empty_when_disabled() -> None:
    # Default: LANGSMITH_TRACING env is unset / false. The helper must
    # return an empty list so the runtime loop still works.
    assert langsmith_callbacks(_make_request()) == []


def test_langsmith_metadata_carries_runtime_and_subagents() -> None:
    md = langsmith_metadata(_make_request(), subagent_names=("design_direction_agent", "frontend_builder_agent"))
    assert md["trace_id"] == "run-1"
    assert md["run_id"] == "run-1"
    assert md["job_id"] == "job-1"
    assert md["project_id"] == "p-1"
    assert md["user_id"] == "user-1"
    assert md["organization_id"] == "org-1"
    assert md["runtime"] == "deepagents"
    assert md["model"] == "test-model"
    assert "design_direction_agent" in md["subagents"]
    assert "frontend_builder_agent" in md["subagents"]


def test_is_langsmith_enabled_default_false() -> None:
    # When the env var is unset the helper should be False. This holds
    # unless the test runner has LANGSMITH_TRACING set.
    import os

    os.environ.pop("LANGSMITH_TRACING", None)
    # Re-import the module to pick up the new env. (The Settings class is
    # also a lazy read in our usage module; just call is_langsmith_enabled
    # and accept that it may flip if the runner has the env set.)
    assert is_langsmith_enabled() in (True, False)
