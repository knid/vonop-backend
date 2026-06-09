from __future__ import annotations

import json
import logging
import os
import time
import uuid
from typing import Any, Mapping, Sequence

from .schemas import RunStreamRequest
from .streaming import ndjson_event

logger = logging.getLogger("deepagents.usage")

RUNTIME_KIND = "deepagents"


class UsageRecorder:
    """Thread-safe accumulator for one agent run's token usage.

    The Go-side TokenTracker is the source of truth for billing; this
    recorder is a thin pass-through that ensures every model call emits an
    `llm_usage` event into the run stream. The Go runtime consumes those
    events and appends TokenEntry rows on the Go side (see
    `utils.PythonUsageEntry` in the Go codebase).
    """

    def __init__(self) -> None:
        self._seq = 0
        # Per-span accumulator so we can compute cache math even when
        # LangChain streams chunks and reports usage in pieces.
        self._spans: dict[str, dict[str, int]] = {}

    def _next_seq(self) -> int:
        self._seq += 1
        return self._seq

    def make_usage_event(
        self,
        *,
        run_id: str,
        job_id: str | None,
        model: str,
        provider: str,
        prompt_tokens: int,
        completion_tokens: int,
        cache_read_tokens: int = 0,
        cache_write_tokens: int = 0,
        cost_usd: float = 0.0,
        latency_ms: int = 0,
        subagent: str = "",
        span_id: str = "",
    ) -> bytes:
        data = {
            "span_id": span_id or uuid.uuid4().hex,
            "provider": provider,
            "model": model,
            "prompt_tokens": max(0, int(prompt_tokens)),
            "completion_tokens": max(0, int(completion_tokens)),
            "cache_read_tokens": max(0, int(cache_read_tokens)),
            "cache_write_tokens": max(0, int(cache_write_tokens)),
            "cost_usd": float(cost_usd or 0.0),
            "latency_ms": max(0, int(latency_ms)),
            "subagent": subagent or "",
            "runtime_kind": RUNTIME_KIND,
        }
        return ndjson_event(
            "llm_usage",
            run_id=run_id,
            seq=self._next_seq(),
            job_id=job_id,
            data=data,
        )

    def span_started(self, span_id: str) -> None:
        self._spans[span_id] = {
            "started_at_ms": int(time.time() * 1000),
            "prompt_tokens": 0,
            "completion_tokens": 0,
            "cache_read_tokens": 0,
            "cache_write_tokens": 0,
            "cost_usd": 0.0,
        }

    def span_finished(self, span_id: str) -> None:
        self._spans.pop(span_id, None)

    def span_accumulate(
        self,
        span_id: str,
        *,
        prompt_tokens: int = 0,
        completion_tokens: int = 0,
        cache_read_tokens: int = 0,
        cache_write_tokens: int = 0,
        cost_usd: float = 0.0,
    ) -> dict[str, int] | None:
        rec = self._spans.get(span_id)
        if rec is None:
            return None
        rec["prompt_tokens"] += int(prompt_tokens)
        rec["completion_tokens"] += int(completion_tokens)
        rec["cache_read_tokens"] += int(cache_read_tokens)
        rec["cache_write_tokens"] += int(cache_write_tokens)
        rec["cost_usd"] = float(rec.get("cost_usd", 0.0)) + float(cost_usd)
        return rec


# Global registry keyed by run_id so the LangChain callback handler can find
# the right recorder. Cleared when the run finishes.
_recorders: dict[str, UsageRecorder] = {}


def get_recorder(run_id: str) -> UsageRecorder:
    rec = _recorders.get(run_id)
    if rec is None:
        rec = UsageRecorder()
        _recorders[run_id] = rec
    return rec


def drop_recorder(run_id: str) -> None:
    _recorders.pop(run_id, None)


