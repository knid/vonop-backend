
from __future__ import annotations

from pathlib import Path
from typing import Iterable

from .schemas import RunStreamRequest

SKILLS_ROOT = Path(__file__).resolve().parents[1] / "skills"
MAX_SKILLS = 18

CORE_SKILLS = (
    "/skills/core/wemob-builder-foundation",
    "/skills/core/security-review",
    "/skills/core/verification-loop",
    "/skills/core/coding-standards",
)

WEB_SKILLS = (
    "/skills/web/web-builder-foundation",
    "/skills/web/nextjs-app-router",
    "/skills/web/tailwind-v4",
    "/skills/web/frontend-patterns",
)

MOBILE_SKILLS = (
    "/skills/mobile/mobile-builder-foundation",
    "/skills/mobile/expo-router",
    "/skills/mobile/nativewind-ui",
)

BACKEND_SKILLS = (
    "/skills/backend/api-design",
    "/skills/backend/postgres-patterns",
)

WORKFLOW_SKILLS = (
    "/skills/workflows/premium-saas-landing-page",
    "/skills/workflows/dashboard-app",
    "/skills/workflows/database-backed-app",
    "/skills/workflows/debugging-build-fix",
    "/skills/workflows/ecommerce-store",
)

KNOWN_SKILL_IDS = {Path(path).name: path for path in (
    *CORE_SKILLS,
    *WEB_SKILLS,
    *MOBILE_SKILLS,
    *BACKEND_SKILLS,
    *WORKFLOW_SKILLS,
)}


def _candidate_exists(path: str) -> bool:
    if not path.startswith("/skills/"):
        return False
    candidate = SKILLS_ROOT / path.removeprefix("/skills/").lstrip("/")
    return candidate.is_dir() and (candidate / "SKILL.md").is_file()


def _normalize_explicit_path(path: str) -> str | None:
    if not path:
        return None
    normalized = Path(path.strip()).as_posix()
    if normalized.startswith("skills/"):
        normalized = f"/{normalized}"
    if normalized.startswith("/"):
        if not normalized.startswith("/skills/"):
            return None
        return f"/{Path(normalized).as_posix().strip('/')}" if normalized != "/skills/" else None
    return f"/skills/{normalized.strip('/')}"


def resolve_skill_paths(requested_paths: Iterable[str] | None, requested_ids: Iterable[str] | None) -> list[str]:
    """Return requested skill paths that exist in the bundled skills tree."""

    normalized: list[str] = []
    seen: set[str] = set()

    for path in requested_paths or ():
        normalized_path = _normalize_explicit_path(path)
        if not normalized_path:
            continue
        if normalized_path in seen:
            continue
        if _candidate_exists(normalized_path):
            seen.add(normalized_path)
            normalized.append(normalized_path)

    for sid in requested_ids or ():
        sid_key = (sid or "").strip()
        if not sid_key:
            continue
        candidate = KNOWN_SKILL_IDS.get(sid_key)
        if candidate and candidate not in seen and _candidate_exists(candidate):
            seen.add(candidate)
            normalized.append(candidate)

    return normalized[:MAX_SKILLS]


def _normalize_keyword_text(message: str) -> str:
    return (message or "").strip().lower()


def resolve_skills(payload: RunStreamRequest) -> list[str]:
    """Return context-aware default skills for the request.

    No external control plane is used in PR5. We keep this deterministic and
    bounded by MAX_SKILLS for predictable DeepAgents startup cost.
    """

    message = _normalize_keyword_text(payload.message)
    selected: list[str] = list(CORE_SKILLS)
    seen = set(selected)

    runtime = str(payload.project_type or "").lower()
    frontend_runtime = str(payload.frontend_runtime or "").lower()

    if runtime in {"app", "ecommerce", "web"} or frontend_runtime in {"nextjs", "web"}:
        for skill in WEB_SKILLS:
            if skill not in seen:
                selected.append(skill)
                seen.add(skill)

    if runtime in {"mobile_app_native", "mobile"} or frontend_runtime in {"mobile", "expo", "react_native"}:
        for skill in MOBILE_SKILLS:
            if skill not in seen:
                selected.append(skill)
                seen.add(skill)

    if runtime in {"ecommerce", "app"} and any(k in message for k in {"landing", "homepage", "marketing", "waitlist", "crm", "saas"}):
        for skill in ("/skills/web/frontend-design-direction", "/skills/workflows/premium-saas-landing-page"):
            if skill not in seen:
                selected.append(skill)
                seen.add(skill)

    if any(k in message for k in {"api", "auth", "sql", "database", "backend", "store", "gateway", "route", "ai", "setup"}):
        for skill in BACKEND_SKILLS:
            if skill not in seen:
                selected.append(skill)
                seen.add(skill)
        if "/skills/workflows/database-backed-app" not in seen:
            selected.append("/skills/workflows/database-backed-app")
            seen.add("/skills/workflows/database-backed-app")

    if any(k in message for k in {"build", "deploy", "error", "fix", "log", "sandbox", "tests", "test", "trace"}):
        for skill in ("/skills/workflows/debugging-build-fix", "/skills/core/security-review"):
            if skill not in seen:
                selected.append(skill)
                seen.add(skill)

    if any(k in message for k in {"dashboard"}) and "/skills/workflows/dashboard-app" not in seen:
        selected.append("/skills/workflows/dashboard-app")
        seen.add("/skills/workflows/dashboard-app")

    if runtime == "ecommerce" and "/skills/workflows/ecommerce-store" not in seen:
        selected.append("/skills/workflows/ecommerce-store")
        seen.add("/skills/workflows/ecommerce-store")

    # Keep deterministically bounded while preserving required core skills first.
    return selected[:MAX_SKILLS]
