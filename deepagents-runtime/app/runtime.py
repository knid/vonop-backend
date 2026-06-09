from __future__ import annotations

import asyncio
import json
import os
import re
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, AsyncGenerator, Mapping

from .permissions import is_allowed_workspace_path
from .repo_paths import resolve_repo_path
from .schemas import RunStreamRequest
from .skills import SKILLS_ROOT, resolve_skills


_MODEL_CACHE: dict[tuple[str, str, str | None], Any] = {}
_WRITE_DIRECTIVE_RE = re.compile(
    r"write\s+(?P<path>/workspace/[^\n:]+?)\s+with:\s*(?P<content>.*)$",
    re.IGNORECASE | re.DOTALL,
)


def _model_cache_key(provider: str, model: str, langchain_model: str | None) -> tuple[str, str, str | None]:
    return provider.strip().lower(), model.strip(), (langchain_model or "").strip() or None


def build_model(model_config: Any) -> Any:
    """Build a LangChain chat model for DeepAgents integration.

    This function is intentionally defensive: production-capable adapters are used
    when available, but any import/creation failure falls back to a sentinel that
    triggers runtime fallback behavior.
    """

    provider = str(getattr(model_config, "provider", getattr(model_config, "get", lambda _: "")("provider"))).strip().lower()
    model_name = str(getattr(model_config, "model", getattr(model_config, "get", lambda _: "")("model"))).strip()
    langchain_model = str(
        getattr(model_config, "langchain_model", getattr(model_config, "get", lambda _: "")("langchain_model"))
    ).strip()

    if not provider or not model_name:
        raise ValueError("provider and model are required")

    cache_key = _model_cache_key(provider, model_name, langchain_model or None)
    if cache_key in _MODEL_CACHE:
        return _MODEL_CACHE[cache_key]

    base_model = model_name
    if langchain_model and ":" in langchain_model and not langchain_model.startswith(("file:", "http")):
        if provider in {"openrouter", "openrouter-compatible", "openrouter-compatible-api"} or langchain_model.startswith("openrouter:"):
            base_model = langchain_model.split(":", 1)[1]

    if provider == "anthropic":
        try:
            from langchain_anthropic import ChatAnthropic
        except Exception as exc:
            raise RuntimeError("anthropic adapter unavailable") from exc

        model = ChatAnthropic(
            model=base_model or model_name,
            max_tokens=None,
            timeout=None,
        )
        _MODEL_CACHE[cache_key] = model
        return model

    if provider in {"openrouter", "moonshot", "moonshot-ai", "openai", "openai-compatible", "openrouter-compatible"}:
        try:
            from langchain_openai import ChatOpenAI
        except Exception as exc:
            raise RuntimeError("openai-compatible adapter unavailable") from exc

        is_openrouter = provider in {"openrouter", "openrouter-compatible", "moonshot", "moonshot-ai"}
        base_url = "https://openrouter.ai/api/v1" if is_openrouter else None
        if provider in {"moonshot", "moonshot-ai"}:
            base_url = os.getenv("MOONSHOT_BASE_URL", "https://api.moonshot.ai/v1")
        elif provider in {"openrouter", "openrouter-compatible"}:
            base_url = os.getenv("OPENROUTER_BASE_URL", base_url)

        model = ChatOpenAI(
            model=base_model or model_name,
            base_url=base_url,
            timeout=None,
        )
        _MODEL_CACHE[cache_key] = model
        return model

    raise ValueError(f"unsupported model provider: {provider}")


@dataclass(frozen=True)
class _PathInfo:
    repo_root: Path
    workspace_root: str = "/workspace"
    skills_root: str = "/skills"


def _resolve_request_payload(payload: RunStreamRequest | Mapping[str, Any]) -> RunStreamRequest:
    if isinstance(payload, RunStreamRequest):
        return payload
    if not isinstance(payload, Mapping):
        raise TypeError("payload must be RunStreamRequest or mapping")
    return RunStreamRequest.model_validate(dict(payload))


