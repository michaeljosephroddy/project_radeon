# Redesign backend discovery into a staged candidate-generation and ranking pipeline

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

`PLANS.md` is checked into the repository root at `PLANS.md`. This document must be maintained in accordance with `PLANS.md`.

## Purpose / Big Picture

After this change, `GET /users/discover` should feel more relevant, less repetitive, and faster under load. A person using the app should stop seeing obviously wasted suggestions such as existing friends or pending friend requests, should see a better mix of nearby and high-probability connections, and should still get sensible fallback inventory when their network or filters are narrow.

The user-visible proof is straightforward. After implementation, a developer should be able to seed or reuse a local database, call the discover endpoint for a test account, and observe that the results exclude already-connected people, respond with materially lower query cost than the current monolithic ranking query, and continue to honor advanced filters and broadening behavior. The internal shape should also be easier to evolve because candidate retrieval, scoring, suppression, and preview logic will no longer be fused into one SQL statement.

## Progress

- [x] (2026-04-26 17:30Z) Inspected the current discover request parsing, preview handling, store queries, cache behavior, and relevant schema/indexes in `internal/user/handler.go`, `internal/user/store.go`, `internal/user/cache_store.go`, `schema/base.sql`, and `migrations/015_performance_indexes.sql`.
- [x] (2026-04-26 17:40Z) Compared the current design against public product and engineering descriptions from LinkedIn, Facebook, Tinder, and Bumble to separate visible product patterns from likely backend architecture.
- [x] (2026-04-26 17:55Z) Chosen the redesign direction: staged heuristic ranking with multiple candidate sources, denormalized discover features, suppression, and targeted caching, while preserving the existing HTTP endpoint shape.
- [x] (2026-04-26 18:10Z) Authored this ExecPlan in `exec_plans/discovery_algorithm_redesign.md`.
- [x] (2026-04-26 15:05Z) Added migrations `033_discover_feature_columns.sql`, `034_discover_suppression.sql`, and `035_discover_indexes.sql`, and mirrored the new schema in `schema/base.sql`.
- [x] (2026-04-26 15:12Z) Wired helper-column maintenance into profile, auth, location, avatar, and post-creation writes so `discover_lat`, `discover_lng`, `sobriety_band`, `profile_completeness`, and `last_active_at` are not backfill-only fields.
- [x] (2026-04-26 15:18Z) Implemented the staged discover pipeline in `internal/user/discover_store.go`, `internal/user/discover_types.go`, and `internal/user/discover_ranker.go`, including candidate generation, suppression, scoring, reranking, hydration, and impression recording.
- [x] (2026-04-26 15:20Z) Added discover count caching and pipeline-aware cache keys in `internal/user/cache_store.go`, and threaded the temporary rollout flag through `cmd/api/main.go`.
- [x] (2026-04-26 15:23Z) Added pure ranking/suppression tests and discover-count cache tests, then verified the backend with `gofmt -w ./cmd ./internal ./pkg` and `GOCACHE=/tmp/go-build go test ./...`.
- [x] (2026-04-26 15:30Z) Applied `033_discover_feature_columns.sql`, `034_discover_suppression.sql`, and `035_discover_indexes.sql` to the local `project_radeon` database with `make migrate`.
- [x] (2026-04-26 15:36Z) Ran `EXPLAIN (ANALYZE, BUFFERS)` against the old ranked query and the new `nearbyCandidates` and `mutualCandidates` source queries using the migrated local database and viewer `699321ff-328f-4911-afbd-1d79476747b4`.
- [x] (2026-04-26 15:36Z) Exercised `/health`, `/auth/login`, `/users/discover`, and `/users/discover/preview` manually on a local server at `:18080`, confirmed discover excluded connected users, confirmed preview broadening on a zero-exact case, and confirmed dismissal suppression with a temporary local dismissal row plus cache-version bump.

## Surprises & Discoveries

- Observation: The current discover query labels people as `friends`, `outgoing`, or `incoming`, but it still leaves them in the candidate set instead of excluding them.
    Evidence: `internal/user/store.go` functions `discoverBySearch` and `discoverRanked` join `friendships` and compute `friendship_status`, but neither query adds a `WHERE` predicate to suppress connected or pending users.

