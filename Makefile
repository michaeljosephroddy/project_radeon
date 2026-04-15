include .env
export
 
run:
	go run ./cmd/api
 
build:
	go build -o bin/project_radeon ./cmd/api
 
tidy:
	go mod tidy
 
migrate:
	psql $(DATABASE_URL) -f migrations/001_bootstrap.sql

migrate2:
	psql $(DATABASE_URL) -f migrations/002_discovery.sql

migrate3:
	psql $(DATABASE_URL) -f migrations/003_discovery_radius.sql

.PHONY: run build tidy migrate migrate2 migrate3
