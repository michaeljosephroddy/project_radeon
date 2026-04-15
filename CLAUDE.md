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

## Go coding standards

### Error handling
- Always handle errors explicitly — never ignore a returned `error` with `_` unless there is a clear, commented reason
- Return errors to the caller rather than logging and continuing; only log at the top-level handler boundary
- Use `fmt.Errorf("context: %w", err)` to wrap errors with context while preserving the original for `errors.Is`/`errors.As`
- Do not panic in library or handler code; panics are reserved for truly unrecoverable init failures in `main`

### Package and file structure
- One package per feature domain under `internal/` — keep packages focused and cohesive
- Avoid circular imports; `internal/` packages must never import each other
- Shared, reusable code lives in `pkg/`; domain-specific code lives in `internal/<domain>/`
- File names should reflect their primary responsibility (e.g. `handler.go`, `middleware.go`, not `utils.go` or `helpers.go`)

### Interfaces and testability
- Define interfaces in the package that *uses* them, not the package that implements them
- Accept interfaces, return concrete types — this keeps callsites flexible and implementations simple
- The database layer should be behind an interface (e.g. `db Querier`) so handlers can be unit-tested with a mock or stub without a real Postgres connection
- Constructors (`NewHandler`, etc.) should accept dependencies by interface so they can be swapped in tests

### Handler pattern
- Handlers must do exactly three things: parse input, call business logic, write response — no SQL inside helpers that also do formatting, no response writing inside query functions
- Input validation (required fields, format checks) happens at the top of the handler before any DB call
- Ownership/authorisation checks happen immediately after parsing, before any mutation
- Keep handlers thin: if a handler grows beyond ~60 lines, extract a typed helper function

### SQL and database
- All SQL is raw — no ORM, no query builders; keep queries readable with consistent indentation
- Never interpolate user input into SQL strings; always use positional parameters (`$1`, `$2`, …)
- Use `pgxpool.Pool` for all queries; never open a standalone connection
- Prefer a single well-joined query over multiple round-trips (use `LEFT JOIN LATERAL` for optional sub-selects)
- Always `defer rows.Close()` immediately after a successful `Query` call

### Types and naming
- Use named structs for all request/response bodies — avoid `map[string]any` except for truly ad-hoc one-off responses
- Unexported types stay in the package that owns them; only export what callers need
- Boolean struct fields should read naturally: `IsGroup`, `IsActive`, not `Group`, `Active`
- Acronyms follow Go convention: `userID`, `avatarURL`, `httpClient` (not `userId`, `avatarUrl`, `httpClient`)

### Modularity and extensibility
- New feature domains get their own package under `internal/<domain>/` with a `Handler` struct and `NewHandler(db) *Handler` constructor — do not add new domains to existing packages
- Middleware must remain stateless and general-purpose; domain logic never belongs in middleware
- Route registration always happens in `cmd/api/main.go` — handlers must not register their own routes
- Add new migrations as new numbered files (`002_...sql`, `003_...sql`) — never modify applied migrations

### Code style
- Run `gofmt` and `go vet` before committing; CI should enforce this
- Prefer short, focused functions; if a function requires a comment to explain what it does (not why), it should probably be split
- Avoid naked returns and named return values except in very short functions where they clearly improve readability
- Use `context.Context` as the first parameter on every function that performs I/O