def make_event_emitter(
    request: RunStreamRequest,
    subagent: str = "",
) -> "Callable[[Mapping[str, Any]], bytes]":
    """Return a closure that builds llm_usage NDJSON frames for this run.

    The closure is what the LangChain callback handler calls on each model
    response. The Go side consumes the resulting event via the
    `llm_usage` switch arm in `runtime_deepagents.go`.
    """
    recorder = get_recorder(request.run_id)

    def _emit(usage: Mapping[str, Any]) -> bytes:
        # LangChain token accounting sometimes splits prompt vs completion
        # across multiple events. We accumulate on the recorder side and
        # emit one final consolidated event per span.
        span_id = str(usage.get("span_id") or uuid.uuid4().hex)
        prompt = int(usage.get("prompt_tokens") or 0)
        completion = int(usage.get("completion_tokens") or 0)
        cache_read = int(usage.get("cache_read_tokens") or 0)
        cache_write = int(usage.get("cache_write_tokens") or 0)
        cost = float(usage.get("cost_usd") or 0.0)
        provider = str(usage.get("provider") or "")
        model = str(usage.get("model") or "")
        latency_ms = int(usage.get("latency_ms") or 0)

        # First time we see this span, record start time so latency is
        # measured from the first model call until final usage.
        if span_id not in recorder._spans:  # noqa: SLF001 — internal but stable
            recorder.span_started(span_id)

        if usage.get("final") is True or usage.get("done") is True:
            rec = recorder._spans.pop(span_id, None)  # noqa: SLF001
            totals = {
                "prompt_tokens": prompt,
                "completion_tokens": completion,
                "cache_read_tokens": cache_read,
                "cache_write_tokens": cache_write,
                "cost_usd": cost,
            }
            if rec is not None:
                # LangChain may report usage in pieces — prefer the larger of
                # the running total and the latest chunk.
                totals = {
                    "prompt_tokens": max(prompt, rec.get("prompt_tokens", 0)),
                    "completion_tokens": max(completion, rec.get("completion_tokens", 0)),
                    "cache_read_tokens": max(cache_read, rec.get("cache_read_tokens", 0)),
                    "cache_write_tokens": max(cache_write, rec.get("cache_write_tokens", 0)),
                    "cost_usd": max(cost, rec.get("cost_usd", 0.0)),
                }
            started = rec.get("started_at_ms") if rec else None
            if started and latency_ms == 0:
                latency_ms = int(time.time() * 1000) - int(started)
            return recorder.make_usage_event(
                run_id=request.run_id,
                job_id=request.job_id,
                model=model,
                provider=provider,
                prompt_tokens=totals["prompt_tokens"],
                completion_tokens=totals["completion_tokens"],
                cache_read_tokens=totals["cache_read_tokens"],
                cache_write_tokens=totals["cache_write_tokens"],
                cost_usd=totals["cost_usd"],
                latency_ms=latency_ms,
                subagent=subagent,
                span_id=span_id,
            )

        # Non-final chunk — accumulate, do not emit.
        recorder.span_accumulate(
            span_id,
            prompt_tokens=prompt,
            completion_tokens=completion,
            cache_read_tokens=cache_read,
            cache_write_tokens=cache_write,
            cost_usd=cost,
        )
        # Return an empty bytes so the callback can still yield without
        # producing a stream frame. The stream consumer filters empty
        # lines anyway.
        return b""

    return _emit


class WemobUsageCallback:
    """LangChain BaseCallbackHandler that emits llm_usage events per call.

    Implementation strategy:
    - on_llm_start records the start time + model + provider.
    - on_llm_end (and on_llm_error) computes latency and emits the final
      consolidated event with the full usage payload from the LLM result.
    - on_chat_model_start / on_llm_new_token are no-ops (token streaming
      events don't carry usage).

    This handler is best-effort. Any failure to extract usage is logged
    but does NOT fail the agent run — billing is durable, model output
    is not.
    """

    # LangChain reads these class-level attributes to know which hooks
    # the handler supports. Setting them explicitly avoids AttributeError
    # on older LangChain versions that probe via hasattr.
    run_inline = True
    raise_error = False
    ignore_agent = False
    ignore_chain = False
    ignore_chat_model = False
    ignore_llm = False
    ignore_retriever = False
    ignore_retry = False
    ignore_subchain = False

    def __init__(self, emit: "Callable[[Mapping[str, Any]], bytes] | None" = None) -> None:
        self._emit = emit
        # Per-run-id -> {span_id -> start_ms, model, provider, subagent}
        self._starts: dict[str, dict[str, dict[str, Any]]] = {}

    # --- LangChain callback hooks ---

    def on_chat_model_start(self, serialized, messages, *, run_id=None, **kwargs):  # type: ignore[no-untyped-def,override]
        self._on_start(run_id=run_id, model=kwargs.get("invocation_params", {}).get("model_name") or kwargs.get("invocation_params", {}).get("model"))

    def on_llm_start(self, serialized, prompts, *, run_id=None, **kwargs):  # type: ignore[no-untyped-def,override]
        self._on_start(run_id=run_id, model=kwargs.get("invocation_params", {}).get("model_name") or kwargs.get("invocation_params", {}).get("model"))

    def _on_start(self, *, run_id: Any, model: Any) -> None:
        if not run_id or self._emit is None:
            return
        rid = str(run_id)
        self._starts.setdefault(rid, {})[uuid.uuid4().hex] = {
            "started_at_ms": int(time.time() * 1000),
            "model": str(model or ""),
            "subagent": "",
        }

    def on_llm_end(self, response, *, run_id=None, **kwargs):  # type: ignore[no-untyped-def,override]
        self._on_end(run_id=run_id, response=response, error=None)

    def on_llm_error(self, error, *, run_id=None, **kwargs):  # type: ignore[no-untyped-def,override]
        self._on_end(run_id=run_id, response=None, error=error)

    def _on_end(self, *, run_id: Any, response: Any, error: Any) -> None:
        if not run_id or self._emit is None:
            return
        rid = str(run_id)
        slots = self._starts.get(rid) or {}
        # Pop the most recent slot — LangChain may not surface the span id
        # back to us, but start/end are balanced per call.
        span_id = ""
        if slots:
            for sid in list(slots.keys()):
                span_id = sid
                break
        slot = slots.pop(span_id, {}) if span_id else {}
        started_ms = int(slot.get("started_at_ms") or 0)
        latency_ms = int(time.time() * 1000) - started_ms if started_ms else 0

        usage = _extract_usage(response, error)
        model = str(slot.get("model") or usage.get("model") or "")
        provider = str(usage.get("provider") or "")

        # Emit a final event. The emit closure handles accumulation when
        # called multiple times for the same span, but for the canonical
        # LangChain lifecycle this is the only call per model invocation.
        if self._emit is not None:
            try:
                self._emit({
                    "span_id": span_id,
                    "model": model,
                    "provider": provider,
                    "prompt_tokens": int(usage.get("prompt_tokens") or 0),
                    "completion_tokens": int(usage.get("completion_tokens") or 0),
                    "cache_read_tokens": int(usage.get("cache_read_tokens") or 0),
                    "cache_write_tokens": int(usage.get("cache_write_tokens") or 0),
                    "cost_usd": float(usage.get("cost_usd") or 0.0),
                    "latency_ms": latency_ms,
                    "subagent": str(slot.get("subagent") or ""),
                    "final": True,
                })
            except Exception:  # pragma: no cover — defensive, never fail the run
                logger.exception("WemobUsageCallback: failed to emit llm_usage event")


