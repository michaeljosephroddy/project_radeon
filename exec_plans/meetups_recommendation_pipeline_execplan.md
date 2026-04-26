# Redesign meetup discovery ranking into a production-ready recommendation pipeline

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

`PLANS.md` is checked into the repository root at `PLANS.md`. This document must be maintained in accordance with `PLANS.md`.

## Purpose / Big Picture

After this change, `GET /meetups` with the default `recommended` sort should feel more relevant, more stable, and cheaper to serve under load. A person using the app should see nearby and timely events that match their interests, but should also see stronger social proof, better variety, and less repetition. The endpoint should stop doing full-candidate decoration and in-memory ranking work for every matching event before pagination, because that pattern will not hold up once event inventory grows.

The user-visible proof is clear. After implementation, a developer should be able to seed a local database with many events, call the discover endpoint for a test account, and observe that the first page returns the same public JSON shape as today, but with faster query behavior, stronger ranking signals, and stable cursor pagination. The developer should also be able to run targeted tests and simple local comparisons showing that recommendation quality is no longer driven only by distance, time, and raw attendee count.

## Progress

- [x] (2026-04-26 21:15Z) Inspected the current meetup discovery path in `internal/meetups/handler.go`, `internal/meetups/logic.go`, `internal/meetups/store.go`, `internal/meetups/types.go`, and `internal/meetups/cache_store.go`.
- [x] (2026-04-26 21:22Z) Identified the current production risks: full-candidate decorate/filter/sort before slicing, post-query distance filtering, shallow personalization, label-based category affinity, and offset-style cursor semantics over a dynamic ranked feed.
- [x] (2026-04-26 21:30Z) Authored this ExecPlan in `exec_plans/meetups_recommendation_pipeline_execplan.md`.
- [x] (2026-04-26 21:58Z) Added `StoreConfig` and the `MEETUPS_RECOMMENDER_V2` rollout flag wiring, then routed `recommended` discover requests through a dedicated pipeline while preserving the existing public `GET /meetups` contract.
- [x] (2026-04-26 22:05Z) Added recommendation-specific modules in `internal/meetups/recommendation_store.go`, `internal/meetups/recommendation_ranker.go`, and `internal/meetups/recommendation_types.go` covering candidate generation, feature hydration, reranking, and stable cursor encoding.
- [x] (2026-04-26 22:08Z) Added cache-key versioning for the new recommendation path and added migration `037_meetups_recommendation_indexes.sql` plus matching schema updates in `schema/base.sql`.
- [x] (2026-04-26 22:12Z) Added pure ranking and cursor regression tests in `internal/meetups/recommendation_ranker_test.go`.
- [x] (2026-04-26 22:15Z) Validated with `GOCACHE=/tmp/go-build go test ./internal/meetups ./cmd/api` and `GOCACHE=/tmp/go-build go test ./...`.
- [x] (2026-04-26 22:16Z) Applied `037_meetups_recommendation_indexes.sql` locally with `make migrate`.
- [ ] Manual API validation and `EXPLAIN (ANALYZE, BUFFERS)` comparison against seeded meetup inventory remain to be captured.

## Surprises & Discoveries

- Observation: The current recommendation pipeline loads all matching public future meetups, decorates them, filters them, sorts them in memory, and only then slices the requested page.
    Evidence: `internal/meetups/store.go` function `DiscoverMeetups` calls `loadDiscoverMeetups`, then `decorateMeetups`, then `filterMeetups`, then `sortMeetups`, then `sliceMeetups`.

- Observation: Distance filtering is not pushed into SQL. The database returns candidate rows first, and distance is enforced later in Go.
    Evidence: `internal/meetups/store.go` function `loadDiscoverMeetups` does not add a distance predicate, while `filterMeetups` applies `params.DistanceKM` after `decorateMeetups` computes `DistanceKM`.

- Observation: Search is still plain `ILIKE` over multiple text columns.
    Evidence: `internal/meetups/store.go` function `loadDiscoverMeetups` uses `ILIKE` against title, description, venue name, and city.

