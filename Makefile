# artist-shop-be build system
# ---------------------------------------------------------------------------
# Pattern notes (borrowed from the conventions used across these projects):
#   * Every target group has its own help target, so `make help` stays short.
#   * Recipes are written for the HOST shell, so shell-specific syntax is
#     selected per OS via *_CMD variables instead of forcing bash. This keeps
#     the Makefile working on Windows cmd.exe (no Git Bash requirement).
#   * Never pipe `go test` through a filter: the exit code would become the
#     filter's (grep/findstr return 1 when every line is filtered) and a real
#     test failure would be masked.
#   * `tidy-check` uses `go mod tidy -diff` so it never mutates the tree.
#   * `make check` mirrors the CI pipeline in .github/workflows/ci.yml.
# ---------------------------------------------------------------------------

GO ?= go
GOLANGCI_LINT ?= golangci-lint
SWAG ?= swag
COMPOSE ?= docker compose
COMPOSE_DEV = $(COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml

BIN_DIR := bin
GO_BUILD_FLAGS := -trimpath -ldflags="-s -w"
GOLANGCI_LINT_VERSION ?= v2.14.0
SWAG_VERSION ?= v1.16.4

# Redirect to both: `2>/dev/null` for POSIX shells, `2>NUL` for Windows cmd.exe.
# Without the second form every make run on Windows prints
# "The system cannot find the path specified." and GIT_SHA falls back to "dev".
GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null 2>NUL || echo dev)

IMAGE_NAME ?= artist-shop-be

# Database credentials for the compose stack (keep in sync with .env).
# No password variables here on purpose: Redis auth comes from the container's
# own REDISCLI_AUTH env, so secrets never appear on a host command line.
DB_NAME ?= artist_shop
DB_USER ?= app

# Published host ports; must match the defaults in docker-compose*.yml.
API_PORT ?= 8080
DB_PORT ?= 5432
REDIS_PORT ?= 6379
MAILPIT_UI_PORT ?= 8025

ifeq ($(OS),Windows_NT)
GO_BIN_EXT := .exe
MKDIR_BIN_CMD := if not exist "$(subst /,\,$(BIN_DIR))" mkdir "$(subst /,\,$(BIN_DIR))"
CLEAN_BIN_CMD := if exist "$(subst /,\,$(BIN_DIR))" rmdir /S /Q "$(subst /,\,$(BIN_DIR))"
CLEAN_COVERAGE_CMD := if exist coverage.out del /Q coverage.out
FMT_CHECK_CMD := powershell -Command "$$files = gofmt -l .; if ($$files) { Write-Error ($$files -join [Environment]::NewLine); exit 1 }"
ENV_CMD := if exist .env (echo .env already exists; left untouched) else (copy /Y .env.example .env >NUL && echo created .env from .env.example)
else
GO_BIN_EXT :=
MKDIR_BIN_CMD := mkdir -p "$(BIN_DIR)"
CLEAN_BIN_CMD := rm -rf "$(BIN_DIR)"
CLEAN_COVERAGE_CMD := rm -f coverage.out
FMT_CHECK_CMD := test -z "$$(gofmt -l .)"
ENV_CMD := if [ -f .env ]; then echo ".env already exists; left untouched"; else cp .env.example .env && echo "created .env from .env.example"; fi
endif

# gofmt -w . recurses into every .go file on both platforms.
FMT_CMD := gofmt -w .

.DEFAULT_GOAL := help

.PHONY: help dev-help quality-help docker-help migration-help
help: ## Show this index
	@echo ================
	@echo ARTIST SHOP BE - BUILD SYSTEM
	@echo ================
	@echo Run one of the following to list targets in a group:
	@echo   make dev-help       : Development workflow
	@echo   make quality-help   : CI quality checks, tests, compilation
	@echo   make docker-help    : Docker Compose stack and image
	@echo   make migration-help : Database migration (goose) commands
	@echo Most used: make up | make up-tools | make check | make seed
	@echo ================

