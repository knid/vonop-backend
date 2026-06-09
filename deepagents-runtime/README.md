# deepagents-runtime

Scaffold for Wemob Python deepagents runtime.

## Runtime endpoints

- `GET /healthz` -> readiness ping.
- `GET /readyz` -> dependency check.
- `POST /internal/v1/runs/stream` -> NDJSON event stream.
- `POST /internal/v1/runs/{run_id}/cancel` -> request cooperative stream cancellation.

## Running locally

```bash
cd deepagents-runtime
export DEEPAGENTS_INTERNAL_TOKEN=dev-token
export WEMOB_REPOS_BASE=/mnt/wemob-repos
uvicorn app.main:create_app --factory --reload --port 8000
```