- Observation: The recommendation score is intentionally simple and deterministic, but it only knows about distance, time-to-start, attendee count, waitlist status, category-label match, and a small online boost.
    Evidence: `internal/meetups/store.go` function `recommendedScore`.

- Observation: Discover results are cached already, but the cache currently stores the output of the existing expensive pipeline rather than a more efficient candidate-generation flow.
    Evidence: `internal/meetups/cache_store.go` function `DiscoverMeetups` caches the result of `inner.DiscoverMeetups(ctx, userID, params)` for 60 seconds keyed by viewer and filters.

- Observation: A stable cursor based on the last returned meetup ID is much easier to land safely here than a full score-and-timestamp cursor because the pipeline still recomputes a bounded ranked pool on each request.
    Evidence: the implemented `recommendedCursor` in `internal/meetups/recommendation_types.go` stores `last_id` and a fallback offset, and `sliceRecommendedMeetups` resumes after that ID in the reranked candidate list.

- Observation: The repository does not currently store meetup impression, save, or hide events in a way that can drive production personalization, so the first recommendation upgrade had to lean on friend attendance and organizer quality rather than viewer-behavior history.
    Evidence: `internal/meetups/types.go` has `saved_count` on `Meetup`, but there is no persisted viewer-meetup interaction table or store path in `internal/meetups/` for saves, hides, or impression logs.

## Decision Log

- Decision: Keep the public API shape centered on `GET /meetups` and the existing query parameters.
    Rationale: The frontend already depends on the current contract. The redesign should improve ranking quality and serving cost behind the same route, not force a client migration.
    Date/Author: 2026-04-26 / Codex

- Decision: Stay with heuristic ranking for this phase, but restructure it into a staged pipeline.
    Rationale: This repository does not yet have event-level impression, click, RSVP, and save data wired into an offline training loop, so a machine-learned ranker would be premature. A staged heuristic pipeline captures the right architecture now and leaves room for later learned scoring.
    Date/Author: 2026-04-26 / Codex

- Decision: Use recommendation-specific candidate generation and bounded ranking rather than continuing to score every matching event in memory.
    Rationale: Query and CPU cost should scale with the page and a bounded candidate pool, not the entire inventory that happens to match loose filters.
    Date/Author: 2026-04-26 / Codex

- Decision: Keep the current `sort` options (`recommended`, `soonest`, `distance`, `popular`, `newest`) but make `recommended` use a different internal path than the simple sort modes.
    Rationale: Users and the frontend already understand those sort keys. The internal pipeline can become more sophisticated without changing the API.
    Date/Author: 2026-04-26 / Codex

- Decision: Replace offset-style cursor behavior for `recommended` feeds with a stable ranking cursor based on event identity and sort position.
    Rationale: A changing ranked feed should not page by “skip N rows after resorting everything.” Stable cursor semantics are needed if the result set changes between page requests.
    Date/Author: 2026-04-26 / Codex

- Decision: Use accepted-friend attendance and organizer historical metrics as the first production signals beyond distance and time, but defer viewer-behavior learning signals until the repository has the required interaction storage.
    Rationale: Those signals are available now from existing tables and can improve relevance immediately without inventing fake personalization data sources.
    Date/Author: 2026-04-26 / Codex

## Outcomes & Retrospective

The main implementation is complete. `recommended` meetup discovery now runs through a separate bounded candidate pipeline, only decorates the final page instead of the full match set, includes friend-attendance and organizer-quality scoring, applies a simple diversity rerank, and uses a stable cursor that resumes after the last returned meetup instead of naïve offset semantics. The cache key and store configuration were updated to keep the new path isolated and reversible behind `MEETUPS_RECOMMENDER_V2`.

The remaining gap is runtime evidence, not code structure. Manual API validation against seeded meetup inventory and at least one `EXPLAIN (ANALYZE, BUFFERS)` comparison should still be captured before calling the work fully closed. The code and tests are in place, and the local index migration has been applied successfully.

## Context and Orientation

This repository is the Go backend for the app. The meetup discovery endpoint is handled in `internal/meetups/handler.go`. Query parameter parsing happens in `parseDiscoverMeetupsParams`, which creates `DiscoverMeetupsParams` defined in `internal/meetups/types.go`. The handler calls `Querier.DiscoverMeetups`, and the PostgreSQL implementation lives in `internal/meetups/store.go`.

