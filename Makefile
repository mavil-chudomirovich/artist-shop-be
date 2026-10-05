.DEFAULT_GOAL := help

GO ?= go
GOLANGCI_LINT ?= golangci-lint

# Recipes are POSIX sh; Git Bash is used on Windows because the default shell
# there (cmd.exe) cannot run them.
ifeq ($(OS),Windows_NT)
SHELL := C:/Program Files/Git/bin/bash.exe
else
SHELL := /bin/bash
endif

.PHONY: help
help: ## List available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the API locally
	$(GO) run ./cmd/api

.PHONY: seed
seed: ## Provision the admin account
	$(GO) run ./cmd/seed

.PHONY: build
build: ## Build api, migrate and seed binaries into bin/
	$(GO) build -o bin/api ./cmd/api
	$(GO) build -o bin/migrate ./cmd/migrate
	$(GO) build -o bin/seed ./cmd/seed

.PHONY: fmt
fmt: ## Format all Go files
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail when any Go file is unformatted
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		echo "unformatted files:"; echo "$$files"; exit 1; \
	fi

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

.PHONY: tidy-check
tidy-check: ## Fail when go.mod / go.sum are not tidy
	$(GO) mod tidy
	@git diff --exit-code -- go.mod go.sum

.PHONY: vet
vet: ## Run go vet for unit and integration builds
	$(GO) vet ./...
	$(GO) vet -tags integration ./...

.PHONY: lint
lint: ## Run golangci-lint (override with GOLANGCI_LINT=/path/to/binary)
	$(GOLANGCI_LINT) run

.PHONY: static-check
static-check: fmt-check tidy-check vet ## Run all static checks

.PHONY: test
test: ## Run unit tests
	$(GO) test -count=1 ./...

.PHONY: test-integration
test-integration: ## Run integration tests (needs Docker)
	$(GO) test -count=1 -p 1 -tags integration ./...

.PHONY: test-race
test-race: ## Run unit tests with the race detector
	$(GO) test -count=1 -race ./...

.PHONY: coverage
coverage: ## Report unit test coverage
	$(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: migrate-up
migrate-up: ## Apply migrations
	$(GO) run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Roll back the last migration
	$(GO) run ./cmd/migrate down

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(GO) run ./cmd/migrate status

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin coverage.out