# Design and implement Redis-backed server-side caching for Project Radeon

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

`PLANS.md` is checked into the repository root at `PLANS.md`. This document must be maintained in accordance with `PLANS.md`.

## Purpose / Big Picture

After this change, the API should feel materially faster on the app’s busiest read paths because repeated and expensive responses will come from Redis instead of recomputing the same PostgreSQL queries on every request. The user-visible outcome is lower latency for feed loads, profile loads, discovery results, meetups, and support screens, while preserving correctness by treating PostgreSQL as the source of truth and using targeted invalidation when writes happen.

The backend already supports local development with environment variables and will eventually run in the cloud. This plan is intentionally designed so a developer can run the same cache code against a local Redis instance during development and against AWS ElastiCache in production with configuration changes only. A novice should be able to implement this plan, start PostgreSQL plus Redis locally, run the API, hit the cached endpoints twice, and observe the second request avoid the expensive database path through logs, metrics, or tests.

## Progress

- [x] (2026-04-26 11:10Z) Inspected the repository structure, entrypoint wiring in `cmd/api/main.go`, and the main domain stores and handlers to identify high-value cache candidates and invalidation boundaries.
- [x] (2026-04-26 11:25Z) Chosen the initial cache scope: feed, user profiles, user posts, discovery, interests, meetups, and support requests. Explicitly excluded auth, notifications, and chat message history from phase 1 because they are highly volatile or not the best latency win per unit of complexity.
- [x] (2026-04-26 11:40Z) Authored this ExecPlan in `exec_plans/redis_server_side_caching.md`.
- [x] (2026-04-26 12:05Z) Refined the invalidation design during implementation setup: profile freshness will also be protected by a cache-aware `friends` store wrapper, feed mutation freshness will use optional post-author lookup hooks, and meetup RSVP freshness will use optional meetup-organizer lookup hooks.
- [x] (2026-04-26 12:10Z) Recorded the local-environment assumption that Redis is installed natively on the development machine rather than run in Docker.
- [x] (2026-04-26 12:35Z) Implemented the shared Redis cache package in `pkg/cache/` with read-through JSON caching, version-key invalidation, and singleflight protection for concurrent misses.
- [x] (2026-04-26 12:45Z) Wired cache-aware wrappers for `internal/user`, `internal/feed`, `internal/meetups`, `internal/support`, and `internal/friends`, and updated `cmd/api/main.go` to configure Redis from environment variables.
- [x] (2026-04-26 12:50Z) Added automated tests covering cache hit and invalidation flows for the user, feed, meetups, and support decorators.
- [x] (2026-04-26 12:55Z) Updated `.env.example` and `README.md` with Redis configuration and the native local Redis assumption.
- [x] (2026-04-26 12:58Z) Verified the implementation with `GOCACHE=/tmp/go-build go test ./...`.
- [ ] Start the API with `CACHE_ENABLED=true` against the user’s local Redis service outside the sandbox and exercise the target endpoints twice to confirm live Redis hits end-to-end.

## Surprises & Discoveries

- Observation: The backend is already structured around per-domain interfaces such as `feed.Querier`, `user.Querier`, `meetups.Querier`, and `support.Querier`, which makes cache decorators practical without forcing cache logic into HTTP handlers.
    Evidence: `internal/feed/handler.go`, `internal/user/handler.go`, `internal/meetups/handler.go`, and `internal/support/handler.go` all depend on interfaces rather than directly on `*pgxpool.Pool`.

- Observation: The discovery and support visibility queries are meaningfully more expensive than simple record fetches because they compute ranking or visibility using multiple joins, lateral subqueries, and scoring logic.
    Evidence: `internal/user/store.go` function `discoverRanked` and `internal/support/store.go` function `ListVisibleSupportRequests` both contain long ranking queries with repeated lateral work and distance-like calculations.

