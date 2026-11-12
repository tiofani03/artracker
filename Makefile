SHELL := /bin/bash

.PHONY: help db-up db-down api web seed build test vet up down logs ps

help:
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*##/\t/'

db-up: ## start dev postgres on :5433
	docker compose up -d db

db-down: ## stop dev postgres (data volume kept)
	docker compose down

api: ## run the Go API on $PORT (default 8080, read from .env)
	cd backend && go run ./cmd/api

web: ## run Vite dev server on $WEB_DEV_PORT (default 5173)
	cd frontend && npm run dev

seed: ## fill every existing project in DATABASE_URL with demo data (needs at least one project)
	cd backend && go run ./cmd/seed

build: ## build both halves
	cd backend && CGO_ENABLED=0 go build -o ../bin/api ./cmd/api
	cd frontend && npm run build

test: ## go test
	cd backend && go test -p 1 ./...

vet: ## go vet
	cd backend && go vet ./...

up: ## full stack in docker (prod compose)
	docker compose -f docker-compose.prod.yml up -d --build

down:
	docker compose -f docker-compose.prod.yml down

logs:
	docker compose -f docker-compose.prod.yml logs -f --tail=100

ps:
	docker compose -f docker-compose.prod.yml ps
