"""Deep Agents backend adapter for Daytona workspaces."""

from __future__ import annotations

from deepagents.backends.protocol import (
    EditResult,
    ExecuteResponse,
    FileDownloadResponse,
    FileUploadResponse,
    GlobResult,
    GrepResult,
    LsResult,
    ReadResult,
    WriteResult,
)
from deepagents.backends.sandbox import BaseSandbox

from wemob_agent.daytona import DaytonaSDKProvider, daytona_workspace_path


class DaytonaWorkspaceBackend(BaseSandbox):
    """Map DeepAgents' routed workspace paths onto Daytona's writable workspace."""

    def __init__(self, provider: DaytonaSDKProvider, daytona_root: str = "workspace") -> None:
        self.provider = provider
        self.daytona_root = daytona_root.strip("/") or "workspace"

    @property
    def id(self) -> str:
        return self.provider.sandbox_id

    def execute(self, command: str, *, timeout: int | None = None) -> ExecuteResponse:
        result = self.provider.execute(command, self.daytona_root, timeout or 1800)
        return ExecuteResponse(
            output=str(result.get("stdout") or result.get("output") or ""),
            exit_code=int(result.get("exit_code") or result.get("code") or 0),
            truncated=bool(result.get("truncated") or False),
        )

    def ls(self, path: str) -> LsResult:
        result = super().ls(self._remote(path))
        if result.entries is not None:
            for entry in result.entries:
                entry["path"] = self._logical(str(entry.get("path") or ""))
        return result

    def read(self, file_path: str, offset: int = 0, limit: int = 2000) -> ReadResult:
        return super().read(self._remote(file_path), offset=offset, limit=limit)

    def grep(self, pattern: str, path: str | None = None, glob: str | None = None) -> GrepResult:
        result = super().grep(pattern, self._remote(path or "/"), glob=glob)
        if result.matches is not None:
            for match in result.matches:
                match["path"] = self._logical(str(match.get("path") or ""))
        return result

    def glob(self, pattern: str, path: str | None = None) -> GlobResult:
        result = super().glob(pattern, self._remote(path or "/"))
        if result.matches is not None:
            for match in result.matches:
                match["path"] = self._logical(str(match.get("path") or ""))
        return result

    def write(self, file_path: str, content: str) -> WriteResult:
        result = super().write(self._remote(file_path), content)
        if result.path is not None:
            result.path = file_path
        return result

    def edit(
        self,
        file_path: str,
        old_string: str,
        new_string: str,
        replace_all: bool = False,
    ) -> EditResult:
        result = super().edit(self._remote(file_path), old_string, new_string, replace_all)
        if result.path is not None:
            result.path = file_path
        return result

    def upload_files(self, files: list[tuple[str, bytes]]) -> list[FileUploadResponse]:
        responses: list[FileUploadResponse] = []
        for path, content in files:
            try:
                remote_path = path if path.startswith("/tmp/") else self._remote(path)
                self.provider.upload_bytes(remote_path, content)
                responses.append(FileUploadResponse(path=path, error=None))
            except Exception as exc:
                responses.append(FileUploadResponse(path=path, error=str(exc)))
        return responses

    def download_files(self, paths: list[str]) -> list[FileDownloadResponse]:
        responses: list[FileDownloadResponse] = []
        for path in paths:
            try:
                content = self.provider.read_bytes(self._remote(path))
                responses.append(FileDownloadResponse(path=path, content=content, error=None))
            except Exception as exc:
                responses.append(FileDownloadResponse(path=path, content=None, error=str(exc)))
        return responses

    def _remote(self, path: str) -> str:
        return daytona_workspace_path(path, self.daytona_root)

    def _logical(self, path: str) -> str:
        normalized = path.strip("/")
        root = self.daytona_root.strip("/")
        if normalized == root:
            return "/"
        if normalized.startswith(root + "/"):
            suffix = normalized.removeprefix(root + "/")
            return f"/{suffix}" if suffix else "/"
        return path if path.startswith("/") else f"/{path}"