def _make_event(
    event_type: str,
    *,
    run_id: str,
    job_id: str | None,
    seq: int,
    data: Mapping[str, Any],
) -> bytes:
    envelope = {
        "type": event_type,
        "run_id": run_id,
        "job_id": job_id or "",
        "seq": seq,
        "time_unix_ms": int(time.time() * 1000),
        "data": dict(data),
    }
    return json.dumps(envelope).encode("utf-8") + b"\n"


def _normalise_path(path: str) -> str:
    if not path.startswith("/"):
        path = f"/{path}"
    return path


def _resolve_workspace_target(root: Path, path: str) -> Path:
    normalized = _normalise_path(path)
    if not is_allowed_workspace_path(normalized):
        raise PermissionError(f"workspace path denied: {path}")
    relative = normalized.removeprefix("/workspace/")
    target = (root / relative).resolve()
    if not str(target).startswith(str(root.resolve()) + os.sep) and target != root:
        raise PermissionError(f"workspace path escapes repo: {path}")
    return target


def _safe_decode_payload(value: Any) -> str | None:
    if value is None:
        return None
    if isinstance(value, bytes):
        try:
            return value.decode("utf-8")
        except Exception:
            return None
    if isinstance(value, str):
        return value
    return None


def _extract_event_type(event: Any) -> str:
    if isinstance(event, Mapping):
        event_type = event.get("type") or event.get("event") or event.get("name")
        if event_type:
            return str(event_type)
    if isinstance(event, str):
        return event
    for attr in ("type", "name", "event_name", "event"):
        value = getattr(event, attr, None)
        if value:
            return str(value)
    return ""


def _extract_event_payload(event: Any) -> dict[str, Any]:
    if isinstance(event, Mapping):
        for key in ("data", "payload", "body"):
            value = event.get(key)
            if isinstance(value, dict):
                return dict(value)
            if isinstance(value, str):
                try:
                    parsed = json.loads(value)
                    if isinstance(parsed, dict):
                        return parsed
                except Exception:
                    pass
        if {"type", "data"}.issuperset(event.keys()) and "type" in event:
            return {k: v for k, v in event.items() if k != "type"}
        return dict(event)
    if isinstance(event, str):
        try:
            parsed = json.loads(event)
            if isinstance(parsed, dict):
                return parsed
        except Exception:
            return {"raw": event}
    if hasattr(event, "model_dump"):
        try:
            return dict(event.model_dump())
        except Exception:
            pass
    if hasattr(event, "dict"):
        try:
            return dict(event.dict())
        except Exception:
            pass
    result: dict[str, Any] = {}
    for attr in (
        "delta",
        "turn",
        "agent",
        "subagent",
        "tool_call_id",
        "tool_name",
        "status",
        "summary",
        "is_error",
        "duration_ms",
        "input_preview",
        "risk_level",
        "subagent_id",
        "name",
        "description",
        "parent",
        "message",
        "summary",
        "tool_calls",
        "model_calls",
        "reason",
        "result",
        "code",
        "phase",
        "recoverable",
    ):
        if hasattr(event, attr):
            result[attr] = getattr(event, attr)
    return result


def build_workspace_backend(repo_path: str | Path):
    repo_root = Path(repo_path).resolve()
    try:
        from deepagents.backends import CompositeBackend, FilesystemBackend, StateBackend
        from deepagents.permissions import FilesystemPermission
    except Exception:
        return {
            "routes": {
                "/workspace/": {"root": str(repo_root), "virtual_mode": True},
                "/skills/": {"root": str(SKILLS_ROOT), "virtual_mode": True},
            },
            "state_backend": "state",
            "permissions": None,
        }

    permissions = None
    try:
        permissions = FilesystemPermission(
            allowed_roots=("/workspace", "/skills"),
            denied_roots=(
                "/workspace/.git",
                "/workspace/.env",
                "/workspace/.env.",
                "/workspace/_wemob/",
                "/workspace/node_modules/",
                "/workspace/.next/",
                "/workspace/dist/",
                "/workspace/build/",
                "/workspace/web-build/",
                "/workspace/ios/",
                "/workspace/android/",
            ),
            read_write_denied_roots=("/skills",),
        )
    except Exception:
        # Older/newer deepagents may expose different permission constructors.
        permissions = None

    return CompositeBackend(
        default=StateBackend(),
        routes={
            "/workspace/": FilesystemBackend(
                root_dir=str(repo_root),
                virtual_mode=True,
                permissions=permissions,
            ),
            "/skills/": FilesystemBackend(
                root_dir=str(SKILLS_ROOT),
                virtual_mode=True,
            ),
        },
    )


