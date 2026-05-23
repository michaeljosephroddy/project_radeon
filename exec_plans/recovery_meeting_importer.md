# Import Recovery Meeting Snapshots Into SoberSpace

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

This plan follows `PLANS.md` in the repository root. It is scoped to the SoberSpace Go backend in `/home/michaelroddy/repos/project_radeon`. Do not implement this plan until the user approves it; this repository's `AGENTS.md` requires planning before code changes and a branch for new feature work.

## Purpose / Big Picture

SoberSpace currently has a mobile support screen with mock recovery meeting data, but it does not yet have real imported recovery meetings from the separate `recovery-meeting-ingestion` service. After this change, an operator can run one backend command that reads a reviewed JSON snapshot, stores the meetings in dedicated SoberSpace recovery meeting tables, and exposes them through backend read APIs. A user-visible follow-up can then switch the mobile app from mock data to those APIs.

The first working demonstration is command-line and HTTP based. From `/home/michaelroddy/repos/project_radeon`, an operator runs:

    DATABASE_URL="postgresql://..." go run ./cmd/import-recovery-meetings \
      --snapshot /home/michaelroddy/repos/recovery-meeting-ingestion/snapshots/latest.json

The command reports the number of meetings and weekly occurrences imported. Then, with the API running, a request such as:

    curl "http://localhost:8080/recovery-meetings?fellowship=ca&format=online&limit=5"

returns real imported recovery meetings, including public online connection details such as Zoom meeting IDs and passcodes.

## Progress

- [x] (2026-05-23T23:15+01:00) Created branch `feature/recovery-meeting-importer` from clean backend `main`.
- [x] (2026-05-23T23:15+01:00) Inspected backend planning rules, migration pattern, command wiring, existing meetups/support packages, and the current snapshot shape.
- [x] (2026-05-23T23:15+01:00) Created this ExecPlan.
- [x] (2026-05-23T23:22+01:00) Implemented migration `082_recovery_meetings.sql` and updated `schema/base.sql`.
- [x] (2026-05-23T23:26+01:00) Added snapshot parsing, validation, and import logic under `internal/recoverymeetings`.
- [x] (2026-05-23T23:26+01:00) Added command `cmd/import-recovery-meetings/main.go` with dry-run and large-drop protection.
- [x] (2026-05-23T23:27+01:00) Added read APIs for recovery meetings and wired them in `cmd/api/main.go`.
- [x] (2026-05-23T23:31+01:00) Added focused parser and handler tests; ran `go test ./...`, `go vet ./...`, and `make build`.
- [x] (2026-05-23T23:33+01:00) Ran the importer against `/home/michaelroddy/repos/recovery-meeting-ingestion/snapshots/latest.json` and verified the API returns imported meetings.

## Surprises & Discoveries

- Observation: The SoberSpace backend has no existing dedicated recovery meeting tables or importer.
    Evidence: Searching `internal`, `migrations`, and `schema` for recovery meeting terms found no backend domain or table for external recovery meetings.
- Observation: The mobile app currently uses mock recovery meeting data.
    Evidence: `/home/michaelroddy/repos/project_radeon_app/src/screens/main/support/recoveryMeetingsMock.ts` defines `RECOVERY_MEETINGS` in TypeScript.
- Observation: The current reviewed snapshot is ready and unblocked.
    Evidence: `/home/michaelroddy/repos/recovery-meeting-ingestion/snapshots/latest.json` has `schema_version: "2026-04-30"` and `5136` meetings. The ingestion-side snapshot DB row `de3e6f4b-4d52-4762-812c-287a67283559` records `blocked_by_review: 0`.
- Observation: Shell startup in this environment prints attempts to remove `/home/guest/*` directories.
    Evidence: Several commands print `rm: cannot remove '/home/guest/Desktop': Read-only file system` before their real output. This appears unrelated to the repo and should not be treated as a project failure.
