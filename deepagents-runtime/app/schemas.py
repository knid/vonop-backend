from __future__ import annotations

from pydantic import BaseModel, Field


class RepoInfo(BaseModel):
    mount_base: str
    project_id: str
    virtual_root: str = "/workspace"


class ModelSpec(BaseModel):
    provider: str
    model: str
    langchain_model: str | None = None


class Limits(BaseModel):
    max_wallclock_ms: int = Field(default=2700000, ge=1)
    max_model_calls: int = Field(default=80, ge=1)
    max_tool_calls: int = Field(default=200, ge=1)
    max_output_tokens_per_call: int = Field(default=64000, ge=1)


class ToolSpec(BaseModel):
    name: str
    description: str | None = None
    schema: dict = Field(default_factory=dict)
    risk_level: str | None = None


class SkillsSpec(BaseModel):
    paths: list[str] = Field(default_factory=list)
    ids: list[str] = Field(default_factory=list)


class Attachment(BaseModel):
    asset_id: str | None = None
    file_name: str | None = None
    content_type: str | None = None
    url: str | None = None
    is_image: bool | None = None


class RunStreamRequest(BaseModel):
    schema_version: str
    job_id: str
    run_id: str
    trace_id: str
    user_id: str
    organization_id: str
    project_id: str
    project_type: str
    frontend_runtime: str = "none"
    is_first_generation: bool = False
    read_only: bool = False
    message: str
    history: list[dict] = Field(default_factory=list)
    attachments: list[Attachment] = Field(default_factory=list)
    repo: RepoInfo
    model: ModelSpec
    limits: Limits = Field(default_factory=Limits)
    tools: list[ToolSpec] = Field(default_factory=list)
    skills: SkillsSpec = Field(default_factory=SkillsSpec)
    metadata: dict = Field(default_factory=dict)
