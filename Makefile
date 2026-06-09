build:
	go build -o bin/app cmd/main.go

run: build
	./bin/app

dev:
	go run cmd/main.go

error-codes:
	go run ./scripts/gen-error-codes
.PHONY: error-codes

lint-errors:
	@if grep -rn "JSONErrorResponse\|JSONCodedErrorResponse\|JSONResponse(w, map\[string\]string{\"error\":" internal/ cmd/; then \
		echo "ERROR: deprecated error helpers detected. Use utils.JSONError / utils.JSONErrorCode / utils.JSONInternalError."; \
		exit 1; \
	fi
.PHONY: lint-errors

lint-events:
	@./scripts/check-event-literals.sh
.PHONY: lint-events

lint: lint-errors lint-events
.PHONY: lint

worker:
	go build -o bin/worker cmd/worker/main.go

worker-dev:
	go run cmd/worker/main.go
.PHONY: worker worker-dev

loadtest-publish:
	go build -o bin/loadtest-publish cmd/loadtest-publish/main.go
.PHONY: loadtest-publish

test-integration:
	go test -tags=integration ./...
.PHONY: test-integration

# Build + push the wemob-base-nextjs base image used as the FROM root for
# customer Next.js Dockerfiles. The base image pre-warms /root/.npm with the
# most common seed deps so customer `npm install --prefer-offline` resolves
# locally instead of hitting the registry. Run this whenever
# internal/templates/base-nextjs/package.json changes or a new dep enters
# common rotation. One-time prerequisite:
#   flyctl apps create wemob-base-nextjs --org <wemob-org>
base-image:
	cd internal/templates/base-nextjs && \
	flyctl deploy --app wemob-base-nextjs --remote-only --build-only --push --image-label v1
.PHONY: base-image

# --- DeepAgents Python runtime targets ---

test-python:
	cd deepagents-runtime && python -m pytest
.PHONY: test-python

lint-python:
	cd deepagents-runtime && python -m ruff check app tests
.PHONY: lint-python

fmt-python:
	cd deepagents-runtime && python -m ruff format app tests
.PHONY: fmt-python

build-runtime:
	docker build -t wemob-deepagents-runtime deepagents-runtime
.PHONY: build-runtime

smoke-runtime:
	@if [ -z "$(PROJECT_ID)" ]; then echo "Usage: make smoke-runtime PROJECT_ID=<uuid>"; exit 1; fi
	@SMOKE_UUID=$$(uuidgen); \
	curl -fsS -N -H "Authorization: Bearer $$(grep DEEPAGENTS_INTERNAL_TOKEN .env 2>/dev/null | cut -d= -f2 || echo smoke-token)" \
		-H "Content-Type: application/json" \
		-d "{\"schema_version\":\"2026-06-09\",\"job_id\":\"$$SMOKE_UUID\",\"run_id\":\"$$SMOKE_UUID\",\"trace_id\":\"$$SMOKE_UUID\",\"user_id\":\"00000000-0000-0000-0000-000000000000\",\"organization_id\":\"00000000-0000-0000-0000-000000000000\",\"project_id\":\"$(PROJECT_ID)\",\"project_type\":\"app\",\"is_first_generation\":false,\"read_only\":true,\"message\":\"smoke\",\"repo\":{\"mount_base\":\"/mnt/wemob-repos\",\"project_id\":\"$(PROJECT_ID)\",\"virtual_root\":\"/workspace\"},\"model\":{\"provider\":\"anthropic\",\"model\":\"claude-sonnet-4\",\"langchain_model\":\"\"},\"limits\":{\"max_wallclock_ms\":5000,\"max_model_calls\":1,\"max_tool_calls\":0,\"max_output_tokens_per_call\":100},\"tools\":[],\"skills\":{\"paths\":[],\"ids\":[]},\"metadata\":{}}" \
		http://localhost:8000/internal/v1/runs/stream
.PHONY: smoke-runtime
