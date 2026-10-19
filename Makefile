SHELL := /bin/bash

DB_URL_LOCAL      ?= postgres://family:family@localhost:5432/family?sslmode=disable
DB_URL_DOCKER     ?= postgres://family:family@postgres:5432/family?sslmode=disable
TEST_DB_NAME      ?= family_test
TEST_DB_URL_LOCAL ?= postgres://family:family@localhost:5432/family_test?sslmode=disable
TEST_DB_URL_DOCKER ?= postgres://family:family@postgres:5432/family_test?sslmode=disable
MIGRATIONS        ?= backend/migrations

.DEFAULT_GOAL := help

## help: show this help
help:
	@echo "Доступные команды:"
	@grep -E '^## [a-z].*:' $(MAKEFILE_LIST) | sed 's/## //; s/:.*//' | sort | awk '{printf "  %-22s\n", $$1}'


## up: build and start postgres + migrations + backend + frontend (one command)
up:
	docker compose up --build -d
	$(MAKE) test-db

## down: stop containers and remove volumes
down:
	docker compose down -v

## logs: follow backend logs
logs:
	docker compose logs -f backend

## ps: show container status
ps:
	docker compose ps


## tools: install the goose CLI (same version as the Docker image)
tools:
	go install github.com/pressly/goose/v3/cmd/goose@v3.24.1


## migrate-up: apply migrations locally (requires goose in PATH, see `make tools`)
migrate-up:
	goose -dir $(MIGRATIONS) postgres "$(DB_URL_LOCAL)" up

## migrate-down: roll back the last migration locally
migrate-down:
	goose -dir $(MIGRATIONS) postgres "$(DB_URL_LOCAL)" down

## migrate-status: show local migration status
migrate-status:
	goose -dir $(MIGRATIONS) postgres "$(DB_URL_LOCAL)" status

## migrate-up-docker: apply migrations via a one-off goose run (usually not needed: `up` migrates)
migrate-up-docker:
	docker compose run --rm --entrypoint goose \
		-v "$(PWD)/$(MIGRATIONS):/migrations:ro" \
		backend -dir /migrations postgres "$(DB_URL_DOCKER)" up

## migrate-down-docker: roll back one migration via a one-off goose run
migrate-down-docker:
	docker compose run --rm --entrypoint goose \
		-v "$(PWD)/$(MIGRATIONS):/migrations:ro" \
		backend -dir /migrations postgres "$(DB_URL_DOCKER)" down

## migrate-status-docker: show migration status via the backend image
migrate-status-docker:
	docker compose run --rm --entrypoint goose \
		-v "$(PWD)/$(MIGRATIONS):/migrations:ro" \
		backend -dir /migrations postgres "$(DB_URL_DOCKER)" status


## seed: insert demo data locally
seed:
	cd backend && DATABASE_URL="$(DB_URL_LOCAL)" go run ./cmd/seed

## seed-reset: wipe and re-insert demo data locally
seed-reset:
	cd backend && DATABASE_URL="$(DB_URL_LOCAL)" go run ./cmd/seed -reset

## seed-docker: wipe and re-insert demo data via the backend image
seed-docker:
	docker compose run --rm --entrypoint /app/seed backend -reset


## bot-commands: register the bot command menu in Telegram (reads .env locally)
bot-commands:
	@set -a; if [ -f .env ]; then . ./.env; fi; set +a; cd backend && go run ./cmd/botcommands

## bot-commands-show: show the commands currently registered in Telegram
bot-commands-show: 
	@set -a; if [ -f .env ]; then . ./.env; fi; set +a; cd backend && go run ./cmd/botcommands -show

## bot-commands-docker: register the bot command menu via the backend image
bot-commands-docker:
	docker compose run --rm --no-deps --entrypoint /app/botcommands backend


## run: run the backend locally
run:
	cd backend && go run ./cmd/app

## web-dev: run the Vite dev server (proxies /api to :8080)
web-dev:
	cd frontend && npm run dev

## tunnel: expose :8080 with localtunnel for Telegram webhooks
tunnel:
	npx localtunnel --port 8080


## build: build backend binaries and the static frontend bundle
build: build-backend build-frontend

## build-frontend: produce a static bundle in frontend/dist
build-frontend:
	cd frontend && npm install && npm run build

## build-backend: compile Go binaries into backend/bin
build-backend:
	cd backend && go build -o bin/server ./cmd/app && go build -o bin/seed ./cmd/seed

## test-db: ensure the PostgreSQL test database exists and is migrated
test-db:
	docker compose up -d --wait postgres
	@docker compose exec -T postgres sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -tAc "SELECT 1 FROM pg_database WHERE datname = '\''$(TEST_DB_NAME)'\''" | grep -q 1 || psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" -c "CREATE DATABASE $(TEST_DB_NAME)"'
	docker compose run --rm --no-deps --entrypoint goose \
		-v "$(PWD)/$(MIGRATIONS):/migrations:ro" \
		backend -dir /migrations postgres "$(TEST_DB_URL_DOCKER)" up

## test: run backend tests against the PostgreSQL test database
test: test-db
	cd backend && TEST_DATABASE_URL="$(TEST_DB_URL_LOCAL)" go test -p 1 ./...

## test-integration: run PostgreSQL integration tests against the test database
test-integration: test-db
	cd backend && TEST_DATABASE_URL="$(TEST_DB_URL_LOCAL)" go test -p 1 ./internal/repo/persistent/ -run Postgres -v

## lint: run go vet and frontend typecheck
lint:
	cd backend && go vet ./...
	cd frontend && npm run typecheck

## fmt: format Go sources
fmt:
	cd backend && gofmt -w .

## tidy: tidy Go modules
tidy:
	cd backend && go mod tidy

## clean: remove build artifacts and installed node modules
clean:
	rm -rf backend/bin frontend/node_modules frontend/dist

## env: create .env from the example if missing
env:
	@test -f .env || (cp .env.example .env && echo "created .env from .env.example")

.PHONY: help up down logs ps tools migrate-up migrate-down migrate-status \
	migrate-up-docker migrate-down-docker migrate-status-docker seed seed-reset \
	seed-docker run web-dev tunnel bot-commands bot-commands-show \
	bot-commands-docker build build-frontend build-backend test-db test test-integration \
	lint fmt tidy clean env
