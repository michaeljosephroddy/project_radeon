# Persist Recovery Meeting Location Codes

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

This plan follows `PLANS.md` in the repository root.

## Purpose / Big Picture

SoberSpace currently imports and returns recovery meeting address names such as `country` and `region`, but it does not persist the stable `country_code` and `region_code` fields now exported by the ingestion pipeline. After this change, the backend stores those codes from the snapshot and exposes them in recovery meeting list/detail JSON so the frontend can filter using stable identifiers while still displaying human-friendly names.

The behavior is visible by parsing a snapshot meeting that contains `country_code = "US"` and `region_code = "CA"`, importing it into `recovery_meetings`, and observing that API response structs include those same fields alongside `country = "United States"` and `region = "California"`.

## Progress

- [x] (2026-05-26T13:19:22Z) Inspected `internal/recoverymeetings/types.go`, `internal/recoverymeetings/importer.go`, `internal/recoverymeetings/store.go`, and migrations `082`, `083`, and `085`.
- [x] (2026-05-26T13:19:22Z) Created branch `feature/recovery-meeting-location-codes`.
- [x] (2026-05-26T13:25:00Z) Added nullable `country_code` and `region_code` columns plus active-code indexes in `migrations/086_recovery_meeting_location_codes.sql`.
- [x] (2026-05-26T13:30:00Z) Threaded `country_code` and `region_code` through snapshot parsing, import upsert, store scans, and JSON response types.
- [x] (2026-05-26T13:34:00Z) Allowed list and suggestion filters to match either human names or codes.
- [x] (2026-05-26T13:36:00Z) Added focused tests for snapshot parsing and SQL query generation.
- [x] (2026-05-26T13:45:00Z) Ran formatting, focused tests, full tests, vet, migration, and a full importer dry-run against the structured snapshot.

## Surprises & Discoveries

- Observation: The existing importer keeps `SnapshotSchemaVersion = "2026-04-30"` and rejects any other schema version.
    Evidence: `internal/recoverymeetings/importer.go` checks `snapshot.SchemaVersion != SnapshotSchemaVersion`.
- Observation: The current Go JSON decoder accepted additive fields during the ingestion dry-run because it uses ordinary `json.Unmarshal`, not a decoder configured to reject unknown fields.
    Evidence: The ingestion-side dry-run against `snapshots/meetings-2026-05-26T125455Z.json` completed before this backend change.
- Observation: The local backend database had migration `085_recovery_meeting_hierarchical_location_indexes.sql` pending before this work.
    Evidence: Running `go run ./cmd/migrate up` applied both `085_recovery_meeting_hierarchical_location_indexes.sql` and `086_recovery_meeting_location_codes.sql`.

## Decision Log

- Decision: Keep the snapshot schema version unchanged for this backend pass.
    Rationale: The fields are additive and optional. Keeping the version avoids blocking existing snapshots while enabling newer snapshots to populate codes.
    Date/Author: 2026-05-26, Codex.
- Decision: Preserve name-based filters and add code matching instead of replacing filter semantics.
    Rationale: Existing frontend and API clients may still send `country=United States` or `region=California`. New clients should also be able to send `country=US` and `region=CA`.
    Date/Author: 2026-05-26, Codex.

## Outcomes & Retrospective

Implemented the backend side of structured recovery meeting location codes. The importer now persists `country_code` and `region_code`, list/detail response structs include those fields, and location/region/country suggestion structs can return matching code fields. Filtering remains backward-compatible with display names and now also matches codes.

Validation:

    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./internal/recoverymeetings
    ok github.com/project_radeon/api/internal/recoverymeetings

    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
    all packages passed

    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go vet ./...
    no output

Migration and importer dry-run:

    Applied 085_recovery_meeting_hierarchical_location_indexes.sql
    Applied 086_recovery_meeting_location_codes.sql

    Recovery meeting import dry-run
    Import run: efde9acc-d09e-438c-aa7f-f57697616333
    Snapshot SHA-256: 91e531a3b71cb200c0c4a0c4bcb667881cf1d687157c1a76fb652830e66d071f
    Meetings seen: 181627
    Meetings upserted: 181627
    Occurrences written: 176062
    Marked stale: 23519
    Marked inactive: 0

No committed import was run; the verification import used `--dry-run`.

## Context and Orientation

The recovery meetings backend lives under `internal/recoverymeetings`. `types.go` defines the snapshot input types and API response types. `importer.go` parses a JSON snapshot and upserts rows into the `recovery_meetings` table. `store.go` reads active meetings back out for list, detail, and location suggestion endpoints. The table was created in `migrations/082_recovery_meetings.sql`; later migrations added search indexes.

The ingestion pipeline now exports `country_code` and `region_code` fields. These are short stable codes: for example `US` for United States, `CA` for California when the country is the United States, `CA` for Canada as a country code, and `ON` for Ontario as a Canadian region code. The distinction comes from using both fields together.

## Plan of Work

First, add migration `086_recovery_meeting_location_codes.sql` with nullable `country_code` and `region_code` columns. Add indexes that support active filters by code without removing existing name indexes.

Second, update `internal/recoverymeetings/types.go` so `SnapshotMeeting` can receive `country_code` and `region_code`, and `RecoveryMeeting`, `LocationSuggestion`, `RegionSuggestion`, and `CountrySuggestion` can return codes where useful.

Third, update `internal/recoverymeetings/importer.go` to insert and update the new columns from the snapshot using the existing `cleanStringPtr` normalization.

Fourth, update `internal/recoverymeetings/store.go` selects and scans so list/detail responses include the codes. Update filters so `country` and `region` query params match names or codes. Update suggestion queries to group and return code fields.

Finally, add focused tests around snapshot parsing and query generation, then run `gofmt`, `go test`, and `go vet`.

## Concrete Steps

Run all commands from `/home/michaelroddy/repos/project_radeon`.

Focused validation:

    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./internal/recoverymeetings

Full backend validation:

    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go vet ./...

Apply migrations locally:

    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go run ./cmd/migrate up

Dry-run the importer against the latest structured snapshot:

    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go run ./cmd/import-recovery-meetings --snapshot /home/michaelroddy/repos/recovery-meeting-ingestion/snapshots/meetings-2026-05-26T125455Z.json --dry-run

## Validation and Acceptance

Acceptance is met because a snapshot meeting containing `country_code` and `region_code` is parsed into `SnapshotMeeting`, the importer SQL writes those fields to `recovery_meetings`, store query tests show filters match by name or code, and the recovery meeting API response type includes `country_code` and `region_code`.

## Idempotence and Recovery

The migration is additive and nullable, so it can be applied to existing databases without backfilling before import. If a query change breaks compatibility, revert only the filter predicate changes while keeping the columns and import path; existing name-based behavior should remain intact throughout.

## Artifacts and Notes

The Go tool in this environment needs writable cache paths:

    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod

The recurring `/home/guest` read-only removal messages appear before Go commands in this shell environment and are unrelated to the recovery meeting code.

## Interfaces and Dependencies

The main interfaces at the end of this plan are:

- `SnapshotMeeting.CountryCode *string`
- `SnapshotMeeting.RegionCode *string`
- `RecoveryMeeting.CountryCode *string`
- `RecoveryMeeting.RegionCode *string`
- `LocationSuggestion.CountryCode *string`
- `LocationSuggestion.RegionCode *string`
- `RegionSuggestion.RegionCode string`
- `RegionSuggestion.CountryCode string`
- `CountrySuggestion.CountryCode string`