- Observation: Some responses are personalized even when they look globally cacheable at first glance. That means keys must include the viewer when the response shape includes fields like `friendship_status`, `is_attending`, `has_responded`, or support summary counts.
    Evidence: `internal/user/store.go` function `GetUser` depends on `viewerID`; `internal/meetups/store.go` functions `ListMeetups` and `GetMeetup` compute `is_attending`; `internal/support/store.go` functions `GetSupportRequest` and `ListVisibleSupportRequests` compute viewer-specific fields.

- Observation: The repository currently has no Redis dependency or cache abstraction, so the plan must include new configuration, new package structure, and new tests rather than assuming existing infrastructure.
    Evidence: `go.mod` has no Redis client and repository searches for `redis` and `cache` do not reveal a general-purpose cache layer.

- Observation: The first version of the cache invalidation scheme mistakenly treated both “missing version key” and “version incremented once” as the same namespace value, which meant the first write did not invalidate anything.
    Evidence: Initial decorator tests for user, feed, meetups, and support all failed until the default version was changed from `1` to `0`, allowing the first `INCR` to produce a new namespace.

- Observation: Direct verification of the local Redis service is blocked inside this Codex sandbox because socket creation to `127.0.0.1:6379` is restricted here.
    Evidence: `redis-cli ping` returned `Could not connect to Redis at 127.0.0.1:6379: Can't create socket: Operation not permitted` during the final verification pass.

## Decision Log

- Decision: Use Redis as a read-through cache only, with PostgreSQL remaining the source of truth for all writes.
    Rationale: This keeps correctness simple. If Redis is down or a key is missing, the API can still function by reading from PostgreSQL. This is the safest first production caching model for this codebase.
    Date/Author: 2026-04-26 / Codex

- Decision: Prefer cache decorators around existing domain store interfaces instead of adding cache logic to handlers.
    Rationale: The current backend already uses domain interfaces. Decorating those interfaces keeps handlers thin, preserves current routing code, and localizes invalidation logic near the underlying read and write methods that affect cached data.
    Date/Author: 2026-04-26 / Codex

- Decision: Use versioned cache namespaces for invalidation rather than relying on wildcard key scans and deletes.
    Rationale: Deleting many keys by pattern is slow and operationally fragile in Redis, especially once key counts grow. Incrementing a small version counter lets stale keys age out naturally while new reads automatically miss into a new namespace.
    Date/Author: 2026-04-26 / Codex

- Decision: Exclude chat message pages, notification lists, and authentication from phase 1.
    Rationale: These areas change frequently, have more complicated freshness requirements, and are not the best place to start for perceptible app-speed gains. Feed, discovery, profiles, meetups, and support offer better returns with lower correctness risk.
    Date/Author: 2026-04-26 / Codex

- Decision: Use the same Go Redis client for local Redis and AWS ElastiCache.
    Rationale: The user’s deployment target is ElastiCache, but local development should remain simple. A single Redis client library with environment-driven configuration avoids environment-specific logic in application code.
    Date/Author: 2026-04-26 / Codex

- Decision: Add a cache-aware wrapper to `internal/friends` even though the first caching phase does not cache friend-list responses.
    Rationale: `GetUser` responses include friendship status and friend counts. Without friend-mutation invalidation, profile caching would serve avoidable stale relationship state. A thin wrapper that only bumps cache-version keys keeps the cached profile layer correct without forcing friend-list caching into scope.
    Date/Author: 2026-04-26 / Codex

- Decision: Treat the local development Redis service as a native installation and not a Docker prerequisite.
    Rationale: The user explicitly stated Redis is already installed locally through the system package manager. The implementation and validation steps should reflect that environment rather than introducing a second local runtime path as the default assumption.
    Date/Author: 2026-04-26 / Codex

## Outcomes & Retrospective

The cache implementation now exists in the codebase and is covered by automated tests. The backend initializes a Redis-backed cache client from environment variables, wraps the selected domain stores with read-through cache decorators, and invalidates affected namespaces on writes using Redis version keys instead of broad key scans. The most important design correction discovered during implementation was the namespace-version bug described above; fixing the default version to `0` made the invalidation model behave correctly on the first write.

