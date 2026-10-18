# AethinkAI Gateway AI — developer command contract (PRD §11.3).
# Every target is safe by default: nothing here destroys data except
# reset-data, which refuses to run outside a local development .env.

SHELL        := /bin/sh
# `-f deploy/compose.yaml` would make Compose read deploy/.env; point both at the
# repository root so one .env configures everything.
COMPOSE      := docker compose --env-file .env -f deploy/compose.yaml
API_DIR      := apps/api
WEB_DIR      := apps/web
GO_IMAGE     := golang:1.24-alpine
NODE_IMAGE   := node:22-alpine

.PHONY: help bootstrap dev test lint build e2e smoke down reset-data \
        api-build web-build api-test web-test fmt vet

help:
	@echo "bootstrap   generate .env with fresh secrets (idempotent)"
	@echo "dev         start postgres + redis, run the API and console locally"
	@echo "test        unit tests for the API and the console"
	@echo "lint        go vet + TypeScript typecheck"
	@echo "build       production builds (gateway binary + console bundle)"
	@echo "e2e         end-to-end gateway checks against a mock upstream"
	@echo "smoke       live Compose checks; no paid upstream calls"
	@echo "down        stop services, keep volumes"
	@echo "reset-data  DESTRUCTIVE local-only: drop volumes and start clean"

# --------------------------------------------------------------- bootstrap
bootstrap:
	@test -f .env && { echo ".env already exists — refusing to overwrite"; exit 1; } || true
	@sed -e "s|REPLACE_WITH_DB_PASSWORD|$$(openssl rand -hex 16)|g" \
	     -e "s|REPLACE_WITH_RANDOM_32_PLUS_BYTES|$$(openssl rand -base64 32 | tr -d '\n')|g" \
	     .env.example > .env
	@echo "wrote .env with generated secrets"
	@echo "next: make dev   (then create the first operator with BOOTSTRAP_TOKEN)"

# -------------------------------------------------------------------- dev
dev:
	$(COMPOSE) up -d postgres redis
	@echo "waiting for postgres to accept connections..."
	@until $(COMPOSE) exec -T postgres pg_isready -U aethink -d aethink >/dev/null 2>&1; do sleep 1; done
	@$(MAKE) --no-print-directory api-run

# Runs the API with the repository mounted, so edits only need a restart.
api-run:
	docker run --rm --network host \
	  --env-file .env \
	  -e HTTP_ADDR=127.0.0.1:8787 \
	  -e DATABASE_URL="postgres://aethink:$$(grep '^POSTGRES_PASSWORD=' .env | cut -d= -f2)@127.0.0.1:5432/aethink?sslmode=disable" \
	  -e REDIS_ADDR=127.0.0.1:6379 \
	  -e WEB_DIST=/app/web \
	  -v "$(CURDIR)/$(API_DIR):/src" \
	  -v "$(CURDIR)/$(WEB_DIR)/dist:/app/web" \
	  -w /src $(GO_IMAGE) go run ./cmd/api

# ------------------------------------------------------------------- test
test: api-test web-test

api-test:
	docker run --rm -v "$(CURDIR)/$(API_DIR):/src" -w /src $(GO_IMAGE) go test ./...

web-test:
	cd $(WEB_DIR) && npm test

# ------------------------------------------------------------------- lint
lint: vet
	cd $(WEB_DIR) && npm run typecheck

vet:
	docker run --rm -v "$(CURDIR)/$(API_DIR):/src" -w /src $(GO_IMAGE) sh -c "go vet ./... && go build ./..."

fmt:
	docker run --rm -v "$(CURDIR)/$(API_DIR):/src" -w /src $(GO_IMAGE) go fmt ./...

# ------------------------------------------------------------------ build
build: api-build web-build

api-build:
	docker build -f deploy/Dockerfile.api -t aethinkai/gateway:local .

web-build:
	cd $(WEB_DIR) && npm run build

# -------------------------------------------------------------------- e2e
# Boots the API against real Postgres and Redis and drives the full journey
# with a mock upstream. No external provider is contacted.
e2e:
	python3 scripts/e2e.py

# ------------------------------------------------------------------ smoke
smoke:
	$(COMPOSE) up -d --build
	@echo "waiting for the gateway to report ready..."
	@until curl -fsS http://127.0.0.1:$${GATEWAY_PORT:-8787}/readyz >/dev/null 2>&1; do sleep 2; done
	@curl -fsS http://127.0.0.1:$${GATEWAY_PORT:-8787}/readyz
	@echo ""
	@echo "smoke passed"

# ------------------------------------------------------------------- down
down:
	$(COMPOSE) down

reset-data:
	@test -f .env || { echo "no .env — nothing to reset"; exit 1; }
	@grep -q "APP_ENV=development" .env || { echo "refusing: APP_ENV is not development"; exit 1; }
	@printf "This deletes all local gateway data. Type 'yes' to continue: " && read ans && [ "$$ans" = "yes" ]
	$(COMPOSE) down -v
	@echo "local volumes removed"