- Observation: The Go toolchain's default build/module cache locations are read-only in this environment.
    Evidence: `go test ./...` initially failed opening `/home/michaelroddy/.cache/go-build/...`, and `make build` initially printed a module stat cache write failure under `/home/michaelroddy/go/pkg/mod/cache`. Setting `GOCACHE=/tmp/go-build` and `GOMODCACHE=/tmp/go-mod` resolved this.
- Observation: The reviewed snapshot has fewer occurrences than meetings.
    Evidence: The importer reported `5136` meetings and `5117` occurrences. Some snapshot meetings have no weekly occurrence rows.
- Observation: The committed local import contains online meeting details.
    Evidence: Database verification counted `844` recovery meetings where `online_url` or `phone_join_info` is present.

## Decision Log

- Decision: Store imported recovery meetings in dedicated tables, not the existing `meetups` tables.
    Rationale: Imported recovery meetings are external, recurring, source-attributed data. User-created meetups are SoberSpace-owned events with organizers, RSVP state, capacity, and lifecycle rules. Mixing the two would confuse ownership, moderation, deletion, and recurrence semantics.
    Date/Author: 2026-05-23 / Codex.
- Decision: Preserve online meeting credentials and passcodes in SoberSpace.
    Rationale: The user explicitly confirmed that SoberSpace should display online meeting credentials and passcodes. The importer must not redact `phone_join_info`, `online_url`, or credential-like text from the snapshot.
    Date/Author: 2026-05-23 / Codex.
- Decision: Use `fellowship + source_id + source_record_id` as the idempotent meeting key.
    Rationale: The ingestion snapshot contract declares this as the stable identity for source-provided records. It allows repeated imports of the same snapshot or future snapshots without duplicating meetings.
    Date/Author: 2026-05-23 / Codex.
- Decision: Replace occurrences for a meeting on every import.
    Rationale: Weekly occurrences are derived from each snapshot. Replacing child rows is simpler, idempotent, and avoids stale day/time rows when a meeting changes schedule.
    Date/Author: 2026-05-23 / Codex.
- Decision: Include an authenticated read API in the backend plan, but defer the mobile app change to a separate app plan.
    Rationale: The backend import is not useful unless SoberSpace can read the imported data. The mobile app lives in a separate repository and should be switched from mocks after the backend endpoint exists and is validated.
    Date/Author: 2026-05-23 / Codex.
- Decision: Do not make `recovery_meeting_import_runs.snapshot_sha256` unique.
    Rationale: The feature must support rerunning the same reviewed snapshot idempotently. A unique checksum on import run metadata would reject the second run even though the meeting upserts are safe. The implementation keeps a normal index on `snapshot_sha256` for audit lookup.
    Date/Author: 2026-05-23 / Codex.

## Outcomes & Retrospective

Implemented and locally validated. The backend now has dedicated recovery meeting import tables, a CLI importer, authenticated read endpoints, and tests for the parser and handler behavior.

Validation results:

    GOCACHE=/tmp/go-build go test ./...
    GOCACHE=/tmp/go-build go vet ./...
    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod make build
    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod make migrate
    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go run ./cmd/import-recovery-meetings --snapshot /home/michaelroddy/repos/recovery-meeting-ingestion/snapshots/latest.json --dry-run
    GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go run ./cmd/import-recovery-meetings --snapshot /home/michaelroddy/repos/recovery-meeting-ingestion/snapshots/latest.json

The dry run and both committed imports reported `5136` meetings seen, `5136` meetings upserted, `5117` occurrences written, and no stale or inactive rows marked. Rerunning the committed import produced a second import run without duplicating meetings or failing on the checksum. A temporary authenticated API check on port `18080` returned `200` for `GET /recovery-meetings?fellowship=ca&meeting_type=online&limit=2`, with imported online meeting data in the response. The temporary test user used for that check was deleted afterward.

## Context and Orientation

This repository is the SoberSpace Go backend. The server entry point is `cmd/api/main.go`. The backend uses the chi router, `pgx`/`pgxpool` for PostgreSQL access, and raw SQL migrations under `migrations/`. The canonical schema snapshot is `schema/base.sql`. New migrations are additive numbered files, and applied migrations must not be edited.