Today, `DiscoverMeetups` in `internal/meetups/store.go` does five things in one linear pass. It loads all candidate events from PostgreSQL with `loadDiscoverMeetups`, decorates those candidates with attendee previews, host data, and computed distance using `decorateMeetups`, removes any rows that fail post-query rules with `filterMeetups`, sorts the remaining rows in memory with `sortMeetups`, and finally slices the page with `sliceMeetups`.

The word “candidate” in this plan means a meetup that is eligible to be considered for ranking before the final page is chosen. The phrase “candidate generation” means fetching a bounded set of plausible events from several cheap sources instead of loading every matching event and hoping an in-memory sort can fix quality later. The phrase “feature” means a small piece of information used by ranking, such as distance to the viewer, whether any friends are attending, or whether the category aligns with a viewer interest.

The cache layer for meetup discovery is in `internal/meetups/cache_store.go`. It already caches discover result pages for a short time. That is good, but it does not fix the underlying algorithm shape. If the uncached path remains too expensive or too naive, cache misses will still be slow and low-quality, and frequent event writes will still invalidate many pages.

The current score function is in `internal/meetups/store.go` as `recommendedScore`. It starts from a constant score and adjusts it based on distance, time until the event starts, attendee count, waitlist/fullness, category-interest match, and a small online-event bonus. This is a useful baseline, but it misses several signals that matter in an events product, such as friends attending, organizer reliability, viewer behavior history, and diversity in the returned page.

## Plan of Work

The implementation should begin by splitting `recommended` discovery into its own internal pipeline while preserving the current public handler and types. Leave `soonest`, `distance`, `popular`, and `newest` available, but stop forcing the default `recommended` sort through the same full-candidate load-and-sort path. Introduce new internal files in `internal/meetups/` for recommendation-specific code, for example `recommendation_store.go`, `recommendation_ranker.go`, and `recommendation_types.go`, so the existing `store.go` does not become an even larger mixed file.

The first major change is candidate generation. Instead of one broad SQL query followed by full in-memory decoration, add several smaller source queries that each return a capped pool of event IDs plus the minimum fields needed for ranking. At minimum, implement nearby upcoming events, category-aligned events, social-proof events where friends or accepted connections are attending, and broader fallback events for sparse inventories. Each source should use explicit limits, such as 100 to 300 candidates, so the combined candidate pool stays bounded. Deduplicate by meetup ID before scoring.

The second major change is moving expensive filtering and ranking decisions closer to the database where appropriate. Distance should no longer be only a post-query filter. Add an indexed location prefilter in SQL for in-person and hybrid events when the viewer has coordinates and `distance_km` is set. If the repository does not yet use PostGIS, use a latitude/longitude bounding-box prefilter followed by exact Haversine calculation only on the already-bounded pool. Keep online events eligible when product rules require them, but make that rule explicit.

The third major change is feature enrichment and scoring. After candidate IDs are chosen, load or compute a bounded set of ranking features for those candidates. Keep existing signals such as distance, time until start, attendee count, and waitlist/fullness, but add better production signals. The first additions should be friend-attendance count, organizer quality metrics such as recent cancellation rate and average attendee turnout, viewer-category affinity using category slugs rather than category labels, and viewer-behavior affinity if lightweight engagement data already exists. If behavior data does not exist yet, explicitly leave a placeholder interface and document it instead of pretending it is available.

The fourth major change is reranking for page quality. After raw scores are computed, apply a deterministic reranker that prevents one page from being dominated by a single organizer, a single category, or one distance bucket when similarly scored alternatives exist. This does not need to be complex. A simple diversity pass that limits repeated organizers and slightly boosts underrepresented categories is enough for a first production-ready version. Also add suppression for events the viewer has repeatedly ignored if that data exists; if not, add the storage and a no-op path for now.

The fifth major change is pagination. Recommended feeds should not rely on offset semantics hidden behind a string cursor. Replace the current `decodeCursorToOffset` approach for `recommended` with a stable cursor that includes enough information to continue after the last returned event. A practical form here is a base64-encoded structure carrying the last event’s score, start time, and ID, or a server-side cached ranked ID list with cursor position for the short TTL window. The chosen approach must keep duplicate or skipped results from appearing when the user loads the second page shortly after the first.