def _extract_usage(response: Any, error: Any) -> dict[str, Any]:
    """Best-effort extraction of usage from a LangChain LLMResult / error.

    LangChain's LLMResult carries llm_output.usage, and individual chat
    generations carry message.usage_metadata. Some adapters put it on
    response_metadata. We try the common locations in order of likelihood.
    """
    out: dict[str, Any] = {}
    if response is None:
        return out
    # llm_output (legacy)
    llm_output = getattr(response, "llm_output", None)
    if isinstance(llm_output, Mapping):
        usage = llm_output.get("usage") or llm_output.get("token_usage")
        if isinstance(usage, Mapping):
            out.update(_normalize_usage(usage))
    # Per-generation usage_metadata
    for gen in getattr(response, "generations", []) or []:
        for g in gen or []:
            meta = getattr(g, "message", None)
            if meta is None:
                continue
            um = getattr(meta, "usage_metadata", None)
            if isinstance(um, Mapping):
                out.update(_normalize_usage(um))
            rm = getattr(meta, "response_metadata", None)
            if isinstance(rm, Mapping):
                inner = rm.get("usage") or rm.get("token_usage")
                if isinstance(inner, Mapping):
                    out.update(_normalize_usage(inner))
            # Some LangChain builds stash the dict on .additional_kwargs
            ak = getattr(meta, "additional_kwargs", None)
            if isinstance(ak, Mapping):
                inner = ak.get("usage") or ak.get("token_usage")
                if isinstance(inner, Mapping):
                    out.update(_normalize_usage(inner))
    # provider (sometimes on response_metadata.usage.provider or .model)
    rm = getattr(response, "response_metadata", None)
    if isinstance(rm, Mapping):
        if not out.get("provider") and isinstance(rm.get("provider"), str):
            out["provider"] = rm["provider"]
        if not out.get("model") and isinstance(rm.get("model_name"), str):
            out["model"] = rm["model_name"]
        if not out.get("model") and isinstance(rm.get("model"), str):
            out["model"] = rm["model"]
    return out


def _normalize_usage(usage: Mapping[str, Any]) -> dict[str, Any]:
    out: dict[str, Any] = {}
    out["prompt_tokens"] = int(usage.get("prompt_tokens") or usage.get("input_tokens") or 0)
    out["completion_tokens"] = int(usage.get("completion_tokens") or usage.get("output_tokens") or 0)
    out["cache_read_tokens"] = int(usage.get("cache_read_input_tokens") or usage.get("cache_read_tokens") or 0)
    out["cache_write_tokens"] = int(usage.get("cache_creation_input_tokens") or usage.get("cache_write_tokens") or 0)
    cost = usage.get("cost") or usage.get("cost_usd")
    if cost is not None:
        out["cost_usd"] = float(cost or 0.0)
    if isinstance(usage.get("model"), str):
        out["model"] = usage["model"]
    if isinstance(usage.get("provider"), str):
        out["provider"] = usage["provider"]
    return out