Feature domains live under `internal/<domain>`. Existing examples include `internal/meetups`, `internal/support`, and `internal/groups`. A domain usually defines public response/request types in `types.go`, a thin HTTP handler in `handler.go`, and a PostgreSQL store in `store.go`. Constructors accept interfaces where possible so tests can use stubs.

The recovery meeting snapshot is generated by the separate repo `/home/michaelroddy/repos/recovery-meeting-ingestion`. The current reviewed file is:

    /home/michaelroddy/repos/recovery-meeting-ingestion/snapshots/latest.json

The snapshot root has:

    schema_version: "2026-04-30"
    generated_at: "2026-05-23T22:07:07.080913Z"
    meetings: [ ... ]

Each meeting has stable source identity fields:

    fellowship
    source_id
    source_record_id

Each meeting also contains display and connection data:

    source_url
    name
    meeting_type
    venue_name
    address_line1
    address_line2
    city
    region
    postal_code
    country
    latitude
    longitude
    is_approximate_location
    online_url
    phone_join_info
    formats
    language
    accessibility_notes
    last_verified_at
    occurrences

Each occurrence has:

    day_of_week
    start_time_local
    end_time_local
    timezone

The term "occurrence" in this plan means one weekly time slot for a recurring meeting. A daily meeting has seven occurrences, one for each day of the week. The term "idempotent" means that running the same import more than once updates the same rows and does not create duplicates.

## Plan of Work

Create a new package `internal/recoverymeetings`. This package owns all backend types and database logic for imported recovery meetings. It must not import other `internal/` packages. It may use standard library packages, `github.com/google/uuid`, and `github.com/jackc/pgx/v5` / `pgxpool` like existing packages.

Add migration `migrations/082_recovery_meetings.sql`. The migration creates:

1. `recovery_meeting_import_runs`, which stores metadata about each snapshot import.
2. `recovery_meetings`, which stores one row per imported external meeting.
3. `recovery_meeting_occurrences`, which stores one row per weekly occurrence for a meeting.

The migration should use UUID primary keys with `gen_random_uuid()`, timestamps with `TIMESTAMPTZ`, and raw SQL constraints. It should create indexes for listing by fellowship, country/city, meeting type, status, source identity, and occurrence day/time.

The proposed table shape is:

    CREATE TABLE IF NOT EXISTS recovery_meeting_import_runs (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
        snapshot_path TEXT NOT NULL,
        snapshot_sha256 TEXT NOT NULL UNIQUE,
        snapshot_schema_version TEXT NOT NULL,
        snapshot_generated_at TIMESTAMPTZ NOT NULL,
        status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
        meetings_seen INTEGER NOT NULL DEFAULT 0 CHECK (meetings_seen >= 0),
        meetings_upserted INTEGER NOT NULL DEFAULT 0 CHECK (meetings_upserted >= 0),
        occurrences_written INTEGER NOT NULL DEFAULT 0 CHECK (occurrences_written >= 0),
        stale_marked INTEGER NOT NULL DEFAULT 0 CHECK (stale_marked >= 0),
        inactive_marked INTEGER NOT NULL DEFAULT 0 CHECK (inactive_marked >= 0),
        error_message TEXT,
        started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        finished_at TIMESTAMPTZ
    );

    CREATE TABLE IF NOT EXISTS recovery_meetings (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
        fellowship TEXT NOT NULL,
        source_id TEXT NOT NULL,
        source_record_id TEXT NOT NULL,
        source_url TEXT NOT NULL,
        name TEXT NOT NULL,
        meeting_type TEXT NOT NULL CHECK (meeting_type IN ('in_person', 'online', 'hybrid', 'phone', 'unknown')),
        venue_name TEXT,
        address_line1 TEXT,
        address_line2 TEXT,
        city TEXT,
        region TEXT,
        postal_code TEXT,
        country TEXT,
        latitude DOUBLE PRECISION,
        longitude DOUBLE PRECISION,
        is_approximate_location BOOLEAN NOT NULL DEFAULT FALSE,
        online_url TEXT,
        phone_join_info TEXT,
        formats TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
        language TEXT,
        accessibility_notes TEXT,
        status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'stale', 'inactive')),
        missing_run_count INTEGER NOT NULL DEFAULT 0 CHECK (missing_run_count >= 0),
        first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        last_verified_at TIMESTAMPTZ,
        last_import_run_id UUID REFERENCES recovery_meeting_import_runs(id) ON DELETE SET NULL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        UNIQUE (fellowship, source_id, source_record_id)
    );

    CREATE TABLE IF NOT EXISTS recovery_meeting_occurrences (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
        recovery_meeting_id UUID NOT NULL REFERENCES recovery_meetings(id) ON DELETE CASCADE,
        day_of_week SMALLINT NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
        start_time_local TIME NOT NULL,
        end_time_local TIME,
        timezone TEXT NOT NULL,
        UNIQUE (recovery_meeting_id, day_of_week, start_time_local, COALESCE(end_time_local, '00:00'::time), timezone)
    );