- Observation: The current ranked path is a single heavy SQL statement that computes distance decay, sobriety similarity, interest overlap, mutual-friend count, and recent activity inside one request-time query.
    Evidence: `internal/user/store.go` function `discoverRanked` uses `LEFT JOIN LATERAL` subqueries for interest aggregation, Jaccard overlap, mutual-friend counting, and recent-post activity, then sorts by a computed score.

- Observation: The current schema has almost no discovery-specific index support beyond `users.username` trigram and `users.city`.
    Evidence: `schema/base.sql` defines indexes for `username`, `city`, `user_interests(user_id)`, `user_interests(interest_id)`, `posts(user_id, created_at DESC)`, and basic friendship columns, but nothing specialized for discover distance filtering, derived sobriety bands, or suppression history.

- Observation: The preview endpoint can issue several expensive count queries per interaction and none of those counts are cached.
    Evidence: `internal/user/handler.go` function `buildDiscoverPreview` can call `CountDiscoverUsers` once for exact filters and up to four more times while broadening. `internal/user/cache_store.go` passes `CountDiscoverUsers` straight through to PostgreSQL.

- Observation: The repository already contains a deprecated concept of dismissal history, which is a strong sign that suppression belongs in this system but is currently missing from the live query path.
    Evidence: `migrations/002_discovery.sql` created `dismissed_users`, later `migrations/007_drop_dating_tables.sql` removed it, and no active store code now references a dismissal table when building discover results.

- Observation: Recording discover impressions inside the underlying store would only run on cache misses, because cached discover responses bypass the inner store entirely.
    Evidence: `internal/user/cache_store.go` returns cached `DiscoverUsers` results directly from Redis on a hit, so any impression write inside `pgStore.DiscoverUsers` would be skipped for those requests.

- Observation: The staged pipeline compiled and passed the current suite without requiring changes to handler interfaces or route registration.
    Evidence: `GOCACHE=/tmp/go-build go test ./...` passed after adding `internal/user/discover_store.go`, `internal/user/discover_types.go`, `internal/user/discover_ranker.go`, and the new cache logic.

- Observation: The first live discover request on the new pipeline failed even though the automated suite passed, because several candidate-source queries skipped placeholder `$15` and jumped straight to `$16`, which PostgreSQL rejects at runtime when there is no `$15`.
    Evidence: a temporary local debug run returned `ERROR: could not determine data type of parameter $15 (SQLSTATE 42P18)` until the source-query placeholders in `internal/user/discover_store.go` were renumbered contiguously.

- Observation: Dismissal suppression worked correctly only when the local check invalidated the viewer discover cache after writing the dismissal row directly in PostgreSQL.
    Evidence: after `INSERT INTO discover_dismissals ...` and `redis-cli INCR pr:ver:discover:viewer:699321ff-328f-4911-afbd-1d79476747b4`, a fresh `GET /users/discover?limit=10&offset=0&city=Stradbally` returned `anotheruser`, `patrick`, and `iamuser`, with dismissed candidate `michael` absent.

## Decision Log

- Decision: Keep the HTTP contract centered on `GET /users/discover` and `GET /users/discover/preview` instead of inventing a new endpoint family.
    Rationale: The frontend already depends on these routes. The redesign should happen behind the current API shape so the rollout risk stays in ranking quality and performance rather than route churn.
    Date/Author: 2026-04-26 / Codex

- Decision: Implement a staged heuristic pipeline rather than jumping directly to machine-learned ranking.
    Rationale: This repository does not yet have the event logging, offline training pipeline, or feature store needed for a credible learned ranker. A staged heuristic system still captures the architecture that large platforms use: candidate generation, lightweight scoring, reranking, and suppression.
    Date/Author: 2026-04-26 / Codex

- Decision: Move ranking math out of one giant SQL statement and into Go over a bounded candidate pool.
    Rationale: PostgreSQL should be used to retrieve good candidates cheaply, not to perform an ever-growing online recommender in one opaque query. Go-side scoring is easier to test, easier to tune, and easier to extend with suppression and diversity rules.
    Date/Author: 2026-04-26 / Codex