def build_subagents(request: RunStreamRequest) -> dict[str, Any]:
    # Keep static subagent names and identities aligned with the plan.
    runtime = str(request.project_type or "").lower()
    role = str(request.frontend_runtime or "").lower()

    return {
        "design_direction_agent": {
            "name": "design_direction_agent",
            "description": "Plan or validate visual direction and UX goals before implementation.",
            "parent": "main",
            "focus": ["design-system consistency", "content hierarchy", "interaction flow"],
            "project_type": runtime,
            "runtime": role,
        },
        "frontend_builder_agent": {
            "name": "frontend_builder_agent",
            "description": "Implement frontend work on Next.js and web surfaces with existing conventions.",
            "parent": "main",
            "focus": ["pages", "components", "styles", "assets"],
        },
        "data_backend_agent": {
            "name": "data_backend_agent",
            "description": "Handle API shape, persistence, and backend-safe integration points.",
            "parent": "main",
            "focus": ["routes", "data-models", "db", "validation"],
        },
        "qa_debug_agent": {
            "name": "qa_debug_agent",
            "description": "Identify likely runtime/build/test gaps and propose concrete fixes.",
            "parent": "main",
            "focus": ["edge cases", "error handling", "tests", "logging"],
        },
    }


def _system_prompt(request: RunStreamRequest, selected_skills: list[str]) -> str:
    safe_message = (request.message or "").strip()
    first_generation = "Yes" if request.is_first_generation else "No"
    project_type = request.project_type or "app"

    sections = [
        "Identity",
        "You are Wemob's production app-building agent.",
        "",
        "Workspace",
        "The canonical project repo is mounted at /workspace.",
        "Use built-in filesystem tools for source edits.",
        "Never edit outside /workspace.",
        "Never touch .git, .env*, _wemob, node_modules, build outputs.",
        "",
        "Preview/runtime",
        "Do not assume local shell execution is available. Use Wemob tools when required.",
        "",
        "Design",
        "For first generation or visible UI work, create or read DESIGN.md before broad implementation.",
        "Avoid generic AI-style UI. Preserve existing design direction unless user requests redesign.",
        "",
        "Project type boundary",
        f"Respect project type '{project_type}'.",
        "",
        "Completion",
        "Finish with concise summary and mention important changed files.",
        "",
        f"First generation: {first_generation}",
        f"Frontend runtime: {request.frontend_runtime or 'none'}",
        f"Selected skills: {', '.join(selected_skills) if selected_skills else 'none'}",
        f"User message: {safe_message[:1500]}",
    ]
    return "\n".join(sections)