Update `schema/base.sql` with the same definitions so a fresh local database has the tables without replaying every migration.

In `internal/recoverymeetings/types.go`, define Go structs for the snapshot file, domain rows, list parameters, and API responses. The snapshot structs should match the JSON field names exactly. Use `time.Time` for timestamp values, `string` for local `TIME` values while parsing JSON, and pointer fields for nullable values. Keep `phone_join_info` and `online_url` unredacted.

In `internal/recoverymeetings/importer.go`, implement:

    type ImportOptions struct {
        SnapshotPath string
        DryRun bool
        AllowEmpty bool
        AllowLargeDrop bool
    }

    type ImportResult struct {
        ImportRunID *uuid.UUID
        MeetingsSeen int
        MeetingsUpserted int
        OccurrencesWritten int
        StaleMarked int
        InactiveMarked int
        SnapshotSHA256 string
    }

    func ImportSnapshot(ctx context.Context, db DBTX, opts ImportOptions) (*ImportResult, error)

`DBTX` should be a small interface in this package that supports the pgx methods needed by the importer. The function reads and validates the JSON, computes a SHA-256 checksum of the file bytes, starts a database transaction, upserts all meetings, replaces occurrences for those meetings, marks absent meetings stale/inactive, records an import run, and commits. In dry-run mode it performs the same validation and counting inside a transaction but rolls back before returning.

Large-drop protection should compare the number of currently active `recovery_meetings` to the incoming snapshot count. If there are existing active rows and the incoming count is more than 20 percent lower, fail unless `AllowLargeDrop` is true. A zero-meeting snapshot should fail unless `AllowEmpty` is true.

Stale marking should be simple and idempotent. For meetings not present in the current snapshot, increment `missing_run_count`, set `status = 'stale'` while the count is less than `3`, and set `status = 'inactive'` when the count reaches `3`. For imported meetings, set `status = 'active'` and `missing_run_count = 0`.

In `cmd/import-recovery-meetings/main.go`, implement a CLI command that loads `.env`, connects with `pkg/database.Connect`, parses flags, calls `recoverymeetings.ImportSnapshot`, prints a concise summary, and exits non-zero on validation or database errors. Supported flags should be:

    --snapshot PATH
    --dry-run
    --allow-empty
    --allow-large-drop

The command must require `--snapshot`.

In `internal/recoverymeetings/store.go`, implement read queries for the API:

    type Querier interface {
        ListRecoveryMeetings(ctx context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error)
        GetRecoveryMeeting(ctx context.Context, id uuid.UUID) (*RecoveryMeeting, error)
    }

Use raw SQL with positional parameters. The listing query should return active meetings only. It should support conservative filters: `fellowship`, `country`, `city`, `meeting_type`, `day_of_week`, and a text query against meeting name/city/country. Do not overbuild geospatial search in this first importer pass.