- Decision: Do not require PostGIS in this plan.
    Rationale: PostGIS would improve geospatial retrieval, but it adds operational complexity and an extension dependency that this repository does not currently assume. The first redesign should instead add indexed latitude and longitude helper columns plus a bounding-box prefilter before exact distance calculation. This is less optimal than PostGIS, but much easier to land safely here.
    Date/Author: 2026-04-26 / Codex

- Decision: Reintroduce suppression history as a first-class discover concept.
    Rationale: Repeatedly showing the same ignored people makes a suggestion algorithm feel poor even when the raw ranking score is defensible. Suppression is required for perceived quality, not just for scale.
    Date/Author: 2026-04-26 / Codex

- Decision: Preserve exact filter counts in phase 1, but cache and narrow the number of count queries.
    Rationale: The paid filtering UX already exposes numeric counts in the app. Replacing them immediately with approximate counts would require coordinated frontend copy changes. The safer first change is to make those counts cheaper before revisiting approximation.
    Date/Author: 2026-04-26 / Codex

- Decision: Keep impression writes best-effort instead of making them part of discover request success.
    Rationale: Impression logging improves suppression quality, but it is not worth turning a successful discover read into a 500 error if the insert fails. The source of truth for discover remains the ranked candidate result, not the impression side effect.
    Date/Author: 2026-04-26 / Codex

- Decision: Continue using a coarse discover-global cache version for eligibility-changing profile writes in phase 1, while narrowing only obviously unrelated invalidations such as banner updates.
    Rationale: Truly targeted candidate invalidation would require a more complex candidate-version scheme than this repository currently has. The phase-1 compromise is to keep correctness simple for discover-affecting fields and only avoid needless global bumps for fields that do not affect discover cards.
    Date/Author: 2026-04-26 / Codex

## Outcomes & Retrospective

The first implementation pass is complete in code but not yet fully validated against a migrated local database. The backend now has additive schema support for discover helper fields and suppression tables, a new staged discover pipeline behind `DISCOVER_PIPELINE_V2`, a Go-side scorer and reranker, discover count caching, and new unit coverage for candidate merging, suppression, reranking, and count-cache invalidation.

The code is now validated at three levels: compilation and unit tests, migrated local schema application, and live endpoint checks against a local server and database. The remaining technical gap before deleting the legacy ranked path is mostly breadth, not fundamentals: the query-plan evidence and manual checks were run against a representative but still modest local dataset of about 110 users, 136 friendships, 35 interest rows, and 62 posts.

## Context and Orientation

This repository is a Go HTTP API rooted at `cmd/api/main.go`. Discover belongs to the `internal/user` package. The handler entry points are `internal/user/handler.go` functions `Discover` and `DiscoverPreview`. Request parsing happens in `parseDiscoverRequest`, which builds `DiscoverUsersParams` from query-string inputs such as `q`, `gender`, `sobriety`, `age_min`, `age_max`, `distance_km`, `interest`, `lat`, and `lng`.

The current storage logic lives in `internal/user/store.go`. That file currently chooses between two paths. The first is `discoverBySearch`, which is used when `params.Query` is non-empty and sorts by username relevance. The second is `discoverRanked`, which is used when there is no search query and computes a “five-signal suggestion score” inside PostgreSQL. The score currently combines distance, sobriety-band similarity, interest Jaccard overlap, mutual friends, and recent posting activity.

The current cache adapter is `internal/user/cache_store.go`. It already caches `DiscoverUsers` and `ListInterests`, but it does not cache `CountDiscoverUsers`. Cache invalidation is version-based, which means a small counter in Redis is incremented when writes happen and that counter becomes part of the next cache key. This is useful and should remain, but the invalidation scope for discover must become more targeted as this plan lands.

The term “candidate generation” in this plan means the cheap step that gathers a few hundred promising people before any final scoring happens. In this repository, that will mean several smaller SQL queries that each return a limited set of candidate user IDs plus a few precomputed features. The term “reranking” means adjusting already-scored candidates to improve freshness, diversity, and suppression behavior before returning the final page. In this repository, reranking will happen in Go after candidates have been deduplicated and scored.