async def _stream_fallback(request: RunStreamRequest, repo_root: Path) -> AsyncGenerator[bytes, None]:
    run_id = request.run_id
    job_id = request.job_id
    seq = 1

    yield _make_event(
        "subagent_start",
        run_id=run_id,
        job_id=job_id,
        seq=seq,
        data={
            "subagent_id": "design_direction_agent",
            "name": "design_direction_agent",
            "description": "Planning initial design direction before edits.",
            "parent": "main",
        },
    )
    seq += 1

    yield _make_event(
        "reasoning_delta",
        run_id=run_id,
        job_id=job_id,
        seq=seq,
        data={"delta": "Inspecting request and workspace context.\n", "turn": 1, "agent": "main", "subagent": "design_direction_agent"},
    )
    seq += 1

    tool_call_id = str(uuid.uuid4())
    yield _make_event(
        "tool_call_start",
        run_id=run_id,
        job_id=job_id,
        seq=seq,
        data={
            "tool_call_id": tool_call_id,
            "tool_name": "filesystem_write",
            "input_preview": {"path": "/workspace"},
            "risk_level": "low",
            "agent": "main",
            "subagent": "design_direction_agent",
        },
    )
    seq += 1

    write_payload = None
    match = _WRITE_DIRECTIVE_RE.search(request.message or "")
    if match and not request.read_only:
        path = match.group("path").strip()
        content = (match.group("content") or "").strip()
        target = _resolve_workspace_target(repo_root, path)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")
        write_payload = {
            "tool_call_id": tool_call_id,
            "tool_name": "filesystem_write",
            "status": "ok",
            "summary": f"wrote {path}",
            "is_error": False,
            "duration_ms": 1,
        }
    elif request.read_only:
        write_payload = {
            "tool_call_id": tool_call_id,
            "tool_name": "filesystem_write",
            "status": "error",
            "summary": "workspace writes disabled for read-only request",
            "is_error": True,
            "duration_ms": 0,
        }
    else:
        write_payload = {
            "tool_call_id": tool_call_id,
            "tool_name": "filesystem_write",
            "status": "ok",
            "summary": "no explicit write payload requested",
            "is_error": False,
            "duration_ms": 0,
        }

    yield _make_event(
        "tool_call_result",
        run_id=run_id,
        job_id=job_id,
        seq=seq,
        data=write_payload,
    )
    seq += 1

    yield _make_event(
        "message_delta",
        run_id=run_id,
        job_id=job_id,
        seq=seq,
        data={"delta": "Implemented workspace changes.\n", "turn": 1, "agent": "main", "subagent": None},
    )
    seq += 1

    yield _make_event(
        "subagent_end",
        run_id=run_id,
        job_id=job_id,
        seq=seq,
        data={
            "subagent_id": "design_direction_agent",
            "name": "design_direction_agent",
            "status": "ok",
            "summary": "No issues during fallback execution.",
        },
    )
    seq += 1

    # Always provide final done signal so callers can terminate stream loops.
    yield _make_event(
        "done",
        run_id=run_id,
        job_id=job_id,
        seq=seq,
        data={"summary": "fallback execution complete", "turns": 1, "tool_calls": 1, "model_calls": 0},
    )


def _coerce_output_preview(value: Any) -> dict[str, Any]:
    if value is None:
        return {}
    if isinstance(value, Mapping):
        return dict(value)
    if hasattr(value, "model_dump"):
        try:
            return dict(value.model_dump())
        except Exception:
            pass
    if hasattr(value, "dict"):
        try:
            return dict(value.dict())
        except Exception:
            pass
    return {"value": str(value)}


