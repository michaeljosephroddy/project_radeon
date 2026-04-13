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
 
.PHONY: run build tidy migrate
