# Makefile for Go + HTMX + Tailwind project
# ============================================

SHELL := /bin/bash

# Variables
APP_NAME       = mywebapp
BINARY_NAME    = bin/$(APP_NAME)
GO_MAIN        = cmd/api/main.go
GO_FILES       = $(shell find . -name '*.go' -type f -not -path "./vendor/*" -not -path "./.git/*" -not -path "./bin/*")
MIGRATIONS_DIR = migrations

# Version metadata (injected via -ldflags -X)
PKG := mywebapp/internal/version

VERSION := $(shell git describe --tags --exact-match HEAD 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo "")
BRANCH  := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
STATE   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X $(PKG).Version=$(VERSION) \
           -X $(PKG).GitCommit=$(COMMIT) \
           -X $(PKG).GitBranch=$(BRANCH) \
           -X $(PKG).BuildDate=$(DATE) \
           -X $(PKG).GitState=$(STATE)

# Load .env file and export variables
ifneq (,$(wildcard .env))
    include .env
    export
endif

# Strip surrounding quotes from DB_NAME, if present.
DB_NAME_CLEAN = $(shell printf '%s' '$(DB_NAME)' | tr -d '"'"'")
TEST_DB_NAME_CLEAN = $(shell printf '%s' '$(TEST_DB_NAME)' | tr -d '"'"'")

# Production database URL
DB_URL = postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME_CLEAN)?sslmode=$(DB_SSLMODE)

# Test database URL
TEST_DB_URL = postgres://$(TEST_DB_USER):$(TEST_DB_PASSWORD)@$(TEST_DB_HOST):$(TEST_DB_PORT)/$(TEST_DB_NAME_CLEAN)?sslmode=$(TEST_DB_SSLMODE)

# Port configuration
PORT     ?= 8080
PID_FILE  = .pid