dev-help:
	@echo DEVELOPMENT:
	@echo   make env            : Create .env from .env.example if missing
	@echo   make run            : Run the API locally with 'go run'
	@echo   make seed           : Provision the admin account (local Go)
	@echo   make build          : Build api, migrate, seed into bin/
	@echo   make dev            : Build everything, ready to run
	@echo   make fmt            : Format all Go source files
	@echo   make tidy           : Tidy go.mod / go.sum
	@echo   make install-tools  : Install pinned golangci-lint
	@echo   make clean          : Remove bin/ and coverage.out

quality-help:
	@echo QUALITY (mirrors .github/workflows/ci.yml):
	@echo   make fmt-check      : Verify formatting without editing files
	@echo   make tidy-check     : Verify go.mod / go.sum are tidy (no writes)
	@echo   make vet            : go vet for unit and integration builds
	@echo   make lint           : golangci-lint (override GOLANGCI_LINT=path)
	@echo   make test           : Unit tests
	@echo   make test-race      : Unit tests with the race detector
	@echo   make test-integration : Integration tests (needs Docker)
	@echo   make coverage       : Unit test coverage summary
	@echo   make static-check   : fmt-check + tidy-check + vet
	@echo   make swagger        : Generate the OpenAPI spec into docs/swagger
	@echo   make swagger-check  : Verify docs/swagger matches the code annotations
	@echo   make check          : Everything CI runs, locally

docker-help:
	@echo DOCKER COMPOSE:
	@echo   make up             : Start db + redis + api (no host db/redis ports)
	@echo   make up-tools       : Same, plus host ports for db/redis and Mailpit
	@echo   make stop           : Stop containers, keep data
	@echo   make down           : Stop and remove containers, keep data
	@echo   make down-all       : Stop and DELETE volumes (wipes the database)
	@echo   make ps             : Container status
	@echo   make logs           : Follow API logs
	@echo   make rebuild        : Rebuild the image without cache, then start
	@echo   make db-shell       : psql inside the db container
	@echo   make redis-cli      : redis-cli inside the redis container
	@echo   make mail           : Print emails captured by Mailpit
	@echo   make seed-docker    : Provision the admin account in a container
	@echo   make docker-build   : Build the image and tag it artist-shop-be:<sha>

migration-help:
	@echo MIGRATIONS (local Go, needs DATABASE_URL):
	@echo   make migrate-up     : Apply pending migrations
	@echo   make migrate-down   : Roll back the last migration
	@echo   make migrate-status : Show applied/pending migrations
	@echo   make migrate-version: Print the current schema version
	@echo MIGRATIONS (inside Docker):
	@echo   make migrate-up-docker     : Apply migrations in a container
	@echo   make migrate-status-docker : Migration status in a container

# --- Development ------------------------------------------------------------

.PHONY: env
env: ## Create .env from .env.example if missing
	@$(ENV_CMD)

.PHONY: run
run: ## Run the API locally (go run)
	$(GO) run ./cmd/api

.PHONY: seed
seed: ## Provision the admin account using local Go
	$(GO) run ./cmd/seed

.PHONY: build
build: ## Build api, migrate and seed into bin/
	@echo Building binaries into $(BIN_DIR)/...
	@$(MKDIR_BIN_CMD)
	$(GO) build $(GO_BUILD_FLAGS) -o "$(BIN_DIR)/api$(GO_BIN_EXT)" ./cmd/api
	$(GO) build $(GO_BUILD_FLAGS) -o "$(BIN_DIR)/migrate$(GO_BIN_EXT)" ./cmd/migrate
	$(GO) build $(GO_BUILD_FLAGS) -o "$(BIN_DIR)/seed$(GO_BIN_EXT)" ./cmd/seed
	@echo Build succeeded: $(BIN_DIR)/api$(GO_BIN_EXT), migrate, seed

.PHONY: dev
dev: build ## Build everything and report how to run it
	@echo Ready. Run 'make run' (local Go) or 'make up' (Docker).

