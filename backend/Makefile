.DEFAULT_GOAL := help

BINARY      := api
BIN_DIR     := bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)

# .PHONY marks targets that are commands rather than files, so make does not
# skip them when a file of the same name happens to exist.
.PHONY: help run build test test-race test-integration test-cover lint fmt vet tidy clean \
        docker-build up down logs ps restart \
        migrate-up migrate-down migrate-reset migrate-version migrate-force migrate-create

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
	 awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

run: ## Run the API locally against .env
	go run ./cmd/api -env .env

build: ## Compile the API and migrate binaries into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/api
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/migrate ./cmd/migrate

# ---------- Migrations ----------
# golang-migrate is driven through our own CLI rather than the upstream binary,
# so migrations use the same config loader, DSN and logger as the application.

migrate-up: ## Apply all pending migrations
	go run ./cmd/migrate -env .env up

migrate-down: ## Roll back one migration (override with STEPS=n)
	go run ./cmd/migrate -env .env down -steps $(or $(STEPS),1)

migrate-reset: ## Roll back every migration (destructive)
	go run ./cmd/migrate -env .env down -all -confirm

migrate-version: ## Print the current schema version; fails if dirty
	go run ./cmd/migrate -env .env version

migrate-force: ## Clear a dirty state after manual repair (VERSION=n)
	@test -n "$(VERSION)" || (echo "usage: make migrate-force VERSION=n"; exit 2)
	go run ./cmd/migrate -env .env force -version $(VERSION) -confirm

migrate-create: ## Scaffold a migration pair (NAME=create_foo_table)
	@test -n "$(NAME)" || (echo "usage: make migrate-create NAME=create_foo_table"; exit 2)
	go run ./cmd/migrate create -name $(NAME)

test: ## Run the test suite
	go test -count=1 ./...

# The race detector needs cgo and therefore a C toolchain, which a stock
# Windows dev box does not have. CI runs this target; developers run `test`.
test-race: ## Run the test suite with the race detector (requires gcc)
	CGO_ENABLED=1 go test -race -count=1 ./...

# Repository tests need a live PostgreSQL with the migrations applied. They are
# behind a build tag so `make test` stays infrastructure-free.
#
# -p 1 runs one PACKAGE at a time. Go parallelises across packages by default,
# and every integration package truncates the same tables to get a clean slate —
# so in parallel they delete each other's fixtures mid-test and fail with
# spurious foreign-key and not-found errors. Serialising is the standard fix for
# suites that share one database.
test-integration: ## Run repository tests against a live database (needs make up + migrate-up)
	go test -tags integration -count=1 -p 1 ./...

test-cover: ## Run tests and open the coverage report
	go test -count=1 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

lint: ## Run golangci-lint (install: https://golangci-lint.run)
	golangci-lint run ./...

fmt: ## Format all Go source
	gofmt -s -w .

vet: ## Run go vet
	go vet ./...

tidy: ## Sync go.mod and go.sum
	go mod tidy

clean: ## Remove build artefacts
	rm -rf $(BIN_DIR) coverage.out

docker-build: ## Build the production image
	docker build --build-arg VERSION=$(VERSION) -t wms-saas/api:$(VERSION) .

# The compose file lives at the repository root, one level above backend/.
up: ## Start the full local stack
	docker compose -f ../docker-compose.yml up -d --build

down: ## Stop the stack and remove volumes
	docker compose -f ../docker-compose.yml down -v

logs: ## Tail API logs
	docker compose -f ../docker-compose.yml logs -f api

ps: ## Show stack status
	docker compose -f ../docker-compose.yml ps

restart: ## Rebuild and restart the API only
	docker compose -f ../docker-compose.yml up -d --build api
