# Optimize Feed Impression Aggregation

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. This plan follows `PLANS.md` in the repository root.

## Purpose / Big Picture

The mobile app logs feed impressions when posts or reshares are viewed. Those impressions are useful ranking signals, so the goal is to keep them while removing avoidable database latency from the request path. After this change, `POST /feed/impressions` should write a batch in one database statement, aggregate-refresh jobs should be queued once per affected item instead of by a row-level trigger, and the background aggregate worker should refresh only the posts, shares, and authors named by its jobs.

The behavior to observe is that `/feed/impressions`, `/feed/home`, and `/users/discover` still work, while slow-operation logs for `db.batch`, broad aggregate inserts, and discover impression writes should drop or disappear under the same local app flow.

## Progress

- [x] (2026-05-20 18:50Z) Inspected the slow log and mapped the slow operations to feed impression logging, feed aggregate refresh jobs, and discover impression recording.
- [x] (2026-05-20 19:05Z) Created branch `optimize-impression-aggregation`.
- [x] (2026-05-20 19:20Z) Replaced per-row feed impression batch writes with one bulk `INSERT ... SELECT FROM unnest(...)` statement that also enqueues distinct aggregate jobs.
- [x] (2026-05-20 19:35Z) Scoped post, share, and author aggregate refresh SQL to target IDs before aggregation.
- [x] (2026-05-20 19:45Z) Made aggregate job completion/failure conditional on the exact claimed job so requeued work is not deleted after a refresh.
- [x] (2026-05-20 19:50Z) Added migration `migrations/076_optimize_impression_aggregation.sql` to drop the feed-impression row trigger and update queue functions.
- [x] (2026-05-20 19:55Z) Moved discover impression recording to a short background best-effort write and changed recent discover impression lookup to candidate-keyed lateral probes.
- [x] (2026-05-20 20:10Z) Ran `gofmt -w ./cmd ./internal ./pkg`.
- [x] (2026-05-20 20:15Z) Ran `GOCACHE=/tmp/go-build go test ./...`; all packages passed.
- [x] (2026-05-20 20:18Z) Applied migration `076_optimize_impression_aggregation.sql` locally with `make migrate`.
- [x] (2026-05-20 20:25Z) Verified the old feed-impression trigger count is zero, verified bulk SQL maps one impression to one aggregate job inside a rollback transaction, and ran a temporary Go verifier through `LogFeedImpressions` plus `ProcessPendingJobs`.

## Surprises & Discoveries

- Observation: The local database snapshot is tiny, so slow operations are not explained by table size alone.
    Evidence: `feed_impressions` had about 38 rows, `discover_impressions` about 420 rows, and `feed_aggregate_jobs` was empty when inspected.
- Observation: Isolated `EXPLAIN ANALYZE` checks were fast for single feed impression insert and discover impression queries.
    Evidence: Feed impression insert was about 6.6ms with trigger cost visible; discover impression insert of 20 rows was about 3.8ms. This points to request-path batching, trigger fan-out, aggregate-worker bursts, or local database contention rather than raw row count.

## Decision Log

- Decision: Keep feed impressions instead of deleting them.
    Rationale: The user changed direction and wants impression signals preserved while fixing the slow operations.
    Date/Author: 2026-05-20 / Codex
- Decision: Move aggregate job enqueue for feed impressions into `LogFeedImpressions` instead of leaving it to a row-level trigger.
    Rationale: The logger already has the full batch and can enqueue one job per distinct affected item, avoiding one trigger execution per impression row.
    Date/Author: 2026-05-20 / Codex
- Decision: Keep discover impression suppression reads synchronous but make writes asynchronous best-effort.
    Rationale: Reads affect what people are shown; writes are useful for future suppression but should not hold up the current discover response.
    Date/Author: 2026-05-20 / Codex
- Decision: Lower the default aggregate worker batch size from 200 to 50.
    Rationale: The app is currently small; smaller batches reduce bursty database work and contention while preserving background progress.
    Date/Author: 2026-05-20 / Codex

## Outcomes & Retrospective