In `internal/recoverymeetings/handler.go`, implement:

    GET /recovery-meetings
    GET /recovery-meetings/{id}

These routes should use the same response envelope as the rest of the backend. They should be protected with the existing authentication middleware because the app's support screen is inside the authenticated app. Parse query parameters carefully and return validation errors for invalid `day_of_week`, invalid `limit`, or unknown `meeting_type`.

Wire the package in `cmd/api/main.go` by constructing `recoverymeetings.NewHandler(recoverymeetings.NewPgStore(db))` and mounting routes inside the authenticated router group.

Add tests:

1. Unit tests for snapshot JSON parsing and validation, including the current snapshot shape.
2. Importer tests that use a temporary database when available or a narrow fake transaction interface if the package pattern supports it. At minimum, verify idempotency, occurrence replacement, stale marking, and large-drop rejection.
3. Handler tests with a stub `Querier` to verify response envelopes, query parsing, credential fields being returned, and validation failures.
4. If a local Postgres test pattern exists for migrations, add a migration smoke test or run `make migrate` manually during validation.

Do not change the React Native app in this plan. After this backend plan is complete, create a separate plan in `/home/michaelroddy/repos/project_radeon_app` to replace `recoveryMeetingsMock.ts` with API calls to these endpoints.

## Concrete Steps

Start from `/home/michaelroddy/repos/project_radeon`.

Confirm branch and baseline:

    git status --short --branch
    go test ./...
    go vet ./...
    make build

If Go cache writes fail because the sandbox cannot write under the home directory, rerun with writable caches:

    GOCACHE=/tmp/project-radeon-go-build GOMODCACHE=/tmp/project-radeon-go-mod go test ./...
    GOCACHE=/tmp/project-radeon-go-build GOMODCACHE=/tmp/project-radeon-go-mod go vet ./...
    GOCACHE=/tmp/project-radeon-go-build GOMODCACHE=/tmp/project-radeon-go-mod make build

Create the migration:

    migrations/082_recovery_meetings.sql

Update:

    schema/base.sql

Add package files:

    internal/recoverymeetings/types.go
    internal/recoverymeetings/importer.go
    internal/recoverymeetings/store.go
    internal/recoverymeetings/handler.go
    internal/recoverymeetings/importer_test.go
    internal/recoverymeetings/handler_test.go

Add command:

    cmd/import-recovery-meetings/main.go

