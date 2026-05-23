package recoverymeetings

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Querier interface {
	ListRecoveryMeetings(ctx context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error)
	GetRecoveryMeeting(ctx context.Context, id uuid.UUID) (*RecoveryMeeting, error)
}

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) Querier {
	return &pgStore{pool: pool}
}

func (s *pgStore) ListRecoveryMeetings(ctx context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error) {
	limit := normalizeLimit(params.Limit)
	offset := decodeOffsetCursor(params.Cursor)

	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	query := `
		SELECT
			rm.id,
			rm.fellowship,
			rm.source_id,
			rm.source_record_id,
			rm.source_url,
			rm.name,
			rm.meeting_type,
			rm.venue_name,
			rm.address_line1,
			rm.address_line2,
			rm.city,
			rm.region,
			rm.postal_code,
			rm.country,
			rm.latitude,
			rm.longitude,
			rm.is_approximate_location,
			rm.online_url,
			rm.phone_join_info,
			rm.formats,
			rm.language,
			rm.accessibility_notes,
			rm.last_verified_at,
			rm.updated_at
		FROM recovery_meetings rm
		WHERE rm.status = 'active'
	`
	if params.Fellowship != "" {
		query += " AND rm.fellowship = " + arg(params.Fellowship)
	}
	if params.Country != "" {
		query += " AND LOWER(COALESCE(rm.country, '')) = LOWER(" + arg(params.Country) + ")"
	}
	if params.City != "" {
		query += " AND LOWER(COALESCE(rm.city, '')) = LOWER(" + arg(params.City) + ")"
	}
	if params.MeetingType != "" {
		query += " AND rm.meeting_type = " + arg(params.MeetingType)
	}
	if params.DayOfWeek != nil {
		query += ` AND EXISTS (
			SELECT 1
			FROM recovery_meeting_occurrences rmo
			WHERE rmo.recovery_meeting_id = rm.id
				AND rmo.day_of_week = ` + arg(*params.DayOfWeek) + `
		)`
	}
	if params.Query != "" {
		pattern := "%" + params.Query + "%"
		placeholder := arg(pattern)
		query += ` AND (
			rm.name ILIKE ` + placeholder + `
			OR COALESCE(rm.city, '') ILIKE ` + placeholder + `
			OR COALESCE(rm.country, '') ILIKE ` + placeholder + `
			OR COALESCE(rm.venue_name, '') ILIKE ` + placeholder + `
			OR EXISTS (
				SELECT 1
				FROM unnest(rm.formats) format
				WHERE format ILIKE ` + placeholder + `
			)
		)`
	}
	query += " ORDER BY LOWER(rm.name) ASC, rm.id ASC LIMIT " + arg(limit+1) + " OFFSET " + arg(offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	meetings := []RecoveryMeeting{}
	for rows.Next() {
		meeting, err := scanRecoveryMeeting(rows)
		if err != nil {
			return nil, err
		}
		meeting.Occurrences = []MeetingOccurrence{}
		meetings = append(meetings, *meeting)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	hasMore := len(meetings) > limit
	if hasMore {
		meetings = meetings[:limit]
	}
	if err := s.attachOccurrences(ctx, meetings); err != nil {
		return nil, err
	}

	var nextCursor *string
	if hasMore {
		next := encodeOffsetCursor(offset + len(meetings))
		nextCursor = &next
	}
	return &CursorPage[RecoveryMeeting]{
		Items:      meetings,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func (s *pgStore) GetRecoveryMeeting(ctx context.Context, id uuid.UUID) (*RecoveryMeeting, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			rm.id,
			rm.fellowship,
			rm.source_id,
			rm.source_record_id,
			rm.source_url,
			rm.name,
			rm.meeting_type,
			rm.venue_name,
			rm.address_line1,
			rm.address_line2,
			rm.city,
			rm.region,
			rm.postal_code,
			rm.country,
			rm.latitude,
			rm.longitude,
			rm.is_approximate_location,
			rm.online_url,
			rm.phone_join_info,
			rm.formats,
			rm.language,
			rm.accessibility_notes,
			rm.last_verified_at,
			rm.updated_at
		FROM recovery_meetings rm
		WHERE rm.id = $1
			AND rm.status = 'active'
	`, id)

	meeting, err := scanRecoveryMeeting(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	meeting.Occurrences = []MeetingOccurrence{}
	meetings := []RecoveryMeeting{*meeting}
	if err := s.attachOccurrences(ctx, meetings); err != nil {
		return nil, err
	}
	return &meetings[0], nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecoveryMeeting(row rowScanner) (*RecoveryMeeting, error) {
	var meeting RecoveryMeeting
	if err := row.Scan(
		&meeting.ID,
		&meeting.Fellowship,
		&meeting.SourceID,
		&meeting.SourceRecordID,
		&meeting.SourceURL,
		&meeting.Name,
		&meeting.MeetingType,
		&meeting.VenueName,
		&meeting.AddressLine1,
		&meeting.AddressLine2,
		&meeting.City,
		&meeting.Region,
		&meeting.PostalCode,
		&meeting.Country,
		&meeting.Latitude,
		&meeting.Longitude,
		&meeting.IsApproximateLocation,
		&meeting.OnlineURL,
		&meeting.PhoneJoinInfo,
		&meeting.Formats,
		&meeting.Language,
		&meeting.AccessibilityNotes,
		&meeting.LastVerifiedAt,
		&meeting.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if meeting.Formats == nil {
		meeting.Formats = []string{}
	}
	return &meeting, nil
}

func (s *pgStore) attachOccurrences(ctx context.Context, meetings []RecoveryMeeting) error {
	if len(meetings) == 0 {
		return nil
	}

	args := make([]any, 0, len(meetings))
	placeholders := make([]string, 0, len(meetings))
	index := map[uuid.UUID]int{}
	for i := range meetings {
		args = append(args, meetings[i].ID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		index[meetings[i].ID] = i
	}

	rows, err := s.pool.Query(ctx, `
		SELECT
			id,
			recovery_meeting_id,
			day_of_week::int,
			to_char(start_time_local, 'HH24:MI:SS'),
			to_char(end_time_local, 'HH24:MI:SS'),
			timezone
		FROM recovery_meeting_occurrences
		WHERE recovery_meeting_id IN (`+strings.Join(placeholders, ", ")+`)
		ORDER BY day_of_week ASC, start_time_local ASC, id ASC
	`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var meetingID uuid.UUID
		var occurrence MeetingOccurrence
		if err := rows.Scan(
			&occurrence.ID,
			&meetingID,
			&occurrence.DayOfWeek,
			&occurrence.StartTimeLocal,
			&occurrence.EndTimeLocal,
			&occurrence.Timezone,
		); err != nil {
			return err
		}
		if i, ok := index[meetingID]; ok {
			meetings[i].Occurrences = append(meetings[i].Occurrences, occurrence)
		}
	}
	return rows.Err()
}

func normalizeLimit(limit int) int {
	if limit < 1 {
		return 20
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func encodeOffsetCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeOffsetCursor(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		if value, parseErr := strconv.Atoi(raw); parseErr == nil && value > 0 {
			return value
		}
		return 0
	}
	value, err := strconv.Atoi(string(decoded))
	if err != nil || value < 0 {
		return 0
	}
	return value
}