# --- LangSmith tracing ---

LANGSMITH_PROJECT = os.getenv("LANGSMITH_PROJECT", "wemob-deepagents-prod")
LANGSMITH_TRACING = (os.getenv("LANGSMITH_TRACING", "false").lower() in {"1", "true", "yes"})


def langsmith_callbacks(run_context: RunStreamRequest) -> list[Any]:
    """Return LangSmith callbacks when tracing is enabled.

    We do not require langsmith at import time. If the package is missing
    we log a warning and return an empty list — the run still completes,
    just without LangSmith traces.
    """
    if not LANGSMITH_TRACING:
        return []
    try:
        pass  # type: ignore
    except Exception:
        logger.warning(
            "LANGSMITH_TRACING=true but langsmith is not installed; "
            "Python LLM calls will not be traced. pip install langsmith."
        )
        return []

    # The langsmith tracing_context is a context manager, not a callback.
    # We expose it via metadata on the agent invocation; deepagents honors
    # `tags`/`metadata` on its invoke, so we let the caller pass these.
    try:
        # Newer langsmith exposes LangChainCallbackHandler
        from langchain_core.tracers import LangChainTracer  # type: ignore
    except Exception:
        LangChainTracer = None  # type: ignore

    tracer = None
    if LangChainTracer is not None:
        try:
            tracer = LangChainTracer(
                project_name=LANGSMITH_PROJECT,
            )
        except Exception:
            logger.exception("failed to construct LangChainTracer; continuing without trace")
            tracer = None

    # Also push metadata into env so any child clients pick it up.
    os.environ.setdefault("LANGSMITH_PROJECT", LANGSMITH_PROJECT)
    os.environ.setdefault("LANGSMITH_TRACING", "true")
    if run_context.run_id:
        os.environ.setdefault("LANGSMITH_RUN_ID", run_context.run_id)

    if tracer is None:
        return []
    return [tracer]


def langsmith_metadata(run_context: RunStreamRequest, subagent_names: Sequence[str]) -> dict[str, Any]:
    """Build the metadata dict we attach to a LangSmith trace.

    The plan (§PR 9 task 7) requires the trace to share the AppAgentRun.ID
    and to surface job_id / project_id / org_id / runtime / model / skills /
    subagents so operators can pivot from a LangSmith trace straight to
    the Wemob app agent run row.
    """
    return {
        "trace_id": run_context.run_id,
        "run_id": run_context.run_id,
        "job_id": run_context.job_id,
        "project_id": run_context.project_id,
        "user_id": run_context.user_id,
        "organization_id": run_context.organization_id,
        "runtime": RUNTIME_KIND,
        "model": run_context.model.model,
        "provider": run_context.model.provider,
        "subagents": list(subagent_names),
        "is_first_generation": bool(run_context.is_first_generation),
    }


def is_langsmith_enabled() -> bool:
    return LANGSMITH_TRACING


# Surface a tiny helper for tests so they can assert the JSON payload
# without going through the LangChain callback.
def make_fake_usage_payload(
    *,
    model: str = "anthropic/claude-sonnet-4.6",
    provider: str = "openrouter",
    prompt_tokens: int = 100,
    completion_tokens: int = 50,
    cache_read_tokens: int = 0,
    cache_write_tokens: int = 0,
    cost_usd: float = 0.001,
    latency_ms: int = 200,
    subagent: str = "",
    span_id: str | None = None,
) -> dict[str, Any]:
    return {
        "span_id": span_id or uuid.uuid4().hex,
        "provider": provider,
        "model": model,
        "prompt_tokens": prompt_tokens,
        "completion_tokens": completion_tokens,
        "cache_read_tokens": cache_read_tokens,
        "cache_write_tokens": cache_write_tokens,
        "cost_usd": cost_usd,
        "latency_ms": latency_ms,
        "subagent": subagent,
        "runtime_kind": RUNTIME_KIND,
    }


def encode_usage_event(run_id: str, payload: Mapping[str, Any], job_id: str | None = None) -> bytes:
    """Helper used by tests and the runtime to render a payload as NDJSON."""
    return ndjson_event(
        "llm_usage",
        run_id=run_id,
        seq=0,
        job_id=job_id,
        data=dict(payload),
    )


# Defensive: log unexpected JSON encoding issues instead of silently
# failing the run.
def _safe_json_dumps(payload: Mapping[str, Any]) -> str:
    try:
        return json.dumps(payload, ensure_ascii=False)
    except (TypeError, ValueError):
        return json.dumps({k: str(v) for k, v in payload.items()}, ensure_ascii=False)