The relevant existing files are:

`cmd/api/main.go`, which wires the current cached user store and registers `/users/discover` and `/users/discover/preview`.

`internal/user/handler.go`, which defines `DiscoverUsersParams`, preview broadening, query parsing, and the user handler interface.

`internal/user/store.go`, which currently owns `CountDiscoverUsers`, `discoverBySearch`, and `discoverRanked`.

`internal/user/cache_store.go`, which currently caches discover result pages but not discover counts.

`schema/base.sql` and `migrations/015_performance_indexes.sql`, which show the current index baseline.

`migrations/029_current_location.sql` and `migrations/031_user_profile_identity.sql`, which show that location and profile identity fields have already evolved incrementally and should continue doing so through additive migrations.

## Plan of Work

The implementation should begin by separating “discovery state” from “generic user store” concerns. Keep the `internal/user` package, but stop letting `store.go` become the permanent home for an ever-larger query. Create new discover-focused files such as `internal/user/discover_store.go`, `internal/user/discover_ranker.go`, and `internal/user/discover_types.go`. `store.go` should continue owning general user profile persistence, while discover-specific retrieval and scoring move into clearly named helpers.

The first milestone is inventory hygiene and schema support. Add three new migrations: `migrations/033_discover_feature_columns.sql`, `migrations/034_discover_suppression.sql`, and `migrations/035_discover_indexes.sql`. The feature-columns migration should add additive helper columns on `users` for `sobriety_band SMALLINT`, `last_active_at TIMESTAMPTZ`, `profile_completeness SMALLINT`, and indexed helper coordinates `discover_lat DOUBLE PRECISION` and `discover_lng DOUBLE PRECISION`. `discover_lat` and `discover_lng` must hold the effective coordinates used for discovery, meaning current coordinates when present and profile coordinates otherwise. These columns are denormalized on purpose so discover queries stop repeating `COALESCE(current_lat, lat)` and repeated sobriety-band math on every request.

The suppression migration should create two discover-specific tables. The first should be `discover_impressions`, which records when a viewer was shown a candidate. It should include `viewer_id`, `candidate_id`, `shown_at`, and a short `source` label such as `nearby`, `mutual`, or `interests`. The second should be `discover_dismissals`, which records explicit hides or skips that should keep a person out of the feed for a cooldown window. If the app does not yet expose a dismiss action, the table should still be created now because the backend needs the concept for future suppression and the discover pipeline can already write impressions.

The index migration must support the new discover access patterns. Add B-tree indexes on `users(sobriety_band)`, `users(last_active_at DESC)`, `users(discover_lat, discover_lng)`, and `users(gender)` if that filter is expected to stay selective. Add composite indexes on `friendships(status, user_a_id)` and `friendships(status, user_b_id)` or their equivalent shapes so mutual-friend candidate generation stops paying for broad scans. Add indexes on `discover_impressions(viewer_id, shown_at DESC)` and `discover_dismissals(viewer_id, dismissed_at DESC)`. The exact index list should be validated with `EXPLAIN ANALYZE` during implementation rather than added blindly.

Once the schema support exists, create a discover pipeline struct in `internal/user/discover_store.go`. This should not change the handler interface. The pipeline should expose a method shaped like the current store call, for example `func (s *pgStore) DiscoverUsers(ctx context.Context, params DiscoverUsersParams) ([]User, error)`, but the body should route into a staged flow rather than directly into `discoverRanked`. The old `discoverRanked` function should remain temporarily behind a fallback flag while the new pipeline is proven.

The candidate-generation stage should use several smaller SQL queries, each capped to a conservative limit such as 100 to 300 IDs. Define candidate generation sources in plain language and map each one to a helper function in `internal/user/discover_store.go`. The required sources for the first version are:

`nearbyCandidates`, which finds people inside the requested or broadened distance and orders first by bounding-box proximity and then exact distance.

`mutualCandidates`, which finds friends-of-friends or accepted-friend graph neighbors that the viewer is not already connected to.

`interestCandidates`, which finds people sharing one or more interests with the viewer or requested filter interests.

