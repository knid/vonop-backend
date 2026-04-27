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

worker:
	go build -o bin/worker cmd/worker/main.go

worker-dev:
	go run cmd/worker/main.go
.PHONY: worker worker-dev