The main remaining work is live end-to-end validation against the user’s real local Redis service or production-like ElastiCache environment. That could not be performed inside this sandbox because local socket access is restricted. The code-side validation is still strong because the full Go test suite passes, including targeted invalidation tests for the new cache decorators.

## Context and Orientation

This repository is a Go HTTP API for a sobriety-focused social community. The entrypoint is `cmd/api/main.go`, which loads environment variables, opens the PostgreSQL connection pool, constructs per-domain handlers, and registers all routes on a `chi` router. A route handler is the code that receives an HTTP request and writes an HTTP response. In this repository, handlers live in files such as `internal/feed/handler.go` and depend on interfaces named `Querier` rather than directly on the database pool.

The persistent database is PostgreSQL, accessed through `pgxpool`. The type `*pgxpool.Pool` is a pool of reusable database connections. Most domain packages have a `store.go` file with a concrete PostgreSQL-backed implementation, usually named `pgStore`, and a constructor named `NewPgStore(pool)`. That design matters because the cache layer can wrap those stores rather than replacing handlers.

Redis is an in-memory key-value data store. In this plan, Redis will hold copies of serialized API read results for a short time. A cache key is the string used to identify a stored value in Redis, such as a specific feed page or profile response. A time-to-live, often abbreviated as TTL, is the amount of time Redis keeps a key before it expires automatically. A cache decorator is a struct that implements the same interface as the underlying PostgreSQL store but first checks Redis for a cached response before calling the real store.

The key backend files for this work are:

- `cmd/api/main.go`, where new Redis configuration and store wiring will be added.
- `go.mod`, where the Redis client dependency will be added.
- `pkg/database/database.go`, which is useful as a style reference for how infrastructure code is currently initialized.
- `internal/user/store.go` and `internal/user/handler.go`, which contain profile, interests, and discovery logic.
- `internal/feed/store.go` and `internal/feed/handler.go`, which contain global feed, user posts, reactions, and comments logic.
- `internal/meetups/store.go` and `internal/meetups/handler.go`, which contain meetups lists, detail, attendees, and RSVP mutations.
- `internal/support/store.go` and `internal/support/handler.go`, which contain support profile, support request lists, and support response logic.

The user’s frontend lives in a separate repository and is not modified here. The only visible effect on the app will be lower response times for repeated reads. The API contract must not change.

## Plan of Work

The implementation should begin by adding a small shared cache package under `pkg/cache/`. This package should define a `Client` or `Store` abstraction around Redis, JSON serialization helpers for storing typed responses, helper functions for building namespaced keys, helper functions for incrementing version counters, and a small TTL jitter helper. Jitter means adding a small random variation to each TTL so that many hot keys do not expire at the exact same second and stampede the database together. The package should also expose a no-op mode so the application can run with caching disabled without changing handler or store behavior.

In `go.mod`, add `github.com/redis/go-redis/v9`. Use this one library for both local Redis and ElastiCache. In `cmd/api/main.go`, load new environment variables such as `CACHE_ENABLED`, `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB`, `REDIS_TLS`, and `REDIS_PREFIX`. If cache is enabled, create the Redis client and pass it into cache decorators; otherwise wire the existing PostgreSQL stores directly. Keep the server functional even if Redis cannot be reached at startup by making that behavior explicit in the chosen implementation. The preferred production-safe default is to log the failure and continue without caching when `CACHE_ENABLED` is false or unset, but to fail fast if `CACHE_ENABLED=true` and Redis cannot be initialized, because that makes misconfiguration obvious.

The user domain should be the first cache decorator because it contains a mix of low-risk and high-value reads. Create a new file `internal/user/cache_store.go`. Define a struct that embeds or references the existing `Querier` implementation plus the shared cache package. It must implement the same interface that `internal/user/handler.go` expects. Cache `GetUser`, `DiscoverUsers`, and `ListInterests`. `GetUser` keys must include both `viewerID` and `userID` because friendship state and request counts are viewer-specific. `DiscoverUsers` keys must include `currentUserID`, city filter, query filter, latitude, longitude, limit, offset, and a viewer-specific version number. `ListInterests` can be keyed globally because the response is not personalized.

