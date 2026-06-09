from __future__ import annotations

from fastapi import HTTPException, Request, status

from .config import require_internal_token


class InternalAuth:
    """Simple bearer-token guard for internal-only endpoints."""

    def __call__(self, request: Request) -> None:
        expected = require_internal_token()
        authorization = request.headers.get("authorization")
        if not authorization:
            raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="missing authorization")

        if not authorization.startswith("Bearer "):
            raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid authorization format")

        if authorization[7:] != expected:
            raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid internal token")
        return None
