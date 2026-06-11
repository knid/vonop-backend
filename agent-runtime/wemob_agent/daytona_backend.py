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

from wemob_agent.daytona import (
    DaytonaSDKProvider,
    daytona_workspace_path,
    normalize_daytona_workspace_root,
)


class DaytonaWorkspaceBackend(BaseSandbox):
    """Map DeepAgents' routed workspace paths onto Daytona's writable workspace."""

    def __init__(self, provider: DaytonaSDKProvider, daytona_root: str = "workspace") -> None:
        self.provider = provider
        self.daytona_root = normalize_daytona_workspace_root(daytona_root)
        self._helper_command_depth = 0

    @property
    def id(self) -> str:
        return self.provider.sandbox_id

    def execute(self, command: str, *, timeout: int | None = None) -> ExecuteResponse:
        result = self.provider.execute(command, self._command_cwd(), timeout or 1800)
        return ExecuteResponse(
            output=str(result.get("stdout") or result.get("output") or ""),
            exit_code=int(result.get("exit_code") or result.get("code") or 0),
            truncated=bool(result.get("truncated") or False),
        )

    def ls(self, path: str) -> LsResult:
        self._helper_command_depth += 1
        try:
            result = super().ls(self._remote(path))
        finally:
            self._helper_command_depth -= 1
        if result.entries is not None:
            for entry in result.entries:
                entry["path"] = self._logical(str(entry.get("path") or ""))
        return result

    def read(self, file_path: str, offset: int = 0, limit: int = 2000) -> ReadResult:
        self._helper_command_depth += 1
        try:
            return super().read(self._remote(file_path), offset=offset, limit=limit)
        finally:
            self._helper_command_depth -= 1

    def grep(self, pattern: str, path: str | None = None, glob: str | None = None) -> GrepResult:
        self._helper_command_depth += 1
        try:
            result = super().grep(pattern, self._remote(path or "/"), glob=glob)
        finally:
            self._helper_command_depth -= 1
        if result.matches is not None:
            for match in result.matches:
                match["path"] = self._logical(str(match.get("path") or ""))
        return result

    def glob(self, pattern: str, path: str | None = None) -> GlobResult:
        self._helper_command_depth += 1
        try:
            result = super().glob(pattern, self._remote(path or "/"))
        finally:
            self._helper_command_depth -= 1
        if result.matches is not None:
            for match in result.matches:
                match["path"] = self._logical(str(match.get("path") or ""))
        return result

    def write(self, file_path: str, content: str) -> WriteResult:
        self._helper_command_depth += 1
        try:
            result = super().write(self._remote(file_path), content)
        finally:
            self._helper_command_depth -= 1
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
        self._helper_command_depth += 1
        try:
            result = super().edit(self._remote(file_path), old_string, new_string, replace_all)
        finally:
            self._helper_command_depth -= 1
        if result.path is not None:
            result.path = file_path
        return result

    def upload_files(self, files: list[tuple[str, bytes]]) -> list[FileUploadResponse]:
        responses: list[FileUploadResponse] = []
        for path, content in files:
            try:
                if path.startswith("/tmp/") or self._is_remote(path):
                    remote_path = path
                else:
                    remote_path = self._remote(path)
                self.provider.upload_bytes(remote_path, content)
                responses.append(FileUploadResponse(path=path, error=None))
            except Exception as exc:
                responses.append(FileUploadResponse(path=path, error=str(exc)))
        return responses

    def download_files(self, paths: list[str]) -> list[FileDownloadResponse]:
        responses: list[FileDownloadResponse] = []
        for path in paths:
            try:
                remote_path = path if self._is_remote(path) else self._remote(path)
                content = self.provider.read_bytes(remote_path)
                responses.append(FileDownloadResponse(path=path, content=content, error=None))
            except Exception as exc:
                responses.append(FileDownloadResponse(path=path, content=None, error=str(exc)))
        return responses

    def _remote(self, path: str) -> str:
        return daytona_workspace_path(path, self.daytona_root)

    def _logical(self, path: str) -> str:
        normalized = path.rstrip("/")
        root = self.daytona_root.rstrip("/")
        if normalized == root:
            return "/"
        if root != "/" and normalized.startswith(root + "/"):
            suffix = normalized.removeprefix(root + "/")
            return f"/{suffix}" if suffix else "/"
        if root == "/" and normalized.startswith("/"):
            return normalized
        return path if path.startswith("/") else f"/{path}"

    def _command_cwd(self) -> str:
        if self._helper_command_depth > 0:
            return "/" if self.daytona_root.startswith("/") else "."
        return self.daytona_root

    def _is_remote(self, path: str) -> bool:
        root = self.daytona_root.rstrip("/")
        normalized = path.rstrip("/")
        return normalized == root or normalized.startswith(root + "/")