.PHONY: fmt
fmt: ## Format all Go source files
	@echo Formatting Go source files...
	@$(FMT_CMD)
	@echo Formatting finished.

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	@echo Tidying go.mod / go.sum...
	$(GO) mod tidy
	@echo Tidy finished.

.PHONY: install-tools
install-tools: ## Install the pinned golangci-lint used by CI
	@echo Installing golangci-lint $(GOLANGCI_LINT_VERSION)...
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@echo Installed. Ensure $$(go env GOPATH)/bin is on PATH.

.PHONY: install-swag
install-swag: ## Install the pinned swag generator used by make swagger
	@echo Installing swag $(SWAG_VERSION)...
	$(GO) install github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION)
	@echo Installed. Ensure $$(go env GOPATH)/bin is on PATH.

.PHONY: swagger
swagger: ## Generate the OpenAPI spec from code annotations into docs/swagger
	@echo Generating OpenAPI specification...
	$(SWAG) init -g cmd/api/main.go -o docs/swagger --parseInternal --parseDependency --outputTypes go,json,yaml
	@echo Generated docs/swagger/{docs.go,swagger.json,swagger.yaml}.

.PHONY: clean
clean: ## Remove bin/ and coverage.out
	@echo Cleaning build artifacts...
	@$(CLEAN_BIN_CMD)
	@$(CLEAN_COVERAGE_CMD)
	@echo Clean finished.

# --- Quality ----------------------------------------------------------------

.PHONY: fmt-check
fmt-check: ## Verify formatting without editing files
	@echo Checking Go source formatting...
	@$(FMT_CHECK_CMD)
	@echo Formatting is valid.

.PHONY: swagger-check
swagger-check: ## Verify docs/swagger is up to date with the code annotations
	@echo Regenerating the OpenAPI specification and checking for drift...
	$(SWAG) init -g cmd/api/main.go -o docs/swagger --parseInternal --parseDependency --outputTypes go,json,yaml
	git diff --exit-code -- docs/swagger
	@echo docs/swagger is up to date.

.PHONY: tidy-check
tidy-check: ## Verify go.mod / go.sum are tidy (does not modify files)
	@echo Checking whether go.mod / go.sum are tidy...
	$(GO) mod tidy -diff
	@echo go.mod / go.sum are tidy.

.PHONY: vet
vet: ## Run go vet for unit and integration builds
	@echo Running go vet...
	$(GO) vet ./...
	$(GO) vet -tags integration ./...
	@echo go vet finished.

.PHONY: lint
lint: ## Run golangci-lint (override with GOLANGCI_LINT=/path/to/binary)
	@echo Running golangci-lint...
	$(GOLANGCI_LINT) run
	@echo Lint finished.

.PHONY: static-check
static-check: fmt-check tidy-check vet ## Run fmt-check + tidy-check + vet

.PHONY: test
test: ## Run unit tests
	@echo Running unit tests...
	$(GO) test -count=1 ./...
	@echo Unit tests finished.

.PHONY: test-race
test-race: ## Run unit tests with the race detector (needs CGO_ENABLED=1 + gcc)
	@echo Running unit tests with the race detector...
ifeq ($(CGO_ENABLED_VALUE),1)
	$(GO) test -count=1 -race ./...
	@echo Race tests finished.
else
	@echo ERROR: the race detector requires CGO_ENABLED=1 and a C compiler.
	@echo        Install gcc (choco install mingw) or run this target in CI.
	@exit 1
endif

.PHONY: test-integration
test-integration: ## Run integration tests (needs Docker)
	@echo Running integration tests (Docker required)...
	$(GO) test -count=1 -p 1 -tags integration ./...
	@echo Integration tests finished.