The sixth major change is cache integration. Update `internal/meetups/cache_store.go` so the discover cache keys include any new recommendation-pipeline versioning needed to keep old and new outputs separate during rollout. If a temporary environment flag is added, make that flag part of the cache key. Consider caching only the first page for `recommended` if later pages are cheap enough from the new pipeline, but if full-page caching remains, keep the TTL short and ensure invalidation happens on event create, publish, update, cancel, delete, RSVP, and any future save/hide actions.

The seventh major change is schema and index support. Add migrations under `migrations/` and mirror the final shape in `schema/base.sql`. The likely additions are indexes for event discovery access patterns: `meetups(status, visibility, starts_at)`, `meetups(category_slug, starts_at)`, `meetups(event_type, starts_at)`, and location-supporting indexes or helper columns. If organizer-quality or viewer-event interaction features require new tables, add them explicitly in this plan with conservative defaults and idempotent migrations.

The rollout should be additive. Keep the old `recommended` path available behind a temporary environment flag, for example `MEETUPS_RECOMMENDER_V2`, threaded through `cmd/api/main.go`. During implementation and local validation, this allows direct comparison without editing code repeatedly. After tests and local checks prove the new path works and the old path is no longer needed, remove the flag and delete the legacy `recommendedScore` ranking path or reduce it to a fallback helper used only for simple sorts.

## Concrete Steps

All commands in this section should be run from `/home/michaelroddy/repos/project_radeon`.

Start by creating the ExecPlan file, then create recommendation-specific internal files under `internal/meetups/`. A minimal file set is:

    internal/meetups/recommendation_store.go
    internal/meetups/recommendation_ranker.go
    internal/meetups/recommendation_types.go
    internal/meetups/recommendation_ranker_test.go

If new schema support is needed, add one or more migrations under `migrations/` and update `schema/base.sql` to match. Then run:

    make migrate

If local PostgreSQL is not already running, point `DATABASE_URL` at a local development database before migrating. All migrations should follow the repository’s existing additive style so they are safe to rerun in development.

Refactor `internal/meetups/store.go` so `DiscoverMeetups` dispatches by `params.Sort`. Keep the current simple path available for `soonest`, `distance`, `popular`, and `newest` while `recommended` uses the new candidate-generation pipeline. Do not change the public `CursorPage[Meetup]` response shape. If stable cursor semantics require a different encoded cursor payload, keep it opaque to the client and parse it only on the server.

Update the cache layer after the recommendation path exists:

    gofmt -w ./cmd ./internal ./pkg
    GOCACHE=/tmp/go-build go test ./internal/meetups ./cmd/api

When the recommendation queries are in place, compare the current and new paths using local seeded data. A representative sequence is:

    go run ./seeds
    GOCACHE=/tmp/go-build go test ./internal/meetups ./cmd/api

Start the API locally and hit the meetup endpoint with a real token:

    curl -H "Authorization: Bearer <token>" "http://localhost:8080/meetups?limit=20"
    curl -H "Authorization: Bearer <token>" "http://localhost:8080/meetups?sort=recommended&category=coffee&distance_km=25"
    curl -H "Authorization: Bearer <token>" "http://localhost:8080/meetups?sort=soonest&category=coffee&distance_km=25"

Capture one or more `EXPLAIN (ANALYZE, BUFFERS)` comparisons for the old broad candidate query and the new source queries. Keep the artifact small and focused on proof that the new path bounds work by source rather than by total inventory.

## Validation and Acceptance

Acceptance is based on behavior, query shape, and tests.

Run:

    GOCACHE=/tmp/go-build go test ./internal/meetups ./cmd/api

and expect all tests to pass. Add or update tests proving each of the following:

The `recommended` path deduplicates events that come from multiple candidate sources.

The `recommended` path honors hard filters such as category, event type, date window, and open-spots-only while still returning a bounded candidate set.

Distance filtering is enforced correctly for in-person and hybrid events without excluding legitimate online events when product rules say they should remain eligible.

