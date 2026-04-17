include .env
export

GO := /usr/local/go/bin/go

run:
	$(GO) run ./cmd/api

dev:
	air

build:
	$(GO) build -o bin/project_radeon ./cmd/api

tidy:
	$(GO) mod tidy

migrate:
	psql $(DATABASE_URL) -f migrations/001_bootstrap.sql

migrate2:
	psql $(DATABASE_URL) -f migrations/002_discovery.sql

migrate3:
	psql $(DATABASE_URL) -f migrations/003_discovery_radius.sql

.PHONY: run dev build tidy migrate migrate2 migrate3
