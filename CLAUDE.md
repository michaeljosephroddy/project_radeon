# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make run        # Start the API server (go run ./cmd/api)
make build      # Compile to bin/project_radeon
make tidy       # go mod tidy
make migrate    # Apply migrations: psql $(DATABASE_URL) -f migrations/001_bootstrap.sql
```

Requires a `.env` file (see `.env.example`) with `PORT`, `ENV`, `DATABASE_URL`, `JWT_SECRET`, and `JWT_EXPIRY_HOURS`.

There are no tests in this project.

## Architecture

Go REST API for a sobriety/recovery social community. Uses **chi** for routing, **pgx** for direct SQL against PostgreSQL (no ORM), and **JWT** (HS256) for auth.

### Package layout

- `cmd/api/main.go` — Entry point: loads env, opens DB pool, wires all handlers, registers routes
- `internal/<domain>/handler.go` — One package per feature domain; each exports a `Handler` struct that holds a `*pgxpool.Pool`
- `pkg/database/` — pgxpool initialization
- `pkg/middleware/auth.go` — JWT validation; injects `userID` (UUID) into request context via `context.WithValue`
- `pkg/response/` — Envelope helpers: `JSON(w, status, data)`, `Error(w, status, msg)`, `ValidationErrors(w, map)`

### Handler pattern

Every domain handler is constructed with `NewHandler(pool)` in `main.go` and attached to the chi router. Handlers call `pgxpool.Pool` directly with raw SQL — no query builders. Context carries the authenticated user's UUID, retrieved with a typed key from `pkg/middleware`.

### Auth flow

1. `POST /auth/register` or `POST /auth/login` → returns a JWT
2. All protected routes pass through `middleware.Authenticate`, which validates the token and injects `userID`
3. Handlers read `userID` from context for ownership checks and queries

### Database

Schema is in `project_radeon_db_schema.sql` (reference) and `migrations/001_bootstrap.sql` (applied). UUID primary keys throughout. 15 interests are seeded in the bootstrap migration.

### Response envelope

Success: `{"data": {...}}`  
Error: `{"error": "message"}`  
Validation: `{"errors": {"field": "message"}}`
