# Backend code health cleanup

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

This plan follows `PLANS.md` in the repository root. It is scoped to backend cleanup that preserves HTTP API behavior.

## Purpose / Big Picture

The Go backend powers the SoberSpace API. This cleanup should make the service easier to maintain while keeping existing routes, response envelopes, and database behavior stable. Developers should be able to run tests, vet, and build successfully after the cleanup.

## Progress

- [x] (2026-05-04T08:58:52Z) Created branch `cleanup/backend-code-health` from clean backend `main`.
- [x] (2026-05-04T08:58:52Z) Created this ExecPlan.
- [x] (2026-05-04T09:04:06Z) Ran baseline `go test ./...`, `go vet ./...`, and `make build` with Go caches in `/tmp`; they passed after rerunning with writable cache paths.
- [x] (2026-05-04T09:04:06Z) Applied low-risk backend cleanup: explicit numeric parse helpers, validation Make targets, and corrected stale repository instructions.
- [x] (2026-05-04T09:04:06Z) Re-ran validation commands; `make test`, `make vet`, and `make build` passed with Go caches pointed at `/tmp`.
- [x] (2026-05-04T09:04:06Z) Recorded final outcome and remaining larger refactor candidates.

## Surprises & Discoveries

- Observation: `AGENTS.md` says there are no tests, but this backend now has many `_test.go` files.
    Evidence: `find cmd internal pkg -maxdepth 3 -type f` showed tests in auth, chats, feed, groups, meetups, middleware, pagination, support, user, and other packages.
- Observation: The sandbox cannot write to the default Go build and module caches under the home directory.
    Evidence: Initial `go test ./...` and `go vet ./...` failed with read-only cache errors until `GOCACHE` and `GOMODCACHE` were pointed at `/tmp`.

## Decision Log

- Decision: Use a separate backend cleanup branch.
    Rationale: The backend worktree was clean on `main`, and the repo workflow asks for a branch for new work.
    Date/Author: 2026-05-04 / Codex.
- Decision: Keep parser behavior forgiving but make fallback handling explicit.
    Rationale: Existing pagination and JWT expiry parsing silently fell back on invalid input; preserving that behavior avoids API surprises while removing unclear ignored errors.
    Date/Author: 2026-05-04 / Codex.

## Outcomes & Retrospective

Backend cleanup completed for this pass. The backend branch now has corrected contributor instructions, `make test` and `make vet` targets, and clearer fallback parsing in JWT expiry and pagination code. Larger handler/store decomposition remains intentionally deferred because it would be higher risk and should be handled feature-by-feature with test coverage.

## Context and Orientation

This is a Go REST API. `cmd/api/main.go` wires the server. Feature packages live under `internal/`, such as `internal/groups`, `internal/feed`, `internal/meetups`, `internal/support`, `internal/user`, and `internal/chats`. Shared packages live under `pkg/`, including database setup, middleware, response helpers, cache, storage, and pagination. The backend uses raw SQL with pgx and PostgreSQL; cleanup must not change SQL semantics unless a clear bug is found and validated.

## Plan of Work

Start with baseline validation. Then apply mechanical cleanup such as `gofmt`, stale documentation correction, obvious named response structs where low-risk, and removal of stale comments or avoidable ignored errors. Avoid large store or handler rewrites in this pass unless a small extraction clearly improves readability and tests cover the behavior.

## Concrete Steps

Run from `/home/michaelroddy/repos/project_radeon`:

    go test ./...
    go vet ./...
    make build

Then edit focused files with `apply_patch` and run:

    gofmt -w <changed-go-files>
    go test ./...
    go vet ./...
    make build

## Validation and Acceptance

Acceptance is all backend validation commands exiting with code 0. Existing API route behavior should remain stable because cleanup is internal and tests should continue passing.

## Idempotence and Recovery

Formatting commands are safe to repeat. If a cleanup change creates unexpected behavior, revert only the affected hunk rather than resetting the branch.

## Artifacts and Notes

Validation transcript summary:

    GOCACHE=/tmp/project-radeon-go-build GOMODCACHE=/tmp/project-radeon-go-mod make test
    /usr/local/go/bin/go test ./...
    ok github.com/project_radeon/api/internal/auth ...
    ok github.com/project_radeon/api/pkg/username ...

    GOCACHE=/tmp/project-radeon-go-build GOMODCACHE=/tmp/project-radeon-go-mod make vet
    /usr/local/go/bin/go vet ./...

    GOCACHE=/tmp/project-radeon-go-build GOMODCACHE=/tmp/project-radeon-go-mod make build
    /usr/local/go/bin/go build -o bin/project_radeon ./cmd/api

All commands exited with code 0. The generated `bin/project_radeon` binary was restored afterward so the cleanup diff does not include build output.

## Interfaces and Dependencies

No new runtime dependencies are planned. Existing dependencies include chi, pgx, JWT, Redis, AWS SDK, and standard Go packages.
