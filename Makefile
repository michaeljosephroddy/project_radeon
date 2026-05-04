include .env
export

GO := /usr/local/go/bin/go
MIGRATE := $(GO) run ./cmd/migrate

run:
	air

dev:
	air

build:
	$(GO) build -o bin/project_radeon ./cmd/api

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

migrate:
	$(MIGRATE) up

migrate-status:
	$(MIGRATE) status

.PHONY: run dev build test vet tidy migrate migrate-status