Run formatting:

    gofmt -w cmd/import-recovery-meetings/main.go internal/recoverymeetings/*.go

Apply migrations locally:

    make migrate

Run a dry import:

    go run ./cmd/import-recovery-meetings \
      --snapshot /home/michaelroddy/repos/recovery-meeting-ingestion/snapshots/latest.json \
      --dry-run

Expected shape:

    snapshot_schema_version=2026-04-30
    meetings_seen=5136
    occurrences_written=<number greater than 5136>
    dry_run=true
    committed=false

Run the real local import:

    go run ./cmd/import-recovery-meetings \
      --snapshot /home/michaelroddy/repos/recovery-meeting-ingestion/snapshots/latest.json

Expected shape:

    status=succeeded
    meetings_seen=5136
    meetings_upserted=5136
    occurrences_written=<number greater than 5136>
    stale_marked=0
    inactive_marked=0

Run it a second time. It should not create duplicate meetings. It should report the same `meetings_seen` and keep total meeting row count stable.

Start the API:

    make run

In another shell, authenticate as usual for the local app, then call:

    curl -H "Authorization: Bearer $TOKEN" \
      "http://localhost:8080/recovery-meetings?fellowship=ca&meeting_type=online&limit=5"

The response should be a `{"data": ...}` envelope with imported meetings. At least some online/hybrid records should include `online_url` or `phone_join_info` containing public connection details.

Finish with:

    go test ./...
    go vet ./...
    make build

## Validation and Acceptance

Acceptance requires all of the following:

1. `make migrate` applies `082_recovery_meetings.sql` without errors.
2. The import command rejects missing `--snapshot`, invalid JSON, unsupported `schema_version`, and zero-meeting snapshots unless `--allow-empty` is set.
3. The dry-run command against the current snapshot reports `5136` meetings and does not write rows.
4. The real command against the current snapshot writes `5136` active recovery meetings and one or more occurrence rows per meeting.
5. Running the same import twice does not increase the count of `recovery_meetings`.
6. A fabricated follow-up snapshot missing one meeting marks that meeting `stale`; after three consecutive missing imports, it becomes `inactive`.
7. `GET /recovery-meetings` returns imported active meetings through the standard `{"data": ...}` response envelope.
8. Online credentials and passcodes from `phone_join_info` are preserved in the API response.
9. `go test ./...`, `go vet ./...`, and `make build` exit with status `0`.

## Idempotence and Recovery

The import command must be safe to rerun. It uses a transaction, so a failed import should roll back all meeting and occurrence changes from that attempt. A successful repeated import of the same snapshot updates the same rows, replaces occurrences, and does not duplicate meetings.

The migration is additive. If local testing needs a reset before production rollout, drop only the three `recovery_meeting_*` tables in a local development database, then rerun `make migrate`. Do not drop or alter existing `meetups` data.

If a snapshot import fails due to large-drop protection, inspect the snapshot file path, checksum, and meeting count before rerunning with `--allow-large-drop`. That flag should be used only after manual approval.

## Artifacts and Notes

Current snapshot sample:

    {
      "schema_version": "2026-04-30",
      "generated_at": "2026-05-23T22:07:07.080913Z",
      "meetings": [
        {
          "fellowship": "aa",
          "source_id": "aa-ie-feed",
          "source_record_id": "daily-reflection-monday",
          "source_url": "https://example.org/meetings.json",
          "name": "Daily Reflection",
          "meeting_type": "in_person",
          "city": "Dublin",
          "country": "IE",
          "online_url": null,
          "phone_join_info": null,
          "occurrences": [
            {
              "day_of_week": 1,
              "start_time_local": "19:30:00",
              "end_time_local": "20:30:00",
              "timezone": "Europe/Dublin"
            }
          ]
        }
      ]
    }

The current production candidate snapshot is:

    /home/michaelroddy/repos/recovery-meeting-ingestion/snapshots/latest.json

It contains:

    schema_version: 2026-04-30
    generated_at: 2026-05-23T22:07:07.080913Z
    meetings: 5136

## Interfaces and Dependencies

Use only existing backend dependencies unless a specific need emerges during implementation. The importer can be built with the Go standard library plus existing `pgx`, `pgxpool`, `godotenv`, and `uuid` dependencies already in `go.mod`.

The public backend package interface should include:

    package recoverymeetings

    type ImportOptions struct {
        SnapshotPath string
        DryRun bool
        AllowEmpty bool
        AllowLargeDrop bool
    }

    type ImportResult struct {
        ImportRunID *uuid.UUID
        MeetingsSeen int
        MeetingsUpserted int
        OccurrencesWritten int
        StaleMarked int
        InactiveMarked int
        SnapshotSHA256 string
    }

    func ImportSnapshot(ctx context.Context, pool *pgxpool.Pool, opts ImportOptions) (*ImportResult, error)

    type Querier interface {
        ListRecoveryMeetings(ctx context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error)
        GetRecoveryMeeting(ctx context.Context, id uuid.UUID) (*RecoveryMeeting, error)
    }

    func NewPgStore(pool *pgxpool.Pool) Querier
    func NewHandler(db Querier) *Handler

The exact internal helper signatures may change during implementation, but the command, migration, and HTTP behavior described above are the stable contract.

## Revision Notes

2026-05-23 / Codex: Initial plan created after confirming the backend has no importer, the mobile app uses mock meeting data, and the ingestion snapshot is unblocked and ready for import.