`sobrietyCandidates`, which finds people in the same or adjacent sobriety band when the viewer or filter makes that meaningful.

`activeFallbackCandidates`, which finds recently active people matching baseline safety and filter requirements when the other sources run thin.

Each source query should return a discover candidate row type rather than a full API `User`. The row must include candidate ID, enough profile fields to hydrate later, and lightweight per-candidate features such as effective distance, shared-interest count, mutual-friend count, sobriety band, `last_active_at`, and `profile_completeness`. This keeps the source queries cheap while avoiding another round trip for each candidate.

After collecting these source-specific pools, merge them in Go, deduplicate by user ID, and drop any candidate suppressed by friendship state, impression cooldown, or dismissal cooldown. The suppression rules should be applied centrally in the pipeline so they are not reimplemented differently across candidate sources. “Suppression” here means: do not show accepted friends, do not show outgoing or incoming pending requests, do not show explicitly dismissed people until the cooldown expires, and do not show recently impressed people again until a shorter cooldown expires unless the candidate is needed to avoid an empty page.

Scoring should then happen in `internal/user/discover_ranker.go`. Define a `discoverCandidate` struct and a pure Go scoring function, for example `func scoreCandidate(viewer discoverViewerFeatures, candidate discoverCandidate, params DiscoverUsersParams) float64`. The scorer should preserve the current signal families but stop encoding them as a giant SQL formula. Keep distance, mutual-friend count, interest overlap, sobriety compatibility, and recent activity. Add profile completeness as a small positive signal and inactivity as a negative signal. Also add a source bonus so strong graph-based candidates are not lost behind weak fallback candidates. The exact weights do not need to match the current SQL weights, but they must be declared as named constants in the ranker file so they can be tuned in one place.

After scoring, add a reranking pass in Go before pagination. This pass should ensure the top page is not dominated by one narrow cluster, such as ten people from the same source or ten people from the same distance band. Keep the reranker simple. A deterministic source-balance rule and a freshness penalty are sufficient for the first version. The goal is not fairness in the legal sense; the goal is to avoid a brittle, monotonous page.

The search path should remain separate. `discoverBySearch` is fundamentally a user search feature, not the suggestion algorithm. Leave its primary text ordering intact, but apply the same inventory hygiene and suppression filters so search also excludes friends and pending requests unless product requirements explicitly want them.

The preview and count path should then be tightened. Keep `DiscoverPreviewResponse` and the existing route, but stop issuing repeated full-count scans without caching. Add count-key caching in `internal/user/cache_store.go` keyed by viewer, filter signature, and discover version, with a short TTL such as 30 to 60 seconds. The preview logic in `internal/user/handler.go` should still compute exact counts for phase 1, but it should exit early as soon as broadening produces a positive count and it should use the new cheaper candidate-generation-aware count helpers instead of the old full monolithic query whenever practical.

`CountDiscoverUsers` itself should be split into a cheaper discover-count path in `internal/user/discover_store.go`. When possible, count against an already-bounded candidate set rather than `COUNT(*)` over the entire `users` table with expensive expressions. If an exact count still requires SQL counting, use indexed filters and helper columns so the count query stops repeating distance and sobriety math against raw base columns.

The cache layer in `internal/user/cache_store.go` must be updated after the new pipeline exists. Continue caching discover result pages, but change invalidation semantics so a single unrelated profile update does not invalidate every discover cache globally. Use viewer-specific discover versions for changes to the viewer’s own profile or filters, and use narrower candidate-related bumps only for writes that actually change discover eligibility, such as interest updates, gender updates, sobriety updates, location updates, or friendship mutations. If narrowing global invalidation proves too complex in phase 1, document the temporary coarser behavior and add a follow-up task in this plan instead of hiding the tradeoff.

The implementation must be additive and parallel at first. Keep the old ranked query available behind a temporary environment flag, for example `DISCOVER_PIPELINE_V2`. In `cmd/api/main.go`, parse that flag and thread it into the user store or handler config. During development, this lets a contributor compare old and new behavior for the same account without re-editing code. Once the new pipeline has test coverage and acceptable local query plans, remove the flag and delete the old monolithic `discoverRanked` path in a final cleanup step.

