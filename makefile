# Makefile for Go + HTMX + Tailwind project
# ============================================

SHELL := /bin/bash

# Variables
APP_NAME       = mywebapp
BINARY_NAME    = bin/$(APP_NAME)
GO_MAIN        = cmd/api/main.go
GO_FILES       = $(shell find . -name '*.go' -type f -not -path "./vendor/*" -not -path "./.git/*" -not -path "./bin/*")
MIGRATIONS_DIR = migrations

# Diagram layout:
#
#   docs/diagrams/  — PlantUML sources (.puml). Committed.
#   docs/images/    — rendered SVGs and any other images the
#                     Asciidoctor documents reference. Committed.
#
# The render runs from the source directory with no -o flag. PlantUML
# writes the SVG next to its source, and the pattern rule moves it
# into docs/images/. This avoids the class of bug where PlantUML
# concatenates a relative input path and a relative output path and
# produces docs/diagrams/docs/diagrams/... instead of the intended
# location.
DIAGRAMS_DIR   = docs/diagrams
IMAGES_DIR     = docs/images
PUML_SOURCES   = $(wildcard $(DIAGRAMS_DIR)/*.puml)
SVG_TARGETS    = $(patsubst $(DIAGRAMS_DIR)/%.puml,$(IMAGES_DIR)/%.svg,$(PUML_SOURCES))

# PlantUML invocation. Override from the command line or the
# environment if your install differs:
#
#   make diagrams PLANTUML=plantuml
#   make diagrams PLANTUML="java -jar /opt/plantuml/plantuml.jar"
#   make diagrams PLANTUML="docker run --rm -v $$PWD/docs/diagrams:/data plantuml/plantuml:latest"
#
# The default assumes the jar is at the path the how-to document
# recommends (~/.local/share/plantuml/plantuml.jar), which is where
# `make install-plantuml` puts it.
PLANTUML ?= java -jar $(HOME)/.local/share/plantuml/plantuml.jar

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

# Load .env file and export variables. .env holds configuration only;
# it never contains secrets. The two passwords the DB targets need are
# decrypted from the SOPS file by the load-secrets target and written
# to a mode-0600 temp file that the DB recipes source.
ifneq (,$(wildcard .env))
    include .env
    export
endif

# Strip surrounding quotes from DB_NAME, if present.
DB_NAME_CLEAN = $(shell printf '%s' '$(DB_NAME)' | tr -d '"'"'")
TEST_DB_NAME_CLEAN = $(shell printf '%s' '$(TEST_DB_NAME)' | tr -d '"'"'")

# Secrets file. The DB targets read from it; the application reads the
# same file at boot via internal/credentials. One source of truth.
SOPS_FILE = secrets/secrets.enc.yaml

# Where load-secrets writes the decrypted passwords for the DB
# recipes to source. Mode 0600, removed by `make clean`. The name is
# gitignored.
SECRETS_ENV = $(CURDIR)/.make-secrets.env

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
	@printf "\n$(GREEN)Diagrams:$(NC)\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## .*$$/ && ($$1 ~ /^diagrams/) {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\n$(GREEN)Testing & Quality:$(NC)\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## .*$$/ && ($$1 ~ /^(test|fmt|lint|coverage)/) {printf "  $(YELLOW)%-20s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf "\n"

# ============================================
# Secrets
# ============================================
#
# DB_PASSWORD and TEST_DB_PASSWORD live in secrets/secrets.enc.yaml,
# not in .env. The Go application reads them via internal/credentials
# at boot. The migrate binary and psql do not know about SOPS, so the
# makefile has to decrypt the file and make the values available
# before any target that connects to Postgres can run.
#
# The load-secrets target below decrypts the two passwords once per
# make invocation and writes them to a mode-0600 file. Every DB
# target depends on it and sources the file before using the URL.

.PHONY: require-sops
require-sops:
	@if [ ! -f "$(SOPS_FILE)" ]; then \
		printf "$(RED)❌ $(SOPS_FILE) not found.$(NC)\n"; \
		printf "$(YELLOW)   The DB targets need the decrypted password.$(NC)\n"; \
		exit 1; \
	fi
	@if ! command -v sops >/dev/null; then \
		printf "$(RED)❌ sops not found in PATH.$(NC)\n"; \
		printf "$(YELLOW)   Install: https://github.com/getsops/sops/releases$(NC)\n"; \
		exit 1; \
	fi

.PHONY: load-secrets
load-secrets: require-sops
	@umask 077; \
	if [ -f "$(SECRETS_ENV)" ] && [ "$(SECRETS_ENV)" -nt "$(SOPS_FILE)" ]; then \
		exit 0; \
	fi; \
	sops --decrypt --extract '["DB_PASSWORD"]' "$(SOPS_FILE)" > "$(SECRETS_ENV).tmp" 2>/dev/null || { \
		printf "$(RED)❌ Failed to decrypt DB_PASSWORD from $(SOPS_FILE).$(NC)\n"; \
		printf "$(YELLOW)   Is the age key available? Run: sops --decrypt $(SOPS_FILE) | head$(NC)\n"; \
		rm -f "$(SECRETS_ENV).tmp"; \
		exit 1; \
	}; \
	printf 'DB_PASSWORD=%s\n' "$$(cat $(SECRETS_ENV).tmp)" > "$(SECRETS_ENV)"; \
	sops --decrypt --extract '["TEST_DB_PASSWORD"]' "$(SOPS_FILE)" > "$(SECRETS_ENV).tmp" 2>/dev/null || { \
		printf "$(RED)❌ Failed to decrypt TEST_DB_PASSWORD from $(SOPS_FILE).$(NC)\n"; \
		rm -f "$(SECRETS_ENV).tmp" "$(SECRETS_ENV)"; \
		exit 1; \
	}; \
	printf 'TEST_DB_PASSWORD=%s\n' "$$(cat $(SECRETS_ENV).tmp)" >> "$(SECRETS_ENV)"; \
	rm -f "$(SECRETS_ENV).tmp"; \
	printf "$(GREEN)✅ Secrets loaded from $(SOPS_FILE)$(NC)\n"

.PHONY: clear-secrets
clear-secrets: ## Remove the cached decrypted passwords
	@rm -f "$(SECRETS_ENV)" "$(SECRETS_ENV).tmp"
	@printf "$(GREEN)✅ Cached secrets removed$(NC)\n"

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
	rm -f $(SECRETS_ENV) $(SECRETS_ENV).tmp
	rm -f coverage.out coverage.raw coverage.src coverage.html coverage-src.html
	go clean -modcache
	@printf "$(GREEN)✅ Clean complete!$(NC)\n"
	@printf "$(YELLOW)   Rendered diagrams in $(IMAGES_DIR)/ are kept.$(NC)\n"
	@printf "$(YELLOW)   Use 'make diagrams-clean' to remove them.$(NC)\n"

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
# Diagrams
# ============================================
#
# The .puml sources live in docs/diagrams/. The rendered .svg files
# live in docs/images/, next to the other images the Asciidoctor
# documents reference.
#
# 'diagrams' renders only the sources that are newer than their
# output. Running it twice in a row is a no-op the second time.
#
# 'diagrams-check' is the CI target: it renders into a temp directory
# and diffs against the committed SVGs. A non-zero exit means the two
# have drifted, and the fix is to run 'make diagrams' and commit the
# result.
#
# 'diagrams-force' ignores timestamps and re-renders everything. Use
# it after installing a new PlantUML version, or when you suspect the
# cached SVGs are stale but the timestamps say otherwise.

.PHONY: diagrams
diagrams: $(SVG_TARGETS) ## Render PlantUML diagrams to docs/images/ (only if source is newer)
	@if [ -z "$(PUML_SOURCES)" ]; then \
		printf "$(YELLOW)⚠️  No .puml sources found in $(DIAGRAMS_DIR)/$(NC)\n"; \
		printf "$(YELLOW)   Create one with: $$EDITOR $(DIAGRAMS_DIR)/mydiagram.puml$(NC)\n"; \
		exit 0; \
	fi
	@printf "$(GREEN)📊 Diagrams up to date$(NC)\n"

# Pattern rule: render one .puml, then move the SVG into docs/images/.
#
# PlantUML is invoked with only the source path, from the repository
# root, and no -o flag. It writes the SVG next to the source. The mv
# then moves it into docs/images/. The intermediate step is
# deliberate: it sidesteps the path concatenation bug that produces
# docs/diagrams/docs/diagrams/... when both -o and a directory-bearing
# input path are given.
$(IMAGES_DIR)/%.svg: $(DIAGRAMS_DIR)/%.puml
	@printf "$(GREEN)📊 Rendering %s...$(NC)\n" "$<"
	@command -v java >/dev/null || { \
		printf "$(RED)❌ java not found. Install a JRE: sudo apt install default-jre$(NC)\n"; \
		exit 1; \
	}
	@mkdir -p $(IMAGES_DIR)
	@$(PLANTUML) -tsvg "$<"
	@if [ ! -f "$(DIAGRAMS_DIR)/$*.svg" ]; then \
		printf "$(RED)❌ expected $(DIAGRAMS_DIR)/$*.svg after rendering $<$(NC)\n"; \
		printf "$(RED)   check for an '!output' directive or a name on the @startuml line$(NC)\n"; \
		printf "$(RED)   found instead:$$(ls -1 $(DIAGRAMS_DIR)/*.svg 2>/dev/null | sed 's/^/\n     /')$(NC)\n"; \
		exit 1; \
	fi
	@mv "$(DIAGRAMS_DIR)/$*.svg" "$@"
	@printf "$(GREEN)✅ %s$(NC)\n" "$@"

.PHONY: diagrams-force
diagrams-force: ## Re-render all diagrams regardless of timestamps
	@printf "$(YELLOW)📊 Forcing render of all diagrams...$(NC)\n"
	@$(MAKE) --no-print-directory -B diagrams
	@printf "$(GREEN)✅ All diagrams rendered$(NC)\n"

.PHONY: diagrams-check
diagrams-check: ## CI: fail if committed SVGs differ from sources
	@printf "$(GREEN)🔍 Checking diagram freshness...$(NC)\n"
	@if [ -z "$(PUML_SOURCES)" ]; then \
		printf "$(YELLOW)⚠️  No .puml sources found in $(DIAGRAMS_DIR)/$(NC)\n"; \
		exit 0; \
	fi
	@tmpdir=$$(mktemp -d); \
	trap "rm -rf $$tmpdir" EXIT; \
	for src in $(PUML_SOURCES); do \
		name=$$(basename "$$src" .puml); \
		printf "$(YELLOW)   checking %s...$(NC)\n" "$$name"; \
		( cd $(DIAGRAMS_DIR) && $(PLANTUML) -tsvg -o "$$tmpdir" "$$(basename $$src)" >/dev/null 2>&1 ) || { \
			printf "$(RED)❌ PlantUML failed on $$src$(NC)\n"; \
			exit 1; \
		}; \
		rendered="$$tmpdir/$$name.svg"; \
		committed="$(IMAGES_DIR)/$$name.svg"; \
		if [ ! -f "$$committed" ]; then \
			printf "$(RED)❌ $$committed is missing; run 'make diagrams'$(NC)\n"; \
			exit 1; \
		fi; \
		if ! diff -q "$$rendered" "$$committed" >/dev/null; then \
			printf "$(RED)❌ $$committed is stale; run 'make diagrams' and commit$(NC)\n"; \
			exit 1; \
		fi; \
	done
	@printf "$(GREEN)✅ All diagrams are fresh$(NC)\n"

.PHONY: diagrams-clean
diagrams-clean: ## Remove rendered SVG files from docs/images/ (sources are kept)
	@printf "$(YELLOW)🧹 Removing rendered diagrams...$(NC)\n"
	@rm -f $(SVG_TARGETS)
	@printf "$(GREEN)✅ Diagrams cleaned$(NC)\n"

.PHONY: diagrams-list
diagrams-list: ## List diagram sources and their rendered outputs
	@printf "$(BLUE)Diagram sources:$(NC)\n"
	@if [ -z "$(PUML_SOURCES)" ]; then \
		printf "  $(YELLOW)(none found in $(DIAGRAMS_DIR)/)$(NC)\n"; \
		exit 0; \
	fi
	@for src in $(PUML_SOURCES); do \
		name=$$(basename "$$src" .puml); \
		svg="$(IMAGES_DIR)/$$name.svg"; \
		if [ -f "$$svg" ]; then \
			if [ "$$src" -nt "$$svg" ]; then \
				printf "  $(YELLOW)%-40s (stale)$(NC)\n" "$$src"; \
			else \
				printf "  $(GREEN)%-40s (fresh)$(NC)\n" "$$src"; \
			fi; \
		else \
			printf "  $(RED)%-40s (not rendered)$(NC)\n" "$$src"; \
		fi; \
	done

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

.PHONY: install-plantuml
install-plantuml: ## Download the PlantUML jar to ~/.local/share/plantuml/
	@printf "$(GREEN)📦 Installing PlantUML...$(NC)\n"
	@mkdir -p $(HOME)/.local/share/plantuml
	@if [ -f $(HOME)/.local/share/plantuml/plantuml.jar ]; then \
		printf "$(YELLOW)⚠️  $(HOME)/.local/share/plantuml/plantuml.jar already exists$(NC)\n"; \
		printf "$(YELLOW)   Remove it first to reinstall$(NC)\n"; \
	else \
		curl -fL -o $(HOME)/.local/share/plantuml/plantuml.jar \
			https://github.com/plantuml/plantuml/releases/latest/download/plantuml.jar; \
		file $(HOME)/.local/share/plantuml/plantuml.jar | grep -q 'Zip archive' || { \
			printf "$(RED)❌ Download did not produce a jar (probably an HTML error page)$(NC)\n"; \
			rm -f $(HOME)/.local/share/plantuml/plantuml.jar; \
			exit 1; \
		}; \
		printf "$(GREEN)✅ PlantUML installed$(NC)\n"; \
	fi
	@command -v java >/dev/null || \
		printf "$(YELLOW)   Install a JRE: sudo apt install default-jre$(NC)\n"

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
migrate-up: load-secrets ## Run database migrations up
	@printf "$(GREEN)📊 Running migrations up on database $(DB_NAME_CLEAN)...$(NC)\n"
	@set -a; . "$(SECRETS_ENV)"; set +a; \
	migrate -path $(MIGRATIONS_DIR) \
		-database "postgres://$(DB_USER):$$DB_PASSWORD@$(DB_HOST):$(DB_PORT)/$(DB_NAME_CLEAN)?sslmode=$(DB_SSLMODE)" \
		up
	@printf "$(GREEN)✅ Migrations complete!$(NC)\n"

.PHONY: migrate-down
migrate-down: load-secrets ## Run database migrations down
	@printf "$(YELLOW)📊 Running migrations down on database $(DB_NAME_CLEAN)...$(NC)\n"
	@set -a; . "$(SECRETS_ENV)"; set +a; \
	migrate -path $(MIGRATIONS_DIR) \
		-database "postgres://$(DB_USER):$$DB_PASSWORD@$(DB_HOST):$(DB_PORT)/$(DB_NAME_CLEAN)?sslmode=$(DB_SSLMODE)" \
		down
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
seed: load-secrets ## Seed the production database with demo users
	@printf "$(GREEN)🌱 Seeding database $(DB_NAME_CLEAN)...$(NC)\n"
	@set -a; . "$(SECRETS_ENV)"; set +a; \
	if [ -f scripts/seed.go ]; then \
		go run scripts/seed.go; \
	else \
		printf "$(RED)❌ scripts/seed.go not found.$(NC)\n"; \
		exit 1; \
	fi
	@printf "$(GREEN)✅ Database seeded!$(NC)\n"

.PHONY: db-reset
db-reset: stop migrate-down migrate-up seed ## Reset production database (down, up, seed)

.PHONY: db-shell
db-shell: load-secrets ## Open PostgreSQL shell against the production database
	@printf "$(GREEN)🐘 Opening PostgreSQL shell for database $(DB_NAME_CLEAN)...$(NC)\n"
	@set -a; . "$(SECRETS_ENV)"; set +a; \
	PGPASSWORD="$$DB_PASSWORD" psql \
		-h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME_CLEAN)

.PHONY: db-check
db-check: load-secrets ## Check production database connection
	@printf "$(GREEN)🔍 Checking database connection...$(NC)\n"
	@set -a; . "$(SECRETS_ENV)"; set +a; \
	PGPASSWORD="$$DB_PASSWORD" psql \
		-h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME_CLEAN) \
		-c "SELECT 'Connected successfully' as status;"
	@printf "$(GREEN)✅ Database connection successful!$(NC)\n"

.PHONY: db-tables
db-tables: load-secrets ## List all tables in the production database
	@printf "$(GREEN)📋 Listing tables in $(DB_NAME_CLEAN)...$(NC)\n"
	@set -a; . "$(SECRETS_ENV)"; set +a; \
	PGPASSWORD="$$DB_PASSWORD" psql \
		-h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME_CLEAN) -c "\dt"

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
test-db-create: load-secrets ## Create the test database (idempotent)
	@printf "$(GREEN)🐘 Creating test database $(TEST_DB_NAME_CLEAN)...$(NC)\n"
	@set -a; . "$(SECRETS_ENV)"; set +a; \
	if PGPASSWORD="$$TEST_DB_PASSWORD" psql \
		-h $(TEST_DB_HOST) -p $(TEST_DB_PORT) -U $(TEST_DB_USER) \
		-d postgres -tc "SELECT 1 FROM pg_database WHERE datname = '$(TEST_DB_NAME_CLEAN)'" \
		| grep -q 1; then \
		:; \
	else \
		PGPASSWORD="$$TEST_DB_PASSWORD" psql \
			-h $(TEST_DB_HOST) -p $(TEST_DB_PORT) -U $(TEST_DB_USER) \
			-d postgres -c "CREATE DATABASE $(TEST_DB_NAME_CLEAN)"; \
	fi
	@printf "$(GREEN)✅ Test database ready$(NC)\n"

.PHONY: test-db-drop
test-db-drop: load-secrets ## Drop the test database (with confirmation)
	@printf "$(YELLOW)⚠️  About to drop test database: $(TEST_DB_NAME_CLEAN)$(NC)\n"
	@printf "$(YELLOW)   Host: $(TEST_DB_HOST):$(TEST_DB_PORT)$(NC)\n"
	@read -p "Type 'yes' to confirm: " confirm; \
	if [ "$$confirm" != "yes" ]; then \
		printf "$(YELLOW)Aborted.$(NC)\n"; \
		exit 1; \
	fi
	@set -a; . "$(SECRETS_ENV)"; set +a; \
	PGPASSWORD="$$TEST_DB_PASSWORD" psql \
		-h $(TEST_DB_HOST) -p $(TEST_DB_PORT) -U $(TEST_DB_USER) \
		-d postgres -c "DROP DATABASE IF EXISTS $(TEST_DB_NAME_CLEAN)"
	@printf "$(GREEN)✅ Test database dropped$(NC)\n"

.PHONY: test-db-shell
test-db-shell: load-secrets ## Open psql against the test database
	@printf "$(GREEN)🐘 Opening test database shell for $(TEST_DB_NAME_CLEAN)...$(NC)\n"
	@set -a; . "$(SECRETS_ENV)"; set +a; \
	PGPASSWORD="$$TEST_DB_PASSWORD" psql \
		-h $(TEST_DB_HOST) -p $(TEST_DB_PORT) -U $(TEST_DB_USER) \
		-d $(TEST_DB_NAME_CLEAN)

.PHONY: test-db-reset
test-db-reset: load-secrets ## Recreate the test database from scratch
	@printf "$(YELLOW)🧹 Resetting test database...$(NC)\n"
	@set -a; . "$(SECRETS_ENV)"; set +a; \
	PGPASSWORD="$$TEST_DB_PASSWORD" psql \
		-h $(TEST_DB_HOST) -p $(TEST_DB_PORT) -U $(TEST_DB_USER) \
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
	@printf "  TEST_DB_PASSWORD=%s\n" "(from SOPS, not shown)"
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
setup: install generate migrate-up build-css diagrams ## Complete project setup

.PHONY: dev-setup
dev-setup: install generate migrate-up build-css diagrams ## Setup and start development
	@printf "$(GREEN)✅ Setup complete! Run 'make dev' to start development.$(NC)\n"

.PHONY: all
all: clean install generate migrate-up build-css build diagrams ## Clean, install, migrate, build, render diagrams

.PHONY: ci
ci: lint test diagrams-check ## What CI runs

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
	@printf "  DB_PASSWORD=%s\n"       "(from SOPS, not in .env)"
	@printf "  PORT=%s\n"              "$(PORT)"
	@printf "\n$(BLUE)Test Environment:$(NC)\n"
	@printf "  TEST_DB_HOST=%s\n"      "$(TEST_DB_HOST)"
	@printf "  TEST_DB_PORT=%s\n"      "$(TEST_DB_PORT)"
	@printf "  TEST_DB_USER=%s\n"      "$(TEST_DB_USER)"
	@printf "  TEST_DB_NAME=%s (cleaned: %s)\n" "$(TEST_DB_NAME)" "$(TEST_DB_NAME_CLEAN)"
	@printf "  TEST_DB_SSLMODE=%s\n"   "$(TEST_DB_SSLMODE)"
	@printf "  TEST_DB_PASSWORD=%s\n"  "(from SOPS, not in .env)"

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
	@printf "  Diagram sources: %s\n" "$$(printf '%s' '$(PUML_SOURCES)' | wc -w | tr -d ' ')"
	@printf "  Diagram outputs: %s\n" "$$(printf '%s' '$(SVG_TARGETS)' | wc -w | tr -d ' ')"
	@printf "  SOPS file: %s\n"  "$(SOPS_FILE)"

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
	mkdir -p $(DIAGRAMS_DIR)
	mkdir -p $(IMAGES_DIR)
	@printf "$(GREEN)✅ Directories created!$(NC)\n"

# ============================================
# Default target
# ============================================

.DEFAULT_GOAL := help