# ANSI colours
RED     := \033[0;31m
GREEN   := \033[0;32m
YELLOW  := \033[1;33m
BLUE    := \033[0;34m
CYAN    := \033[0;36m
MAGENTA := \033[0;35m
NC      := \033[0m

# ============================================
# Help
# ============================================

.PHONY: help
help: ## Show this help message
	@printf "$(BLUE)Go HTMX App - Available Commands$(NC)\n\n"
	@printf "$(GREEN)Development:$(NC)\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## .*$$/ {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\n$(GREEN)Database:$(NC)\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## .*$$/ && ($$1 ~ /^(migrate|seed|db-)/) {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\n$(GREEN)Test database:$(NC)\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## .*$$/ && ($$1 ~ /^test-db-/) {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\n$(GREEN)Docker:$(NC)\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## .*$$/ && ($$1 ~ /^docker-/) {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\n$(GREEN)Testing & Quality:$(NC)\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## .*$$/ && ($$1 ~ /^(test|fmt|lint|coverage)/) {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\n"

# ============================================
# Application Management
# ============================================

.PHONY: run
run: ## Run the application
	@printf "$(GREEN)🚀 Running application from $(GO_MAIN)...$(NC)\n"
	go run $(GO_MAIN)

.PHONY: start
start: build ## Start the application in background
	@printf "$(GREEN)🚀 Starting application on port $(PORT)...$(NC)\n"
	@if [ -f $(PID_FILE) ] && kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
		printf "$(YELLOW)⚠️  Application is already running (PID: %s)$(NC)\n" "$$(cat $(PID_FILE))"; \
		exit 1; \
	fi
	@mkdir -p logs
	@PORT=$(PORT) nohup $(BINARY_NAME) > logs/app.log 2>&1 & echo $$! > $(PID_FILE)
	@printf "$(GREEN)✅ Application started (PID: %s)$(NC)\n" "$$(cat $(PID_FILE))"
	@printf "$(GREEN)📍 http://localhost:$(PORT)$(NC)\n"

.PHONY: stop
stop: ## Stop the background application
	@printf "$(YELLOW)🛑 Stopping application...$(NC)\n"
	@PID=""; \
	if [ -f $(PID_FILE) ]; then PID=$$(cat $(PID_FILE)); fi; \
	if [ -z "$$PID" ] || ! kill -0 $$PID 2>/dev/null; then \
		PID=$$(lsof -ti :$(PORT) 2>/dev/null || true); \
	fi; \
	if [ -z "$$PID" ]; then \
		printf "$(YELLOW)⚠️  No application found on port $(PORT)$(NC)\n"; \
		rm -f $(PID_FILE); \
		exit 0; \
	fi; \
	printf "$(YELLOW)   sending SIGTERM to PID %s$(NC)\n" "$$PID"; \
	kill -TERM $$PID 2>/dev/null || true; \
	for i in 1 2 3 4 5 6 7 8 9 10; do \
		if ! kill -0 $$PID 2>/dev/null; then \
			printf "$(GREEN)✅ Application stopped (PID: %s)$(NC)\n" "$$PID"; \
			rm -f $(PID_FILE); \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	printf "$(RED)⚠️  PID %s did not exit after 10s — sending SIGKILL$(NC)\n" "$$PID"; \
	kill -9 $$PID 2>/dev/null || true; \
	rm -f $(PID_FILE); \
	printf "$(GREEN)✅ Application force-killed$(NC)\n"

.PHONY: restart
restart: stop start ## Restart the application

.PHONY: status
status: ## Check if the application is running
	@if [ -f $(PID_FILE) ] && kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
		printf "$(GREEN)✅ Application is running (PID: %s)$(NC)\n" "$$(cat $(PID_FILE))"; \
		printf "$(GREEN)📍 http://localhost:$(PORT)$(NC)\n"; \
	else \
		printf "$(RED)❌ Application is not running$(NC)\n"; \
	fi

.PHONY: logs
logs: ## Tail the application logs
	@if [ -f logs/app.log ]; then \
		tail -f logs/app.log; \
	else \
		printf "$(YELLOW)⚠️  No log file found. Run 'make start' first.$(NC)\n"; \
	fi

.PHONY: clean
clean: stop ## Clean build artifacts
	@printf "$(YELLOW)🧹 Cleaning...$(NC)\n"
	rm -rf bin/
	rm -rf node_modules/
	rm -f static/css/output.css
	rm -f $(PID_FILE)
	go clean -modcache
	@printf "$(GREEN)✅ Clean complete!$(NC)\n"

# ============================================
# Development
# ============================================

.PHONY: dev
dev: build-css ## Run in development mode with CSS watch in background
	@printf "$(GREEN)🔧 Starting development mode...$(NC)\n"
	@mkdir -p logs
	@printf "$(YELLOW)Starting CSS watch in background...$(NC)\n"
	@npm run watch:css > logs/css.log 2>&1 &
	@printf "$(GREEN)✅ CSS watching started!$(NC)\n"
	@printf "$(YELLOW)Starting Go application with hot reload...$(NC)\n"
	@if command -v air > /dev/null; then \
		air -d; \
	else \
		printf "$(YELLOW)⚠️  Air not installed. Installing...$(NC)\n"; \
		go install github.com/air-verse/air@latest; \
		air -d; \
	fi

.PHONY: build
build: build-css ## Build the application for production
	@printf "$(GREEN)🔨 Building application...$(NC)\n"
	@printf "$(YELLOW)   version: $(VERSION)$(NC)\n"
	@printf "$(YELLOW)   commit:  $(COMMIT)$(NC)\n"
	@printf "$(YELLOW)   branch:  $(BRANCH)$(NC)\n"
	@printf "$(YELLOW)   state:   $(STATE)$(NC)\n"
	@printf "$(YELLOW)   built:   $(DATE)$(NC)\n"
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) $(GO_MAIN)
	@printf "$(GREEN)✅ Build complete! Binary: $(BINARY_NAME)$(NC)\n"

.PHONY: build-css
build-css: ## Build Tailwind CSS
	@printf "$(GREEN)🎨 Building CSS...$(NC)\n"
	npm run build:css
	@printf "$(GREEN)✅ CSS build complete!$(NC)\n"

.PHONY: watch-css
watch-css: ## Watch CSS changes
	@printf "$(GREEN)👀 Watching CSS...$(NC)\n"
	npm run watch:css

# ============================================
# Version
# ============================================

.PHONY: version
version: ## Show the version metadata that will be injected by 'make build'
	@printf "$(BLUE)Version metadata:$(NC)\n"
	@printf "  version: %s\n" "$(VERSION)"
	@printf "  commit:  %s\n" "$(COMMIT)"
	@printf "  branch:  %s\n" "$(BRANCH)"
	@printf "  state:   %s\n" "$(STATE)"
	@printf "  date:    %s\n" "$(DATE)"
	@printf "  pkg:     %s\n" "$(PKG)"

.PHONY: version-run
version-run: ## Query /version on the running server
	@printf "$(GREEN)🔎 Querying /version on port $(PORT)...$(NC)\n"
	@curl -sS "http://localhost:$(PORT)/version" | (command -v jq >/dev/null && jq . || cat)
	@printf "\n"

# ============================================
# Dependencies
# ============================================

.PHONY: install
install: ## Install all dependencies
	@printf "$(GREEN)📦 Installing dependencies...$(NC)\n"
	@printf "$(YELLOW)Installing Go dependencies...$(NC)\n"
	go mod download
	go mod tidy
	@printf "$(YELLOW)Installing Node dependencies...$(NC)\n"
	npm install
	@printf "$(YELLOW)Installing development tools...$(NC)\n"
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/air-verse/air@latest
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	@printf "$(GREEN)✅ All dependencies installed!$(NC)\n"

.PHONY: generate
generate: ## Generate SQLC code
	@printf "$(GREEN)⚙️  Generating SQLC code...$(NC)\n"
	@if [ -f "sqlc/sqlc.yml" ]; then \
		printf "$(GREEN)✅ Found sqlc/sqlc.yml$(NC)\n"; \
		printf "$(YELLOW)Running sqlc from sqlc/ directory...$(NC)\n"; \
		cd sqlc && sqlc generate -f sqlc.yml; \
	elif [ -f "sqlc/sqlc.yaml" ]; then \
		printf "$(GREEN)✅ Found sqlc/sqlc.yaml$(NC)\n"; \
		printf "$(YELLOW)Running sqlc from sqlc/ directory...$(NC)\n"; \
		cd sqlc && sqlc generate -f sqlc.yaml; \
	else \
		printf "$(RED)❌ No sqlc configuration found in sqlc/ directory!$(NC)\n"; \
		printf "$(YELLOW)Looking for: sqlc/sqlc.yml or sqlc/sqlc.yaml$(NC)\n"; \
		exit 1; \
	fi
	@printf "$(GREEN)✅ SQLC generation complete!$(NC)\n"
	@printf "$(GREEN)📁 Generated files in internal/repository/$(NC)\n"

# ============================================
# Database
# ============================================

.PHONY: migrate-up
migrate-up: ## Run database migrations up
	@printf "$(GREEN)📊 Running migrations up on database $(DB_NAME_CLEAN)...$(NC)\n"
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" up
	@printf "$(GREEN)✅ Migrations complete!$(NC)\n"

.PHONY: migrate-down
migrate-down: ## Run database migrations down
	@printf "$(YELLOW)📊 Running migrations down on database $(DB_NAME_CLEAN)...$(NC)\n"
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" down
	@printf "$(GREEN)✅ Migrations rolled back!$(NC)\n"

.PHONY: migrate-create
migrate-create: ## Create a new migration (usage: make migrate-create name=create_users_table)
	@if [ -z "$(name)" ]; then \
		printf "$(RED)❌ Missing required argument: name=<migration_name>$(NC)\n"; \
		printf "$(YELLOW)Usage: make migrate-create name=create_users_table$(NC)\n"; \
		exit 1; \
	fi
	@printf "$(GREEN)📝 Creating migration: $(name)$(NC)\n"
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $(name)
	@printf "$(GREEN)✅ Migration created!$(NC)\n"

.PHONY: seed
seed: ## Seed the production database with demo users
	@printf "$(GREEN)🌱 Seeding database $(DB_NAME_CLEAN)...$(NC)\n"
	@if [ -f scripts/seed.go ]; then \
		go run scripts/seed.go; \
	else \
		printf "$(RED)❌ scripts/seed.go not found.$(NC)\n"; \
		exit 1; \
	fi
	@printf "$(GREEN)✅ Database seeded!$(NC)\n"

.PHONY: db-reset
db-reset: stop migrate-down migrate-up seed ## Reset production database (down, up, seed)

.PHONY: db-shell
db-shell: ## Open PostgreSQL shell against the production database
	@printf "$(GREEN)🐘 Opening PostgreSQL shell for database $(DB_NAME_CLEAN)...$(NC)\n"
	psql -d $(DB_NAME_CLEAN)

.PHONY: db-check
db-check: ## Check production database connection
	@printf "$(GREEN)🔍 Checking database connection...$(NC)\n"
	@psql -d $(DB_NAME_CLEAN) -c "SELECT 'Connected successfully' as status;"
	@printf "$(GREEN)✅ Database connection successful!$(NC)\n"

.PHONY: db-tables
db-tables: ## List all tables in the production database
	@printf "$(GREEN)📋 Listing tables in $(DB_NAME_CLEAN)...$(NC)\n"
	psql -d $(DB_NAME_CLEAN) -c "\dt"

# ============================================
# Test database
# ============================================
#
# The test database is entirely separate from the production one.
# It is created and reset automatically by the test suite on first
# run — these targets exist for manual inspection and cleanup.
#
# The test suite refuses to run if TEST_DB_NAME equals DB_NAME or
# if TEST_DB_NAME does not look like a test database.

.PHONY: test-db-create
test-db-create: ## Create the test database (idempotent)
	@printf "$(GREEN)🐘 Creating test database $(TEST_DB_NAME_CLEAN)...$(NC)\n"
	@psql -h $(TEST_DB_HOST) -p $(TEST_DB_PORT) -U $(TEST_DB_USER) \
		-d postgres -tc "SELECT 1 FROM pg_database WHERE datname = '$(TEST_DB_NAME_CLEAN)'" \
		| grep -q 1 \
		|| psql -h $(TEST_DB_HOST) -p $(TEST_DB_PORT) -U $(TEST_DB_USER) \
			-d postgres -c "CREATE DATABASE $(TEST_DB_NAME_CLEAN)"
	@printf "$(GREEN)✅ Test database ready$(NC)\n"

.PHONY: test-db-drop
test-db-drop: ## Drop the test database (with confirmation)
	@printf "$(YELLOW)⚠️  About to drop test database: $(TEST_DB_NAME_CLEAN)$(NC)\n"
	@printf "$(YELLOW)   Host: $(TEST_DB_HOST):$(TEST_DB_PORT)$(NC)\n"
	@read -p "Type 'yes' to confirm: " confirm; \
	if [ "$$confirm" != "yes" ]; then \
		printf "$(YELLOW)Aborted.$(NC)\n"; \
		exit 1; \
	fi
	@psql -h $(TEST_DB_HOST) -p $(TEST_DB_PORT) -U $(TEST_DB_USER) \
		-d postgres -c "DROP DATABASE IF EXISTS $(TEST_DB_NAME_CLEAN)"
	@printf "$(GREEN)✅ Test database dropped$(NC)\n"

.PHONY: test-db-shell
test-db-shell: ## Open psql against the test database
	@printf "$(GREEN)🐘 Opening test database shell for $(TEST_DB_NAME_CLEAN)...$(NC)\n"
	@psql -h $(TEST_DB_HOST) -p $(TEST_DB_PORT) -U $(TEST_DB_USER) \
		-d $(TEST_DB_NAME_CLEAN)

.PHONY: test-db-reset
test-db-reset: ## Recreate the test database from scratch
	@printf "$(YELLOW)🧹 Resetting test database...$(NC)\n"
	@psql -h $(TEST_DB_HOST) -p $(TEST_DB_PORT) -U $(TEST_DB_USER) \
		-d postgres -c "DROP DATABASE IF EXISTS $(TEST_DB_NAME_CLEAN)"
	@$(MAKE) test-db-create
	@printf "$(GREEN)✅ Test database reset$(NC)\n"

.PHONY: test-db-check
test-db-check: ## Show the resolved test database configuration
	@printf "$(BLUE)Test database:$(NC)\n"
	@printf "  TEST_DB_HOST=%s\n"     "$(TEST_DB_HOST)"
	@printf "  TEST_DB_PORT=%s\n"     "$(TEST_DB_PORT)"
	@printf "  TEST_DB_USER=%s\n"     "$(TEST_DB_USER)"
	@printf "  TEST_DB_NAME=%s\n"     "$(TEST_DB_NAME_CLEAN)"
	@printf "  TEST_DB_SSLMODE=%s\n"  "$(TEST_DB_SSLMODE)"
	@printf "\n$(BLUE)Production database (for comparison):$(NC)\n"
	@printf "  DB_NAME=%s\n"          "$(DB_NAME_CLEAN)"
	@if [ "$(TEST_DB_NAME_CLEAN)" = "$(DB_NAME_CLEAN)" ]; then \
		printf "\n$(RED)✗ TEST_DB_NAME equals DB_NAME — tests will refuse to run$(NC)\n"; \
		exit 1; \
	else \
		printf "\n$(GREEN)✓ TEST_DB_NAME differs from DB_NAME$(NC)\n"; \
	fi

# ============================================
# Testing & Quality
# ============================================

.PHONY: test
test: ## Run tests (serialized; DB tests use TEST_DB_*)
	@printf "$(GREEN)🧪 Running tests...$(NC)\n"
	go test -p 1 -count=1 -v ./...

.PHONY: test-fast
test-fast: ## Run tests without serialization (non-DB packages only)
	@printf "$(GREEN)🧪 Running tests (parallel)...$(NC)\n"
	go test -count=1 ./...

.PHONY: test-coverage
test-coverage: ## Run tests with raw coverage (all packages)
	@printf "$(GREEN)📊 Running tests with coverage...$(NC)\n"
	go test -p 1 -count=1 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@printf "$(GREEN)✅ Coverage report: coverage.html$(NC)\n"

.PHONY: test-coverage-src
test-coverage-src: ## Coverage excluding generated, wiring, and tooling packages
	@printf "$(GREEN)📊 Running source-only coverage...$(NC)\n"
	go test -p 1 -count=1 -coverprofile=coverage.raw ./...
	@grep -v -E '/(cmd|scripts|domain|repository|logging)/' coverage.raw > coverage.src
	@go tool cover -func=coverage.src | tail -1
	@go tool cover -html=coverage.src -o coverage-src.html
	@printf "$(GREEN)✅ Source-only report: coverage-src.html$(NC)\n"

.PHONY: fmt
fmt: ## Format code
	@printf "$(GREEN)🎨 Formatting code...$(NC)\n"
	go fmt ./...
	@if [ -f package.json ]; then npm run build:css 2>/dev/null || true; fi
	@printf "$(GREEN)✅ Code formatted!$(NC)\n"

.PHONY: lint
lint: ## Run linters
	@printf "$(GREEN)🔍 Running linters...$(NC)\n"
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run ./...; \
	else \
		printf "$(YELLOW)⚠️  golangci-lint not installed. Installing...$(NC)\n"; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
		golangci-lint run ./...; \
	fi

# ============================================
# Docker
# ============================================

.PHONY: docker-up
docker-up: ## Start Docker containers
	@printf "$(GREEN)🐳 Starting Docker containers...$(NC)\n"
	docker-compose up -d
	@printf "$(GREEN)✅ Docker containers started!$(NC)\n"

.PHONY: docker-down
docker-down: ## Stop Docker containers
	@printf "$(YELLOW)🐳 Stopping Docker containers...$(NC)\n"
	docker-compose down
	@printf "$(GREEN)✅ Docker containers stopped!$(NC)\n"

.PHONY: docker-build
docker-build: ## Build Docker containers
	@printf "$(GREEN)🐳 Building Docker containers...$(NC)\n"
	docker-compose build
	@printf "$(GREEN)✅ Docker containers built!$(NC)\n"

.PHONY: docker-logs
docker-logs: ## Show Docker logs
	docker-compose logs -f

# ============================================
# Combined Commands
# ============================================

.PHONY: setup
setup: install generate migrate-up build-css ## Complete project setup

.PHONY: dev-setup
dev-setup: install generate migrate-up build-css ## Setup and start development
	@printf "$(GREEN)✅ Setup complete! Run 'make dev' to start development.$(NC)\n"

.PHONY: all
all: clean install generate migrate-up build-css build ## Clean, install, migrate, build

# ============================================
# Utilities
# ============================================

.PHONY: check-env
check-env: ## Check environment variables
	@printf "$(BLUE)Environment Variables:$(NC)\n"
	@printf "  DB_HOST=%s\n"           "$(DB_HOST)"
	@printf "  DB_PORT=%s\n"           "$(DB_PORT)"
	@printf "  DB_USER=%s\n"           "$(DB_USER)"
	@printf "  DB_NAME=%s (cleaned: %s)\n" "$(DB_NAME)" "$(DB_NAME_CLEAN)"
	@printf "  DB_SSLMODE=%s\n"        "$(DB_SSLMODE)"
	@printf "  PORT=%s\n"              "$(PORT)"
	@printf "  DB_URL=%s\n"            "$(DB_URL)"
	@printf "\n$(BLUE)Test Environment:$(NC)\n"
	@printf "  TEST_DB_HOST=%s\n"      "$(TEST_DB_HOST)"
	@printf "  TEST_DB_PORT=%s\n"      "$(TEST_DB_PORT)"
	@printf "  TEST_DB_USER=%s\n"      "$(TEST_DB_USER)"
	@printf "  TEST_DB_NAME=%s (cleaned: %s)\n" "$(TEST_DB_NAME)" "$(TEST_DB_NAME_CLEAN)"
	@printf "  TEST_DB_SSLMODE=%s\n"   "$(TEST_DB_SSLMODE)"
	@printf "  TEST_DB_URL=%s\n"       "$(TEST_DB_URL)"

.PHONY: info
info: ## Show project information
	@printf "$(BLUE)Project Information:$(NC)\n"
	@printf "  App Name: %s\n"  "$(APP_NAME)"
	@printf "  Binary: %s\n"    "$(BINARY_NAME)"
	@printf "  Main: %s\n"      "$(GO_MAIN)"
	@printf "  Port: %s\n"      "$(PORT)"
	@printf "  PID File: %s\n"  "$(PID_FILE)"
	@printf "  Prod DB: %s\n"   "$(DB_NAME_CLEAN)"
	@printf "  Test DB: %s\n"   "$(TEST_DB_NAME_CLEAN)"
	@printf "  Version: %s\n"   "$(VERSION)"
	@printf "  Commit: %s\n"    "$(COMMIT)"
	@printf "  Branch: %s\n"    "$(BRANCH)"
	@printf "  Go Files: %s files\n" "$$(printf '%s' '$(GO_FILES)' | wc -w | tr -d ' ')"

# ============================================
# Directories
# ============================================

.PHONY: dirs
dirs: ## Create required directories
	@printf "$(GREEN)📁 Creating directories...$(NC)\n"
	mkdir -p bin
	mkdir -p logs
	mkdir -p static/css
	mkdir -p static/js
	mkdir -p internal/templates/layouts
	mkdir -p internal/templates/partials
	mkdir -p internal/web
	mkdir -p internal/handler
	mkdir -p internal/service
	mkdir -p internal/domain
	mkdir -p internal/repository
	mkdir -p internal/testutil
	mkdir -p pkg/db
	mkdir -p migrations
	mkdir -p scripts
	@printf "$(GREEN)✅ Directories created!$(NC)\n"

# ============================================
# Default target
# ============================================

.DEFAULT_GOAL := help