Next, create `internal/feed/cache_store.go`. Cache `ListFeed` and `ListUserPosts`. Use cursor-aware keys that include the `before` cursor and limit. Do not cache `ToggleReaction`, `AddComment`, or `CreatePost` responses themselves, but have those mutation methods increment the relevant version counters so the next read falls into a fresh namespace. When a post is created or deleted, invalidate the global feed namespace and that author’s user-posts namespace. When a reaction or comment changes, invalidate the global feed namespace and the specific author’s posts namespace if the author can be determined safely. If determining the author would require extra expensive queries that complicate the first version too much, invalidate only the global feed and the specific post-related list namespace you can identify directly, and document that tradeoff in the plan as it is implemented.

Then create `internal/meetups/cache_store.go`. Cache `ListMeetups`, `ListMyMeetups`, `GetMeetup`, and optionally `GetAttendees` if the implementation remains simple. Meetups are personalized because `is_attending` depends on the viewer, so list and detail keys must include the `userID`. On `CreateMeetup`, increment a global meetups list version and the creator’s personal meetups version. On `AddRSVP` and `RemoveRSVP`, increment the specific meetup detail version, the global meetups list version, and the affected user’s personal meetups version. If attendee previews are cached as part of the list payload, these invalidations naturally refresh them too.

Then create `internal/support/cache_store.go`. Cache `GetSupportProfile`, `ListMySupportRequests`, `ListVisibleSupportRequests`, `GetSupportRequest`, and `FetchSupportSummary`. Be careful here because many of these responses are personalized. `ListVisibleSupportRequests` and `FetchSupportSummary` must be keyed by the viewer. On support mutations such as `UpdateSupportProfile`, `CreateSupportRequest`, `CloseSupportRequest`, and `CreateSupportResponse`, increment viewer-specific and request-specific versions as appropriate. Since some support writes affect multiple users, prefer conservative invalidation over overly clever partial updates. It is acceptable for some support keys to become stale for a few seconds if the TTL is short and correctness is still maintained by explicit version bumps for the direct actor.

The implementation should use read-through cache flow everywhere. On each cacheable read, build the key, ask Redis for the value, and if found deserialize JSON into the typed Go value and return it. If the key is not found, call the underlying PostgreSQL store, serialize the result, and store it with TTL plus jitter. If Redis returns an operational error, log at a low enough level to avoid noise, skip caching for that request, and return the PostgreSQL result. This is known as fail-open behavior and is important because caching is an optimization, not a correctness dependency.

To reduce duplicate concurrent database work on hot keys, add in-process request coalescing in `pkg/cache/` using a singleflight mechanism. A singleflight group makes concurrent identical cache misses wait for one shared database fetch instead of all hitting PostgreSQL at once. This is especially valuable for feed pages, discovery pages, and popular profile lookups.

Key naming must be explicit and stable. Use a short prefix such as `pr` or a configurable prefix from `REDIS_PREFIX`. A safe example key pattern is `pr:user:profile:v<version>:viewer:<viewerID>:target:<userID>`. Use similar patterns for feed, discover, meetups, and support. Version counters should live at keys such as `pr:ver:user:<userID>`, `pr:ver:discover:<userID>`, `pr:ver:feed`, `pr:ver:meetups`, `pr:ver:meetup:<meetupID>`, and `pr:ver:support:<userID>`. Whenever a write affects cached data, increment the relevant version key or keys. Because the version is part of the read key, old cache entries become unreachable immediately and can expire naturally.

Testing must be added during implementation, not postponed. Each domain cache decorator should have unit tests that use a fake or local Redis test client and a fake underlying store. These tests should prove four behaviors: a cache miss reads through to the underlying store and stores a value, a cache hit does not call the underlying store again, a write invalidation causes the next read to miss into the fresh namespace, and a Redis failure still returns the underlying store result. Add tests only where they are maintainable and fast. If a shared cache helper package is introduced, it should have focused tests for key generation, version increments, and serialization helpers.