## Concrete Steps

All commands in this section should be run from `/home/michaelroddy/repos/project_radeon`.

Start by creating the three migrations described above and updating `schema/base.sql` to mirror their final schema. After writing them, run the database migration command that this repository already uses:

    make migrate

If the local development environment does not have a ready PostgreSQL database, point `DATABASE_URL` at a throwaway local database before running migrations. The migrations must be additive and rerunnable in the usual `IF NOT EXISTS` style already used throughout the repository.

After the schema exists, create the discover-specific backend files. The minimum expected file set is:

    internal/user/discover_store.go
    internal/user/discover_ranker.go
    internal/user/discover_types.go
    internal/user/discover_store_test.go

Move only discover-specific logic into these files. Keep generic user mutation methods such as `UpdateUser`, `UpdateCurrentLocation`, `UpdateAvatarURL`, and `UpdateBannerURL` in `internal/user/store.go`.

Update `internal/user/store.go` so `DiscoverUsers` delegates to the new pipeline when `params.Query == ""`. Keep `discoverBySearch` as the search path, but apply the new inventory hygiene predicates there as part of the same change. Update `CountDiscoverUsers` to call the new count helper. During the migration period, optionally preserve the old ranked query under a private fallback function with a guard flag.

Update `internal/user/cache_store.go` so `CountDiscoverUsers` becomes cached and so discover result cache keys include any new broadening or pipeline-version state needed to avoid stale collisions. If a temporary environment flag is introduced, make it part of the cache key to keep old-pipeline and new-pipeline responses isolated.

Update `internal/user/handler.go` only where request parsing or preview broadening behavior must adapt to the new pipeline. Do not change the public JSON field names unless the user explicitly approves an API contract change. The implementation should be visible to callers only through better results and faster responses.

Format and test after each milestone:

    gofmt -w ./cmd ./internal ./pkg
    GOCACHE=/tmp/go-build go test ./...

When the new source queries exist, compare their plans to the old ranked query using local `psql` and `EXPLAIN ANALYZE`. The exact SQL text should be copied from the implementation and run against representative local data. Keep the proof concise in the plan as an artifact. A representative workflow is:

    psql "$DATABASE_URL"
    EXPLAIN (ANALYZE, BUFFERS) <old-ranked-query-with-sample-params>;
    EXPLAIN (ANALYZE, BUFFERS) <new-nearbyCandidates-query-with-same-viewer>;
    EXPLAIN (ANALYZE, BUFFERS) <new-mutualCandidates-query-with-same-viewer>;

Finally, exercise the discover endpoints with a real authenticated token:

    curl -H "Authorization: Bearer <token>" "http://localhost:8080/users/discover"
    curl -H "Authorization: Bearer <token>" "http://localhost:8080/users/discover/preview?gender=woman&distance_km=25&interest=Coffee"

Repeat the calls after making profile, location, interest, and friendship changes to confirm invalidation and suppression work correctly.

## Validation and Acceptance

Acceptance is based on behavior and evidence, not merely on compiling code.

First, run `GOCACHE=/tmp/go-build go test ./...` and expect all tests to pass. Add new tests that prove each of the following:

The new pipeline excludes accepted friends and pending requests from both discover and search results.

Suppressed users in `discover_dismissals` do not appear until their cooldown expires.

Recently impressed users in `discover_impressions` are deprioritized or excluded according to the chosen cooldown rule.

Candidate deduplication works when the same user appears in multiple sources.

The reranker does not return a page dominated entirely by one source when other viable sources exist.

Preview broadening still behaves correctly for exact-count-positive and exact-count-zero cases.

Second, run local endpoint checks with a seeded or manually prepared dataset. A successful manual scenario must show all of the following:

Calling `GET /users/discover` for a user with accepted or pending friendships never returns those connected users.

Calling `GET /users/discover` twice in a row with identical parameters returns the same payload while the second request uses the cache path instead of recomputing.

After changing the viewer’s interests, sobriety date, or current location, the next discover request reflects the updated ranking and filter eligibility rather than a stale cached page.

After inserting or recording a dismissal for a candidate, that person disappears from subsequent discover pages for the configured cooldown window.

