build:
	go build -o bin/app cmd/main.go

run: build
	./bin/app

dev:
	go run cmd/main.go

error-codes:
	go run ./scripts/gen-error-codes
.PHONY: error-codes