Stable cursor pagination for `recommended` does not duplicate or skip events between page one and page two for a static dataset.

The reranker does not return a page dominated by one organizer or one category when similarly scored alternatives exist.

If friend-attendance or organizer-quality signals are added, tests prove they materially affect ranking order.

Then run local manual checks against a seeded database. A successful local scenario should show:

`GET /meetups?sort=recommended` returns public future events in a sensible order, not just the soonest or nearest events mechanically sorted.

Changing category or distance filters changes the first page without obviously breaking relevance.

Fetching page two immediately after page one returns the next set of events without duplicates or visible jumps.

Running the same first-page request twice within the TTL hits the cache path and returns identical results.

After updating, publishing, cancelling, deleting, or RSVPing to an event, the next request reflects the change instead of a stale discover page.

The redesign is complete when the first page remains API-compatible, ranking quality is observably richer than the current hand-tuned score, pagination is stable, and the uncached runtime cost is no longer dominated by full-candidate decoration and in-memory sorting.

## Idempotence and Recovery

This plan is intentionally additive. New migrations must be safe to apply in development more than once via the existing migration tooling. If a migration introduces helper columns or interaction tables and a backfill fails halfway, fix the backfill and rerun it rather than dropping partially created structures.

During rollout, the old recommendation path should remain available behind a temporary environment flag. If the new path misbehaves, disable the flag, restart the API, and the old ranking path should resume immediately. Only remove the old path after local and test validation are complete.

If cache entries become confusing during local iteration, it is safe to flush local Redis:

    redis-cli FLUSHDB

Use that only against local development Redis.

## Artifacts and Notes

The current recommendation score is intentionally simple:

    score := 100
    minus distance penalty
    minus hours-until-start penalty
    plus attendee-count boost
    minus waitlist/full penalty
    plus category-interest match
    plus small online bonus

That simplicity is useful as a baseline, but it is too shallow for a production events surface because it ignores social proof, organizer reliability, repeated exposure, and page diversity.

The current discover cache key already includes viewer, filters, cursor, and limit in `internal/meetups/cache_store.go`. That means this redesign should preserve the idea of short-lived page caching, but improve the underlying uncached path rather than treating cache hits as the primary optimization.

Expected validation evidence should eventually include small excerpts such as:

    EXPLAIN (ANALYZE, BUFFERS) old broad candidate query
    Execution Time: <higher / scales with full candidate pool>

    EXPLAIN (ANALYZE, BUFFERS) new nearby source query
    Execution Time: <lower / bounded by explicit source limit>

    curl .../meetups?sort=recommended
    {
      "data": {
        "items": [...20 events...],
        "has_more": true,
        "next_cursor": "..."
      }
    }

## Interfaces and Dependencies

In `internal/meetups/types.go`, preserve `DiscoverMeetupsParams`, `Meetup`, and `CursorPage[Meetup]` as the external contract consumed by handlers and the frontend. Any new internal ranking state should live in recommendation-specific internal structs rather than changing those public JSON-facing types unnecessarily.

In `internal/meetups/store.go` or a new recommendation module, define a recommendation entry point with a stable signature, for example:

    func (s *pgStore) discoverRecommendedMeetups(ctx context.Context, userID uuid.UUID, params DiscoverMeetupsParams, viewer viewerContext) (*CursorPage[Meetup], error)

In `internal/meetups/recommendation_ranker.go`, define pure scoring helpers that can be tested without PostgreSQL, for example:

    type recommendedCandidate struct { ... }
    func rankRecommendedCandidates(candidates []recommendedCandidate, viewer viewerContext, now time.Time) []recommendedCandidate

If interaction history, organizer quality, or friend-attendance summaries require new store helpers, define those helpers in recommendation-specific files and keep their responsibilities narrow: one helper for candidate generation, one for feature hydration, one for ranking, and one for cursor slicing.

Revision note: created this plan on 2026-04-26 to turn the existing meetup discover score into a staged, production-ready recommendation pipeline without changing the public meetup discovery contract. Updated on 2026-04-26 after implementation to record the completed pipeline, added tests, local migration application, and the remaining manual validation work.