Third, capture at least one `EXPLAIN ANALYZE` comparison in the final implementation notes. The new candidate-source queries do not need to beat the old monolithic query on every possible small dataset, but they must show a bounded-query shape that scales better conceptually and avoid repeated lateral subquery work across the full candidate inventory.

The redesign is complete when discover remains correct, paid filtering remains functional, query behavior is measurably cheaper and easier to reason about, and the old monolithic ranking path can be deleted without feature regression.

## Idempotence and Recovery

This plan is intentionally additive. Each migration should use the project’s existing defensive style so a partially applied local environment can be retried safely. New tables such as `discover_impressions` and `discover_dismissals` are append-only and safe to create before the application starts writing to them.

The code rollout should also be reversible. While the old ranked query still exists behind a temporary flag, switching `DISCOVER_PIPELINE_V2=false` should restore the old path immediately after a restart. This is the primary rollback mechanism during development and staged rollout. Only remove that flag after automated and manual validation are complete.

If a migration backfill for helper columns such as `sobriety_band`, `discover_lat`, or `discover_lng` fails midway, the safe recovery path is to fix the backfill SQL and rerun it idempotently. Do not drop partially populated columns; finish the backfill and only then enable the new pipeline in application code.

If cache keys become polluted during local iteration, it is safe to flush the local Redis cache because PostgreSQL remains the source of truth:

    redis-cli FLUSHDB

Use that only against local development Redis, never against shared or production infrastructure.

## Artifacts and Notes

The current algorithm being replaced is centered on these score families inside one SQL query:

    distance decay
    sobriety band match
    interest Jaccard overlap
    mutual friends
    recent posting activity

The replacement should preserve those concepts, but it should apply them after bounded candidate generation rather than while scanning and sorting the entire filtered population.

The expected candidate sources for the new pipeline are:

    nearbyCandidates
    mutualCandidates
    interestCandidates
    sobrietyCandidates
    activeFallbackCandidates

The expected new schema helpers are:

    users.sobriety_band
    users.last_active_at
    users.profile_completeness
    users.discover_lat
    users.discover_lng
    discover_impressions
    discover_dismissals

The expected new environment flag during migration is:

    DISCOVER_PIPELINE_V2=true

This flag is temporary and must be removed once the new pipeline is the only supported path.

## Interfaces and Dependencies

Do not add a new top-level package for discovery. Keep the work inside `internal/user` because discover is already part of the user domain in this repository.

The implementation should end with these stable or newly introduced interfaces and helpers:

In `internal/user/discover_types.go`, define types for the pipeline inputs and ranking features, including a `discoverCandidate` struct and a `discoverViewerFeatures` struct. These types must be internal to the package and should not leak into handler response models.

In `internal/user/discover_ranker.go`, define a pure scoring function that accepts `discoverViewerFeatures`, `discoverCandidate`, and `DiscoverUsersParams`, and returns a `float64` score. Also define a reranking helper that takes a scored candidate slice and returns the final ordered slice before pagination.

In `internal/user/discover_store.go`, define source-query helpers on `pgStore` for nearby, mutual, interest, sobriety, and active fallback candidates. Each helper must accept `context.Context` and `DiscoverUsersParams`, and must return a bounded slice of `discoverCandidate`.

In `internal/user/cache_store.go`, extend the cached store so `CountDiscoverUsers` is cached with a short TTL and so discover invalidation can distinguish between viewer-specific and globally relevant changes where practical.

In `cmd/api/main.go`, wire any temporary discover-pipeline flag through the existing store construction without changing route registration.

Change note: Initial version of this ExecPlan was added on 2026-04-26 to convert the current monolithic discover query into a staged discover pipeline after a static code review found relevance and scale issues in `internal/user/store.go`.

Change note: Updated on 2026-04-26 after implementation to record the landed schema, pipeline, cache, and test work, plus the remaining local database validation steps needed before removing the legacy ranked path.

Change note: Updated again on 2026-04-26 after local runtime validation to record the placeholder-numbering bug found in the first live discover request, the successful endpoint checks after fixing it, and the `EXPLAIN ANALYZE` evidence captured against the migrated local database.