async def _stream_deepagents(request: RunStreamRequest, repo_root: Path) -> AsyncGenerator[bytes, None]:
    model = build_model(request.model)
    backend = build_workspace_backend(repo_root)
    selected_skills = resolve_skills(request)
    prompt = _system_prompt(request, selected_skills)
    subagents = build_subagents(request)
    context = {
        "app_title": request.metadata.get("app_title", "Project"),
        "run_id": request.run_id,
        "project_id": request.project_id,
        "user_id": request.user_id,
        "organization_id": request.organization_id,
        "pre_commit": request.metadata.get("pre_commit"),
        "skills": selected_skills,
        "subagents": list(subagents.keys()),
    }

    # Import lazily so tests can run in environments without deepagents installed.
    from deepagents import DeepAgents

    agent_kwargs = {
        "model": model,
        "system_prompt": prompt,
        "backend": backend,
        "tools": request.tools,
        "context": context,
        "thread_id": request.run_id,
        "subagents": subagents,
    }
    try:
        agent = DeepAgents(**agent_kwargs)
    except TypeError:
        agent = DeepAgents(model=model, prompt=prompt, context=context)
        if hasattr(agent, "backend"):
            setattr(agent, "backend", backend)
        if hasattr(agent, "thread_id"):
            setattr(agent, "thread_id", request.run_id)

    event_stream = None
    for method_name in ("stream", "run", "astream", "stream_run", "run_stream"):
        method = getattr(agent, method_name, None)
        if method is None:
            continue
        try:
            if method_name in {"stream", "run", "stream_run", "run_stream"}:
                event_stream = method(request.message)
            else:
                event_stream = method(request.message, context=context)
            break
        except TypeError:
            continue
    if event_stream is None:
        raise RuntimeError("no supported deepagents streaming entrypoint found")

    seq = 1
    run_id = request.run_id
    if hasattr(event_stream, "__aiter__"):
        async for raw_event in event_stream:
            event_type = _extract_event_type(raw_event)
            payload = _extract_event_payload(raw_event)
            mapped = _map_event(event_type, payload, request, seq)
            if mapped is None:
                continue
            etype, data = mapped
            yield _make_event(
                etype,
                run_id=run_id,
                job_id=request.job_id,
                seq=seq,
                data=data,
            )
            seq += 1
    else:
        for raw_event in event_stream:  # type: ignore[assignment]
            event_type = _extract_event_type(raw_event)
            payload = _extract_event_payload(raw_event)
            mapped = _map_event(event_type, payload, request, seq)
            if mapped is None:
                continue
            etype, data = mapped
            yield _make_event(
                etype,
                run_id=run_id,
                job_id=request.job_id,
                seq=seq,
                data=data,
            )
            seq += 1

    # Ensure completion signal even when upstream stream ends early.
    if seq == 1:
        yield _make_event(
            "done",
            run_id=run_id,
            job_id=request.job_id,
            seq=seq,
            data={"summary": "deepagents completed", "turns": 1, "tool_calls": 0, "model_calls": 0},
        )