The final integration step is updating `cmd/api/main.go` so the handler wiring uses cached decorators for `user`, `feed`, `meetups`, and `support`, while leaving `auth`, `friends`, `chats`, and `notifications` on direct PostgreSQL stores for now. This split is intentional and should remain documented so a future contributor knows phase 1 is selective rather than universal.

## Concrete Steps

All commands below should be run from the repository root at `/home/michaelroddy/repos/project_radeon`.

Begin by ensuring the locally installed Redis service is running. Any local Redis 6 or newer instance is acceptable. On a Debian or Ubuntu-style system where Redis was installed through `apt`, a typical check is:

    redis-cli ping

Expect:

    PONG

Create or update the local environment file so the API knows to use Redis:

    CACHE_ENABLED=true
    REDIS_ADDR=localhost:6379
    REDIS_PASSWORD=
    REDIS_DB=0
    REDIS_TLS=false
    REDIS_PREFIX=pr

After adding the Redis dependency and cache package, update dependencies and format code:

    go mod tidy
    gofmt -w ./cmd ./internal ./pkg

Run the automated test suite after each milestone:

    go test ./...

Start the API after wiring the cache decorators:

    make run

In a separate terminal, exercise the endpoints with repeated requests. The exact user identifiers depend on local seed data, but the workflow should be:

    curl -H "Authorization: Bearer <token>" http://localhost:8080/users/me
    curl -H "Authorization: Bearer <token>" http://localhost:8080/feed
    curl -H "Authorization: Bearer <token>" "http://localhost:8080/users/discover?limit=20"
    curl -H "Authorization: Bearer <token>" http://localhost:8080/meetups
    curl -H "Authorization: Bearer <token>" http://localhost:8080/support/requests

The implementation should add concise logs or test-visible instrumentation so the second identical request can be shown to hit Redis rather than the underlying PostgreSQL store. If logs are added, keep them debug-oriented and avoid noisy per-request production logs.

After mutation behavior is wired, verify invalidation by calling a cached read, then a write that should affect it, then the same read again. Example flows include:

    1. GET /feed
    2. POST /posts
    3. GET /feed

    1. GET /users/me
    2. PATCH /users/me
    3. GET /users/me

    1. GET /meetups
    2. POST /meetups/{id}/rsvp
    3. GET /meetups

    1. GET /support/requests
    2. POST /support/requests
    3. GET /support/requests

The second read after each write must reflect the updated database state, proving invalidation works.

## Validation and Acceptance

Acceptance is based on observable behavior, not only on code changes.

First, run `go test ./...` and expect all tests to pass. New tests should include cache decorator coverage for at least the `user`, `feed`, `meetups`, and `support` domains. If a test uses a fake underlying store, it should explicitly assert call counts so a reader can see that the second read came from cache instead of PostgreSQL.

Second, start the API with local Redis enabled and confirm the health endpoint still works:

    curl http://localhost:8080/health

Expect an HTTP 200 response with a JSON body containing a success envelope and `status: ok`.

Third, obtain or reuse an authenticated token and call a cacheable endpoint twice with identical parameters. The second request must return the same JSON payload as the first request, and logs, counters, or test instrumentation must show that the second request did not execute the underlying store method again.

Fourth, perform a write that should invalidate the cached response, then repeat the read. The response must reflect the updated data immediately after the write. This is the most important acceptance criterion because it proves the cache speeds up reads without serving stale data indefinitely.

Fifth, simulate or stub a Redis failure in tests. The endpoint behavior must remain correct by falling back to PostgreSQL. A user should see a normal successful response rather than an internal server error caused only by cache unavailability.

The change is complete when a novice can run the API locally with Redis, hit the target endpoints twice, see cache hits for repeat reads, perform writes that affect those reads, and see fresh data returned immediately after invalidation.

## Idempotence and Recovery