.PHONY: coverage
coverage: ## Report unit test coverage
	$(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

# `go test -race` needs cgo and a C compiler. CI (ubuntu) has both; a Windows
# box without gcc reports CGO_ENABLED=0, so `make check` skips test-race there
# instead of failing. `make test-race` stays available and explains the problem.
CGO_ENABLED_VALUE := $(shell $(GO) env CGO_ENABLED)
ifeq ($(CGO_ENABLED_VALUE),1)
CHECK_TARGETS := static-check swagger-check lint test test-race test-integration build
else
CHECK_TARGETS := static-check swagger-check lint test test-integration build
endif

.PHONY: check
check: $(CHECK_TARGETS) ## Run the whole CI suite locally
ifeq ($(CGO_ENABLED_VALUE),1)
	@echo All checks passed (including test-race).
else
	@echo All checks passed. SKIPPED test-race: it needs CGO_ENABLED=1 and a C
	@echo compiler (gcc). Install gcc or run it in CI.
endif

# --- Migrations -------------------------------------------------------------

.PHONY: migrate-up
migrate-up: ## Apply pending migrations (local Go)
	$(GO) run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Roll back the last migration (local Go)
	$(GO) run ./cmd/migrate down

.PHONY: migrate-status
migrate-status: ## Show migration status (local Go)
	$(GO) run ./cmd/migrate status

.PHONY: migrate-version
migrate-version: ## Print the current schema version (local Go)
	$(GO) run ./cmd/migrate version

# --- Docker -----------------------------------------------------------------

.PHONY: up
up: env ## Start db + redis + api (builds the image)
	$(COMPOSE) up -d --build --wait
	@echo API: http://localhost:$(API_PORT)/healthz

.PHONY: up-tools
up-tools: env ## Start the stack with host ports for db/redis plus Mailpit
	$(COMPOSE_DEV) up -d --build --wait
	@echo API:      http://localhost:$(API_PORT)/healthz
	@echo Mailpit:  http://localhost:$(MAILPIT_UI_PORT)
	@echo Postgres: localhost:$(DB_PORT) (user $(DB_USER), db $(DB_NAME))
	@echo Redis:    localhost:$(REDIS_PORT)
	@echo For Mailpit set SMTP_HOST=mailpit and SMTP_PORT=1025 in .env

.PHONY: stop
stop: ## Stop containers, keep data
	$(COMPOSE_DEV) stop

.PHONY: down
down: ## Remove containers and networks, keep volumes
	$(COMPOSE_DEV) down

.PHONY: down-all
down-all: ## Remove containers and volumes (DELETES the database)
	$(COMPOSE_DEV) down -v

.PHONY: ps
ps: ## Show container status
	$(COMPOSE_DEV) ps

.PHONY: logs
logs: ## Follow API logs
	$(COMPOSE) logs -f api

.PHONY: rebuild
rebuild: ## Rebuild the image without cache, then start
	$(COMPOSE_DEV) build --no-cache api
	$(COMPOSE_DEV) up -d --wait

.PHONY: docker-build
docker-build: ## Build the image and tag it artist-shop-be:<git-sha>
	docker build -t $(IMAGE_NAME):$(GIT_SHA) -t $(IMAGE_NAME):latest .

.PHONY: db-shell
db-shell: ## Open psql inside the db container
	$(COMPOSE_DEV) exec db psql -U $(DB_USER) -d $(DB_NAME)

.PHONY: redis-cli
redis-cli: ## Open redis-cli inside the redis container (auth comes from the container env)
	$(COMPOSE_DEV) exec redis redis-cli

.PHONY: mail
mail: ## Print emails captured by Mailpit (requires make up-tools)
	curl -s http://localhost:$(MAILPIT_UI_PORT)/api/v1/messages

.PHONY: seed-docker
seed-docker: ## Provision the admin account in a container
	$(COMPOSE) run --rm seed

.PHONY: migrate-up-docker
migrate-up-docker: ## Apply migrations in a container
	$(COMPOSE) run --rm migrate up

.PHONY: migrate-status-docker
migrate-status-docker: ## Show migration status in a container
	$(COMPOSE) run --rm migrate status
