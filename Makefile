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