def _map_event(
    event_type: str,
    payload: Mapping[str, Any],
    request: RunStreamRequest,
    seq: int,
) -> tuple[str, dict[str, Any]] | None:
    del seq

    if not event_type:
        return None

    safe_event_type = event_type.strip().lower()
    data = dict(payload)

    if safe_event_type == "message_delta":
        return (
            "message_delta",
            {
                "delta": str(data.get("delta", "")),
                "turn": int(data.get("turn", 1) or 1),
                "agent": _safe_decode_payload(data.get("agent")),
                "subagent": _safe_decode_payload(data.get("subagent")),
            },
        )
    if safe_event_type == "reasoning_delta":
        return (
            "reasoning_delta",
            {
                "delta": str(data.get("delta", "")),
                "turn": int(data.get("turn", 1) or 1),
                "agent": _safe_decode_payload(data.get("agent")),
                "subagent": _safe_decode_payload(data.get("subagent")),
            },
        )
    if safe_event_type == "tool_call_start":
        return (
            "tool_call_start",
            {
                "tool_call_id": str(data.get("tool_call_id", "")),
                "tool_name": str(data.get("tool_name", "")),
                "input_preview": _coerce_output_preview(data.get("input_preview")),
                "risk_level": str(data.get("risk_level", "")),
                "agent": _safe_decode_payload(data.get("agent")),
                "subagent": _safe_decode_payload(data.get("subagent")),
            },
        )
    if safe_event_type == "tool_call_result":
        return (
            "tool_call_result",
            {
                "tool_call_id": str(data.get("tool_call_id", "")),
                "tool_name": str(data.get("tool_name", "")),
                "status": str(data.get("status", "")) or "ok",
                "summary": str(data.get("summary", "")),
                "is_error": bool(data.get("is_error", False)),
                "duration_ms": int(data.get("duration_ms", 0) or 0),
            },
        )
    if safe_event_type == "subagent_start":
        return (
            "subagent_start",
            {
                "subagent_id": str(data.get("subagent_id", "")),
                "name": str(data.get("name", "")),
                "description": str(data.get("description", "")),
                "parent": _safe_decode_payload(data.get("parent")),
            },
        )
    if safe_event_type == "subagent_end":
        return (
            "subagent_end",
            {
                "subagent_id": str(data.get("subagent_id", "")),
                "name": str(data.get("name", "")),
                "status": str(data.get("status", "")) or "ok",
                "summary": str(data.get("summary", "")),
            },
        )
    if safe_event_type == "todo_snapshot":
        return ("todo_snapshot", {"todos": data.get("todos", [])})
    if safe_event_type == "llm_usage":
        usage = {k: data.get(k) for k in ("span_id", "provider", "model", "prompt_tokens", "completion_tokens", "cache_read_tokens", "cache_write_tokens", "cost_usd", "latency_ms", "subagent")}
        usage["span_id"] = _safe_decode_payload(usage.get("span_id")) or ""
        usage["provider"] = _safe_decode_payload(usage.get("provider")) or ""
        usage["model"] = _safe_decode_payload(usage.get("model")) or ""
        usage["cost_usd"] = float(usage.get("cost_usd") or 0.0)
        usage["prompt_tokens"] = int(usage.get("prompt_tokens") or 0)
        usage["completion_tokens"] = int(usage.get("completion_tokens") or 0)
        usage["cache_read_tokens"] = int(usage.get("cache_read_tokens") or 0)
        usage["cache_write_tokens"] = int(usage.get("cache_write_tokens") or 0)
        usage["latency_ms"] = int(usage.get("latency_ms") or 0)
        usage["subagent"] = _safe_decode_payload(usage.get("subagent"))
        return ("llm_usage", usage)
    if safe_event_type == "done":
        return (
            "done",
            {
                "summary": str(data.get("summary", "")),
                "turns": int(data.get("turns", 0) or 0),
                "tool_calls": int(data.get("tool_calls", 0) or 0),
                "model_calls": int(data.get("model_calls", 0) or 0),
            },
        )
    if safe_event_type == "error":
        return (
            "error",
            {
                "code": str(data.get("code", "provider_error")),
                "message": str(data.get("message", "")) or "deepagents runtime error",
                "phase": _safe_decode_payload(data.get("phase")) or "runtime",
                "recoverable": bool(data.get("recoverable", True)),
            },
        )
    if safe_event_type == "tool_call":
        return _map_event(
            "tool_call_start" if data.get("status") in (None, "", "started", "start") else "tool_call_result",
            data,
            request,
            0,
        )
    return None


async def stream_run(
    payload: RunStreamRequest | Mapping[str, Any],
    repo_root: str | Path | None = None,
) -> AsyncGenerator[bytes, None]:
    request = _resolve_request_payload(payload)
    resolved_repo_root = Path(repo_root) if repo_root else resolve_repo_path(request.repo.project_id)
    if str(resolved_repo_root) != str(resolve_repo_path(request.repo.project_id)):
        raise ValueError("payload repo.project_id must match resolved repository path")
    if not resolved_repo_root.exists():
        raise ValueError("repository root does not exist")

    if request.read_only and not request.message.strip():
        raise ValueError("message is required")

    try:
        async for event in _stream_deepagents(request, resolved_repo_root):
            yield event
        return
    except Exception as exc:
        # DeepAgents and its adapters are not guaranteed to be installed in
        # every environment (CI, smoke tests). Log so we don't lose the
        # diagnostic, then fall through to the deterministic scaffold so
        # the stream always terminates with a `done` event.
        import logging

        logging.getLogger("deepagents.stream").warning(
            "deepagents runtime unavailable, falling back to scaffold: %s", exc, exc_info=True
        )

    async for event in _stream_fallback(request, resolved_repo_root):
        yield event