The implementation keeps impressions and removes the main request-path and worker inefficiencies. Feed impression logging now uses one bulk statement and queues distinct aggregate jobs without the row-level feed-impression trigger. Aggregate refresh SQL is scoped to claimed targets. Discover impression writes no longer block the discover response. Validation passed with `GOCACHE=/tmp/go-build go test ./...`, `make migrate`, and a temporary Go verifier that wrote a real feed impression and processed the resulting aggregate job.

## Context and Orientation

The backend is a Go API in `/home/michaelroddy/repos/project_radeon`. Feed impression logging is implemented in `internal/feed/foundation_store.go` through `LogFeedImpressions`. Feed ranking reads derived aggregate tables through `internal/feed/read_store.go`. The background aggregate worker lives in `internal/feed/aggregate_worker.go` and refreshes derived rows through `internal/feed/aggregate_store.go`.

An impression is a record that an item or candidate was shown. Feed impressions are passive feed-view telemetry stored in `feed_impressions`. Discover impressions and dating impressions are separate tables used to avoid repeatedly showing the same people or dating candidates.

An aggregate job is a row in `feed_aggregate_jobs` that tells the background worker to recompute derived ranking features for a post, reshare, or author.

## Plan of Work

First, replace the feed impression write path with a single SQL statement. The statement validates inputs in Go, expands arrays with PostgreSQL `unnest`, upserts `feed_impressions`, and inserts distinct `feed_aggregate_jobs` rows for the affected posts and reshares. This removes the pgx batch and the row-level trigger fan-out from the hot request path.

Second, update aggregate refresh SQL so subqueries join against `target_posts`, `target_shares`, or `target_authors` CTEs before grouping. This keeps refresh work proportional to claimed jobs.

Third, make the queue safer. Claimed jobs carry `claimed_at`; completion and failure updates only affect rows that still have that exact claim and have not been requeued with a newer `queued_at`.

Fourth, update schema and migrations to drop `trg_feed_impressions_enqueue_feed_aggregate_job`, preserve the aggregate enqueue helper for other feed events, and keep claimed jobs claimed if new work arrives while a worker is processing.

Fifth, keep discover impressions but remove synchronous write latency from `GET /users/discover` by writing them in a short background context.

## Concrete Steps

Run all commands from `/home/michaelroddy/repos/project_radeon`.

Format Go files:

    gofmt -w ./cmd ./internal ./pkg

Run backend tests:

    go test ./...

In this sandbox, use a writable Go build cache:

    GOCACHE=/tmp/go-build go test ./...

Apply migrations locally:

    make migrate

Optionally start the API and exercise the app flow:

    make run

Then open the app, load the feed, scroll until `/feed/impressions` is sent, and open discover. The expected result is no slow `db.batch` log for feed impressions and no slow synchronous discover impression insert in the discover request.

## Validation and Acceptance

Acceptance requires `go test ./...` to pass, local migration `076_optimize_impression_aggregation.sql` to apply successfully, and the app flow to continue returning HTTP 200 for `/feed/home`, `/feed/impressions`, and `/users/discover`.

The targeted behavior proof is that `POST /feed/impressions` now has one database operation rather than a pgx batch of per-row inserts, and `feed_aggregate_jobs` receives one row per distinct affected post/share target. The worker should still populate `post_quality_features`, `share_quality_features`, and `author_feed_stats`.

## Idempotence and Recovery

The migration uses `DROP TRIGGER IF EXISTS` and `CREATE OR REPLACE FUNCTION`, so it is safe to apply once through the normal migration runner and safe for fresh schema reconstruction through `schema/base.sql`. If the migration fails before completion, rerun `make migrate` after fixing the reported SQL error. No data is deleted.

## Artifacts and Notes

Key edited files:

- `internal/feed/foundation_store.go`
- `internal/feed/aggregate_store.go`
- `internal/feed/aggregate_worker.go`
- `internal/user/discover_store.go`
- `cmd/api/main.go`
- `schema/base.sql`
- `migrations/076_optimize_impression_aggregation.sql`

## Interfaces and Dependencies

No new external dependencies are introduced. The implementation uses existing `pgxpool.Pool` methods and PostgreSQL SQL features already used elsewhere in the repository: CTEs, `unnest`, `ON CONFLICT`, and `RETURNING`.
