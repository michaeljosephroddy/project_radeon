package recoverymeetings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const largeDropThreshold = 0.80

var validMeetingTypes = map[string]struct{}{
	"in_person": {},
	"online":    {},
	"hybrid":    {},
	"phone":     {},
	"unknown":   {},
}

func ImportSnapshot(ctx context.Context, pool *pgxpool.Pool, opts ImportOptions) (*ImportResult, error) {
	if strings.TrimSpace(opts.SnapshotPath) == "" {
		return nil, fmt.Errorf("%w: snapshot path is required", ErrInvalidSnapshot)
	}

	bytes, err := os.ReadFile(opts.SnapshotPath)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}

	snapshot, err := ParseSnapshotBytes(bytes, opts.AllowEmpty)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(bytes)
	sha := hex.EncodeToString(sum[:])

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var activeCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*)::int FROM recovery_meetings WHERE status = 'active'`).Scan(&activeCount); err != nil {
		return nil, fmt.Errorf("count active recovery meetings: %w", err)
	}
	if activeCount > 0 && len(snapshot.Meetings) < int(float64(activeCount)*largeDropThreshold) && !opts.AllowLargeDrop {
		return nil, ErrLargeDropRejected
	}

	var importRunID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO recovery_meeting_import_runs (
			snapshot_path,
			snapshot_sha256,
			snapshot_schema_version,
			snapshot_generated_at,
			status,
			meetings_seen
		)
		VALUES ($1, $2, $3, $4, 'running', $5)
		RETURNING id
	`, opts.SnapshotPath, sha, snapshot.SchemaVersion, snapshot.GeneratedAt, len(snapshot.Meetings)).Scan(&importRunID); err != nil {
		return nil, fmt.Errorf("create import run: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE imported_recovery_meeting_keys (
			fellowship TEXT NOT NULL,
			source_id TEXT NOT NULL,
			source_record_id TEXT NOT NULL
		) ON COMMIT DROP
	`); err != nil {
		return nil, fmt.Errorf("create imported key table: %w", err)
	}

	result := &ImportResult{
		ImportRunID:    &importRunID,
		MeetingsSeen:   len(snapshot.Meetings),
		SnapshotSHA256: sha,
		DryRun:         opts.DryRun,
	}

	for _, meeting := range snapshot.Meetings {
		if _, err := tx.Exec(ctx, `
			INSERT INTO imported_recovery_meeting_keys (fellowship, source_id, source_record_id)
			VALUES ($1, $2, $3)
		`, meeting.Fellowship, meeting.SourceID, meeting.SourceRecordID); err != nil {
			return nil, fmt.Errorf("record imported key: %w", err)
		}

		var meetingID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO recovery_meetings (
				fellowship,
				source_id,
				source_record_id,
				source_url,
				name,
				meeting_type,
				venue_name,
				address_line1,
				address_line2,
				city,
				region,
				region_code,
				postal_code,
				country,
				country_code,
				latitude,
				longitude,
				is_approximate_location,
				online_url,
				phone_join_info,
				formats,
				language,
				accessibility_notes,
				status,
				missing_run_count,
				last_seen_at,
				last_verified_at,
				last_import_run_id,
				updated_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
				$11, $12, $13, $14, $15, $16, $17, $18, $19,
				$20, $21, $22, $23, 'active', 0, NOW(), $24, $25, NOW()
			)
			ON CONFLICT (fellowship, source_id, source_record_id)
			DO UPDATE SET
				source_url = EXCLUDED.source_url,
				name = EXCLUDED.name,
				meeting_type = EXCLUDED.meeting_type,
				venue_name = EXCLUDED.venue_name,
				address_line1 = EXCLUDED.address_line1,
				address_line2 = EXCLUDED.address_line2,
				city = EXCLUDED.city,
				region = EXCLUDED.region,
				region_code = EXCLUDED.region_code,
				postal_code = EXCLUDED.postal_code,
				country = EXCLUDED.country,
				country_code = EXCLUDED.country_code,
				latitude = EXCLUDED.latitude,
				longitude = EXCLUDED.longitude,
				is_approximate_location = EXCLUDED.is_approximate_location,
				online_url = EXCLUDED.online_url,
				phone_join_info = EXCLUDED.phone_join_info,
				formats = EXCLUDED.formats,
				language = EXCLUDED.language,
				accessibility_notes = EXCLUDED.accessibility_notes,
				status = 'active',
				missing_run_count = 0,
				last_seen_at = NOW(),
				last_verified_at = EXCLUDED.last_verified_at,
				last_import_run_id = EXCLUDED.last_import_run_id,
				updated_at = NOW()
			RETURNING id
		`,
			meeting.Fellowship,
			meeting.SourceID,
			meeting.SourceRecordID,
			meeting.SourceURL,
			meeting.Name,
			meeting.MeetingType,
			cleanStringPtr(meeting.VenueName),
			cleanStringPtr(meeting.AddressLine1),
			cleanStringPtr(meeting.AddressLine2),
			cleanStringPtr(meeting.City),
			cleanStringPtr(meeting.Region),
			cleanStringPtr(meeting.RegionCode),
			cleanStringPtr(meeting.PostalCode),
			cleanStringPtr(meeting.Country),
			cleanStringPtr(meeting.CountryCode),
			meeting.Latitude,
			meeting.Longitude,
			meeting.IsApproximate,
			cleanStringPtr(meeting.OnlineURL),
			cleanStringPtr(meeting.PhoneJoinInfo),
			meeting.Formats,
			cleanStringPtr(meeting.Language),
			cleanStringPtr(meeting.AccessibilityNotes),
			meeting.LastVerifiedAt,
			importRunID,
		).Scan(&meetingID); err != nil {
			return nil, fmt.Errorf("upsert recovery meeting %s/%s/%s: %w", meeting.Fellowship, meeting.SourceID, meeting.SourceRecordID, err)
		}
		result.MeetingsUpserted++

		if _, err := tx.Exec(ctx, `DELETE FROM recovery_meeting_occurrences WHERE recovery_meeting_id = $1`, meetingID); err != nil {
			return nil, fmt.Errorf("replace recovery meeting occurrences: %w", err)
		}

		for _, occurrence := range meeting.Occurrences {
			if _, err := tx.Exec(ctx, `
				INSERT INTO recovery_meeting_occurrences (
					recovery_meeting_id,
					day_of_week,
					start_time_local,
					end_time_local,
					timezone
				)
				VALUES ($1, $2, $3::time, $4::time, $5)
			`, meetingID, occurrence.DayOfWeek, occurrence.StartTimeLocal, cleanStringPtr(occurrence.EndTimeLocal), occurrence.Timezone); err != nil {
				return nil, fmt.Errorf("insert recovery meeting occurrence: %w", err)
			}
			result.OccurrencesWritten++
		}
	}

	rows, err := tx.Query(ctx, `
		WITH missing AS (
			SELECT rm.id, rm.missing_run_count + 1 AS next_count
			FROM recovery_meetings rm
			LEFT JOIN imported_recovery_meeting_keys imported
				ON imported.fellowship = rm.fellowship
				AND imported.source_id = rm.source_id
				AND imported.source_record_id = rm.source_record_id
			WHERE imported.source_id IS NULL
				AND rm.status IN ('active', 'stale')
		)
		UPDATE recovery_meetings rm
		SET
			missing_run_count = missing.next_count,
			status = CASE WHEN missing.next_count >= 3 THEN 'inactive' ELSE 'stale' END,
			updated_at = NOW()
		FROM missing
		WHERE rm.id = missing.id
		RETURNING rm.status
	`)
	if err != nil {
		return nil, fmt.Errorf("mark absent recovery meetings: %w", err)
	}
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			rows.Close()
			return nil, err
		}
		if status == "inactive" {
			result.InactiveMarked++
		} else if status == "stale" {
			result.StaleMarked++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if _, err := tx.Exec(ctx, `
		UPDATE recovery_meeting_import_runs
		SET
			status = 'succeeded',
			meetings_upserted = $2,
			occurrences_written = $3,
			stale_marked = $4,
			inactive_marked = $5,
			finished_at = NOW()
		WHERE id = $1
	`, importRunID, result.MeetingsUpserted, result.OccurrencesWritten, result.StaleMarked, result.InactiveMarked); err != nil {
		return nil, fmt.Errorf("finish import run: %w", err)
	}

	if opts.DryRun {
		if err := tx.Rollback(ctx); err != nil {
			return nil, fmt.Errorf("rollback dry run: %w", err)
		}
		committed = true
		return result, nil
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}

func ParseSnapshotBytes(bytes []byte, allowEmpty bool) (*Snapshot, error) {
	var snapshot Snapshot
	if err := json.Unmarshal(bytes, &snapshot); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}
	if err := validateSnapshot(snapshot, allowEmpty); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func validateSnapshot(snapshot Snapshot, allowEmpty bool) error {
	if snapshot.SchemaVersion != SnapshotSchemaVersion {
		return fmt.Errorf("%w: unsupported schema_version %q", ErrInvalidSnapshot, snapshot.SchemaVersion)
	}
	if snapshot.GeneratedAt.IsZero() {
		return fmt.Errorf("%w: generated_at is required", ErrInvalidSnapshot)
	}
	if len(snapshot.Meetings) == 0 && !allowEmpty {
		return fmt.Errorf("%w: meetings must not be empty", ErrInvalidSnapshot)
	}

	seen := map[string]struct{}{}
	for i, meeting := range snapshot.Meetings {
		prefix := fmt.Sprintf("meetings[%d]", i)
		if strings.TrimSpace(meeting.Fellowship) == "" {
			return fmt.Errorf("%w: %s.fellowship is required", ErrInvalidSnapshot, prefix)
		}
		if strings.TrimSpace(meeting.SourceID) == "" {
			return fmt.Errorf("%w: %s.source_id is required", ErrInvalidSnapshot, prefix)
		}
		if strings.TrimSpace(meeting.SourceRecordID) == "" {
			return fmt.Errorf("%w: %s.source_record_id is required", ErrInvalidSnapshot, prefix)
		}
		key := meeting.Fellowship + "\x00" + meeting.SourceID + "\x00" + meeting.SourceRecordID
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate source key at %s", ErrInvalidSnapshot, prefix)
		}
		seen[key] = struct{}{}
		if strings.TrimSpace(meeting.SourceURL) == "" {
			return fmt.Errorf("%w: %s.source_url is required", ErrInvalidSnapshot, prefix)
		}
		if strings.TrimSpace(meeting.Name) == "" {
			return fmt.Errorf("%w: %s.name is required", ErrInvalidSnapshot, prefix)
		}
		if _, ok := validMeetingTypes[meeting.MeetingType]; !ok {
			return fmt.Errorf("%w: %s.meeting_type is invalid", ErrInvalidSnapshot, prefix)
		}
		for j, occurrence := range meeting.Occurrences {
			occPrefix := fmt.Sprintf("%s.occurrences[%d]", prefix, j)
			if occurrence.DayOfWeek < 0 || occurrence.DayOfWeek > 6 {
				return fmt.Errorf("%w: %s.day_of_week must be between 0 and 6", ErrInvalidSnapshot, occPrefix)
			}
			if !validLocalTime(occurrence.StartTimeLocal) {
				return fmt.Errorf("%w: %s.start_time_local is invalid", ErrInvalidSnapshot, occPrefix)
			}
			if occurrence.EndTimeLocal != nil && !validLocalTime(*occurrence.EndTimeLocal) {
				return fmt.Errorf("%w: %s.end_time_local is invalid", ErrInvalidSnapshot, occPrefix)
			}
			if strings.TrimSpace(occurrence.Timezone) == "" {
				return fmt.Errorf("%w: %s.timezone is required", ErrInvalidSnapshot, occPrefix)
			}
		}
	}
	return nil
}

func validLocalTime(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if _, err := time.Parse("15:04:05", value); err == nil {
		return true
	}
	if _, err := time.Parse("15:04", value); err == nil {
		return true
	}
	return false
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