The implementation steps in this plan are additive and can be repeated safely. Running `go mod tidy`, `gofmt`, and `go test ./...` multiple times is safe. Restarting the API is safe. Reusing the same Redis instance is safe because the cache contains derived data only.

If Redis contains stale or malformed development data during implementation, it is safe to flush the local development Redis instance because PostgreSQL remains the source of truth. For a local Redis container, a safe recovery command is:

    redis-cli -h localhost -p 6379 FLUSHDB

This command is acceptable only for local development against the cache database described in this plan. It must not be used blindly against shared or production Redis environments.

If the cache implementation introduces incorrect behavior, a safe rollback path is to set `CACHE_ENABLED=false` and restart the API. Because the application must keep working without Redis, this toggle provides an immediate operational escape hatch while preserving the rest of the codebase.

## Artifacts and Notes

The following key shapes should exist after implementation. These are examples, not exact hard-coded strings if the final package chooses small naming refinements:

    pr:ver:feed
    pr:feed:v3:before:none:limit:21
    pr:user:ver:6f8...user-id
    pr:user:profile:v6:viewer:6f8...:target:6f8...
    pr:userposts:ver:6f8...user-id
    pr:userposts:v4:user:6f8...:before:none:limit:21
    pr:discover:ver:6f8...viewer-id
    pr:discover:v9:viewer:6f8...:city::q::lat:none:lng:none:limit:20:offset:0
    pr:meetups:ver
    pr:meetup:ver:9ab...meetup-id
    pr:meetups:list:v5:viewer:6f8...:city::q::limit:21:offset:0
    pr:support:ver:6f8...viewer-id
    pr:support:list:v2:viewer:6f8...:before:none:limit:21

The following time-to-live ranges are the starting point and should be encoded as constants in the new cache package or domain cache files so they can be adjusted centrally:

    interests: 24 hours
    user profile: 5 minutes
    user posts: 60 seconds
    global feed: 30 seconds
    discovery ranked results: 10 minutes
    discovery search results: 3 minutes
    meetups list: 60 seconds
    meetup detail: 5 minutes
    support lists and summaries: 30 seconds

These values are intentionally conservative. Short time-to-live values reduce stale-risk while still improving repeated-read latency significantly.

## Interfaces and Dependencies

Add the Redis client dependency in `go.mod`:

    github.com/redis/go-redis/v9

Create a new package at `pkg/cache/`. At the end of the first milestone, it should provide stable interfaces and helpers equivalent to the following conceptual API:

    type Store interface {
        GetJSON(ctx context.Context, key string, dest any) (found bool, err error)
        SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error
        IncrVersion(ctx context.Context, key string) (int64, error)
        GetVersion(ctx context.Context, key string) (int64, error)
    }

    type Config struct {
        Enabled  bool
        Addr     string
        Password string
        DB       int
        TLS      bool
        Prefix   string
    }

The exact names may change during implementation, but the package must support those capabilities. It must also expose a constructor used from `cmd/api/main.go`, such as:

    func New(ctx context.Context, cfg Config) (Store, error)

For each domain cache decorator, define a constructor in the domain package that returns the same handler-facing interface as the existing PostgreSQL store. For example:

    internal/user/cache_store.go:
        func NewCachedStore(inner Querier, cache cache.Store) Querier

    internal/feed/cache_store.go:
        func NewCachedStore(inner Querier, cache cache.Store) Querier

    internal/meetups/cache_store.go:
        func NewCachedStore(inner Querier, cache cache.Store) Querier

    internal/support/cache_store.go:
        func NewCachedStore(inner Querier, cache cache.Store) Querier

Each cached store must preserve the existing method signatures expected by the handlers. The handler code should not need to know whether it is talking to a pure PostgreSQL store or a cached decorator.

The implementation should also add an in-process singleflight helper, either directly in `pkg/cache/` or as a private field on the cache client, so concurrent misses for the same key are collapsed into one underlying load.

Revision note: Created this ExecPlan to turn the earlier high-level caching design into the repository’s required formal execution-plan format before any implementation begins.
