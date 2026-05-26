package recoverymeetings

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var searchTokenPattern = regexp.MustCompile(`[[:alnum:]]+`)

type Querier interface {
	ListRecoveryMeetings(ctx context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error)
	ListLocationSuggestions(ctx context.Context, query, country, region, fellowship string, limit int) ([]LocationSuggestion, error)
	ListRegionSuggestions(ctx context.Context, query, country, fellowship string, limit int) ([]RegionSuggestion, error)
	ListCountrySuggestions(ctx context.Context, query, fellowship string, limit int) ([]CountrySuggestion, error)
	GetRecoveryMeeting(ctx context.Context, id uuid.UUID) (*RecoveryMeeting, error)
}

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) Querier {
	return &pgStore{pool: pool}
}

func (s *pgStore) ListRecoveryMeetings(ctx context.Context, params ListParams) (*CursorPage[RecoveryMeeting], error) {
	query, args, limit := buildRecoveryMeetingListQuery(params)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []listedRecoveryMeeting{}
	for rows.Next() {
		var item listedRecoveryMeeting
		meeting, err := scanRecoveryMeetingWithSort(rows, &item.Sort)
		if err != nil {
			return nil, err
		}
		meeting.Occurrences = []MeetingOccurrence{}
		item.Meeting = *meeting
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	meetings := make([]RecoveryMeeting, 0, len(items))
	for _, item := range items {
		meetings = append(meetings, item.Meeting)
	}
	if err := s.attachOccurrences(ctx, meetings); err != nil {
		return nil, err
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		next := encodeListCursor(items[len(items)-1].Sort)
		nextCursor = &next
	}
	return &CursorPage[RecoveryMeeting]{
		Items:      meetings,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func buildRecoveryMeetingListQuery(params ListParams) (string, []any, int) {
	limit := normalizeLimit(params.Limit)
	cursor, hasCursor := decodeListCursor(params.Cursor)
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	location := strings.TrimSpace(params.Location)
	if location == "" {
		location = strings.TrimSpace(params.City)
	}
	sortLocationRank := "0"
	if location != "" {
		exactLocation := arg(location)
		sortLocationRank = `CASE
				WHEN LOWER(COALESCE(rm.city, '')) = LOWER(` + exactLocation + `) THEN 0
				WHEN LOWER(COALESCE(rm.region, '')) = LOWER(` + exactLocation + `) THEN 1
				ELSE 2
			END`
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
			rm.region_code,
			rm.postal_code,
			rm.country,
			rm.country_code,
			rm.latitude,
			rm.longitude,
			rm.is_approximate_location,
			rm.online_url,
			rm.phone_join_info,
			rm.formats,
			rm.language,
			rm.accessibility_notes,
			rm.last_verified_at,
			rm.updated_at,
			` + sortLocationRank + ` AS sort_location_rank,
			COALESCE(next_occ.day_of_week, 7)::int AS sort_day,
			COALESCE(to_char(next_occ.start_time_local, 'HH24:MI:SS'), '') AS sort_time,
			LOWER(rm.name) AS sort_name
		FROM recovery_meetings rm
		LEFT JOIN LATERAL (
			SELECT
				rmo.day_of_week::int AS day_of_week,
				rmo.start_time_local
			FROM recovery_meeting_occurrences rmo
			WHERE rmo.recovery_meeting_id = rm.id
			ORDER BY rmo.day_of_week ASC, rmo.start_time_local ASC, rmo.id ASC
			LIMIT 1
		) next_occ ON true
		WHERE rm.status = 'active'
	`
	if params.Fellowship != "" {
		query += " AND rm.fellowship = " + arg(params.Fellowship)
	}
	if params.Country != "" {
		country := arg(params.Country)
		query += " AND (LOWER(COALESCE(rm.country, '')) = LOWER(" + country + ") OR LOWER(COALESCE(rm.country_code, '')) = LOWER(" + country + "))"
	}
	if params.Region != "" {
		region := arg(params.Region)
		query += " AND (LOWER(COALESCE(rm.region, '')) = LOWER(" + region + ") OR LOWER(COALESCE(rm.region_code, '')) = LOWER(" + region + "))"
	}
	if location != "" {
		placeholder := arg("%" + location + "%")
		query += ` AND (
			COALESCE(rm.city, '') ILIKE ` + placeholder + `
			OR COALESCE(rm.city, '') || ', ' || COALESCE(rm.region, '') ILIKE ` + placeholder + `
		)`
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
		queryFellowship, searchTerms := parseMeetingSearchQuery(params.Query)
		if queryFellowship != "" {
			query += " AND rm.fellowship = " + arg(queryFellowship)
		}
		for _, term := range searchTerms {
			placeholder := arg("%" + term + "%")
			query += ` AND (
					rm.name ILIKE ` + placeholder + `
					OR rm.meeting_type::text ILIKE ` + placeholder + `
					OR COALESCE(rm.city, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.region, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.region_code, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.country, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.country_code, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.venue_name, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.address_line1, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.address_line2, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.postal_code, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.online_url, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.phone_join_info, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.source_url, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.source_id, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.source_record_id, '') ILIKE ` + placeholder + `
					OR EXISTS (
						SELECT 1
						FROM unnest(rm.formats) format
						WHERE format ILIKE ` + placeholder + `
					)
				)`
		}
	}
	if hasCursor {
		cursorSortLocationRank := arg(cursor.SortLocationRank)
		sortDay := arg(cursor.SortDay)
		sortTime := arg(cursor.SortTime)
		sortName := arg(cursor.SortName)
		id := arg(cursor.ID)
		query += ` AND (
			` + sortLocationRank + `,
			COALESCE(next_occ.day_of_week, 7)::int,
			COALESCE(to_char(next_occ.start_time_local, 'HH24:MI:SS'), ''),
			LOWER(rm.name),
			rm.id
		) > (` + cursorSortLocationRank + `, ` + sortDay + `, ` + sortTime + `, ` + sortName + `, ` + id + `)`
	}
	query += `
		ORDER BY
			sort_location_rank ASC,
			COALESCE(next_occ.day_of_week, 7)::int ASC,
			COALESCE(to_char(next_occ.start_time_local, 'HH24:MI:SS'), '') ASC,
			LOWER(rm.name) ASC,
			rm.id ASC
		LIMIT ` + arg(limit+1)
	return query, args, limit
}

func parseMeetingSearchQuery(query string) (string, []string) {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return "", nil
	}

	fellowship := detectSearchFellowship(normalized)
	rawTokens := searchTokenPattern.FindAllString(normalized, -1)
	terms := make([]string, 0, len(rawTokens))
	seen := map[string]struct{}{}
	for _, token := range rawTokens {
		token = strings.TrimSpace(token)
		if len([]rune(token)) < 2 {
			continue
		}
		if isFellowshipSearchToken(token, fellowship) {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		terms = append(terms, token)
	}
	return fellowship, terms
}

func detectSearchFellowship(query string) string {
	switch {
	case containsRecoveryPhrase(query, "narcotics anonymous"), containsInitialism(query, "na"):
		return "na"
	case containsRecoveryPhrase(query, "cocaine anonymous"), containsInitialism(query, "ca"):
		return "ca"
	case containsRecoveryPhrase(query, "alcoholics anonymous"), containsInitialism(query, "aa"):
		return "aa"
	default:
		return ""
	}
}

func containsRecoveryPhrase(query, phrase string) bool {
	return strings.Contains(query, phrase)
}

func containsInitialism(query, initialism string) bool {
	dotted := strings.Join(strings.Split(initialism, ""), ".") + "."
	if strings.Contains(query, dotted) {
		return true
	}
	for _, token := range searchTokenPattern.FindAllString(query, -1) {
		if token == initialism {
			return true
		}
	}
	return false
}

func isFellowshipSearchToken(token, fellowship string) bool {
	switch token {
	case "aa", "ca", "na":
		return token == fellowship
	case "alcoholics", "anonymous":
		return fellowship == "aa"
	case "cocaine":
		return fellowship == "ca"
	case "narcotics":
		return fellowship == "na"
	default:
		return false
	}
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
			rm.region_code,
			rm.postal_code,
			rm.country,
			rm.country_code,
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

func (s *pgStore) ListLocationSuggestions(ctx context.Context, query, country, region, fellowship string, limit int) ([]LocationSuggestion, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return []LocationSuggestion{}, nil
	}
	country = strings.TrimSpace(country)
	if country == "" {
		return []LocationSuggestion{}, nil
	}
	limit = normalizeSuggestionLimit(limit)
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	containsPattern := "%" + query + "%"
	prefixPattern := query + "%"
	contains := arg(containsPattern)
	exact := arg(query)
	prefix := arg(prefixPattern)
	limitArg := arg(limit)
	sql := `
		WITH grouped_locations AS (
			SELECT
				TRIM(rm.city) AS location,
				NULLIF(TRIM(COALESCE(rm.region, '')), '') AS region,
				NULLIF(TRIM(COALESCE(rm.region_code, '')), '') AS region_code,
				NULLIF(TRIM(COALESCE(rm.country, '')), '') AS country,
				NULLIF(TRIM(COALESCE(rm.country_code, '')), '') AS country_code,
				COUNT(*)::int AS meeting_count
			FROM recovery_meetings rm
			WHERE rm.status = 'active'
				AND TRIM(COALESCE(rm.city, '')) <> ''
				AND (
					LOWER(COALESCE(rm.country, '')) = LOWER(` + arg(country) + `)
					OR LOWER(COALESCE(rm.country_code, '')) = LOWER(` + arg(country) + `)
				)
				AND (
					rm.city ILIKE ` + contains + `
					OR CONCAT_WS(', ', rm.city, rm.region) ILIKE ` + contains + `
				)
	`
	if region = strings.TrimSpace(region); region != "" {
		regionArg := arg(region)
		sql += " AND (LOWER(COALESCE(rm.region, '')) = LOWER(" + regionArg + ") OR LOWER(COALESCE(rm.region_code, '')) = LOWER(" + regionArg + "))"
	}
	if fellowship = strings.TrimSpace(strings.ToLower(fellowship)); fellowship != "" {
		sql += " AND rm.fellowship = " + arg(fellowship)
	}
	sql += `
			GROUP BY
				TRIM(rm.city),
				NULLIF(TRIM(COALESCE(rm.region, '')), ''),
				NULLIF(TRIM(COALESCE(rm.region_code, '')), ''),
				NULLIF(TRIM(COALESCE(rm.country, '')), ''),
				NULLIF(TRIM(COALESCE(rm.country_code, '')), '')
		)
		SELECT location, region, region_code, country, country_code, meeting_count
		FROM grouped_locations
		ORDER BY
			CASE
				WHEN LOWER(location) = LOWER(` + exact + `) THEN 0
				WHEN LOWER(location) LIKE LOWER(` + prefix + `) THEN 1
				ELSE 2
			END,
			meeting_count DESC,
			location ASC
		LIMIT ` + limitArg

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	suggestions := []LocationSuggestion{}
	for rows.Next() {
		var suggestion LocationSuggestion
		if err := rows.Scan(
			&suggestion.Location,
			&suggestion.Region,
			&suggestion.RegionCode,
			&suggestion.Country,
			&suggestion.CountryCode,
			&suggestion.MeetingCount,
		); err != nil {
			return nil, err
		}
		suggestion.Label = suggestion.Location
		if suggestion.Region != nil && strings.TrimSpace(*suggestion.Region) != "" {
			suggestion.Label += ", " + strings.TrimSpace(*suggestion.Region)
		}
		if suggestion.Country != nil && strings.TrimSpace(*suggestion.Country) != "" {
			suggestion.Label += ", " + strings.TrimSpace(*suggestion.Country)
		}
		suggestions = append(suggestions, suggestion)
	}
	return suggestions, rows.Err()
}

func (s *pgStore) ListRegionSuggestions(ctx context.Context, query, country, fellowship string, limit int) ([]RegionSuggestion, error) {
	query = strings.TrimSpace(query)
	country = strings.TrimSpace(country)
	if len([]rune(query)) < 2 || country == "" {
		return []RegionSuggestion{}, nil
	}
	limit = normalizeSuggestionLimit(limit)
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	containsPattern := "%" + query + "%"
	prefixPattern := query + "%"
	contains := arg(containsPattern)
	exact := arg(query)
	prefix := arg(prefixPattern)
	limitArg := arg(limit)
	sql := `
		SELECT
			TRIM(rm.region) AS region,
			COALESCE(NULLIF(TRIM(rm.region_code), ''), '') AS region_code,
			TRIM(rm.country) AS country,
			COALESCE(NULLIF(TRIM(rm.country_code), ''), '') AS country_code,
			COUNT(*)::int AS meeting_count
		FROM recovery_meetings rm
		WHERE rm.status = 'active'
			AND TRIM(COALESCE(rm.region, '')) <> ''
			AND (
				LOWER(COALESCE(rm.country, '')) = LOWER(` + arg(country) + `)
				OR LOWER(COALESCE(rm.country_code, '')) = LOWER(` + arg(country) + `)
			)
			AND (
				rm.region ILIKE ` + contains + `
				OR rm.region_code ILIKE ` + contains + `
			)`
	if fellowship = strings.TrimSpace(strings.ToLower(fellowship)); fellowship != "" {
		sql += " AND rm.fellowship = " + arg(fellowship)
	}
	sql += `
		GROUP BY
			TRIM(rm.region),
			COALESCE(NULLIF(TRIM(rm.region_code), ''), ''),
			TRIM(rm.country),
			COALESCE(NULLIF(TRIM(rm.country_code), ''), '')
		ORDER BY
			CASE
				WHEN LOWER(TRIM(rm.region)) = LOWER(` + exact + `) THEN 0
				WHEN LOWER(TRIM(rm.region_code)) = LOWER(` + exact + `) THEN 0
				WHEN LOWER(TRIM(rm.region)) LIKE LOWER(` + prefix + `) THEN 1
				WHEN LOWER(TRIM(rm.region_code)) LIKE LOWER(` + prefix + `) THEN 1
				ELSE 2
			END,
			meeting_count DESC,
			region ASC
		LIMIT ` + limitArg

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	suggestions := []RegionSuggestion{}
	for rows.Next() {
		var suggestion RegionSuggestion
		if err := rows.Scan(
			&suggestion.Region,
			&suggestion.RegionCode,
			&suggestion.Country,
			&suggestion.CountryCode,
			&suggestion.MeetingCount,
		); err != nil {
			return nil, err
		}
		suggestion.Label = suggestion.Region
		if strings.TrimSpace(suggestion.Country) != "" {
			suggestion.Label += ", " + strings.TrimSpace(suggestion.Country)
		}
		suggestions = append(suggestions, suggestion)
	}
	return suggestions, rows.Err()
}

func (s *pgStore) ListCountrySuggestions(ctx context.Context, query, fellowship string, limit int) ([]CountrySuggestion, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return []CountrySuggestion{}, nil
	}
	limit = normalizeSuggestionLimit(limit)
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	containsPattern := "%" + query + "%"
	prefixPattern := query + "%"
	contains := arg(containsPattern)
	exact := arg(query)
	prefix := arg(prefixPattern)
	limitArg := arg(limit)
	sql := `
		SELECT
			TRIM(rm.country) AS country,
			COALESCE(NULLIF(TRIM(rm.country_code), ''), '') AS country_code,
			COUNT(*)::int AS meeting_count
		FROM recovery_meetings rm
		WHERE rm.status = 'active'
			AND TRIM(COALESCE(rm.country, '')) <> ''
			AND (
				rm.country ILIKE ` + contains + `
				OR rm.country_code ILIKE ` + contains + `
			)`
	if fellowship = strings.TrimSpace(strings.ToLower(fellowship)); fellowship != "" {
		sql += " AND rm.fellowship = " + arg(fellowship)
	}
	sql += `
		GROUP BY TRIM(rm.country), COALESCE(NULLIF(TRIM(rm.country_code), ''), '')
		ORDER BY
			CASE
				WHEN LOWER(TRIM(rm.country)) = LOWER(` + exact + `) THEN 0
				WHEN LOWER(TRIM(rm.country_code)) = LOWER(` + exact + `) THEN 0
				WHEN LOWER(TRIM(rm.country)) LIKE LOWER(` + prefix + `) THEN 1
				WHEN LOWER(TRIM(rm.country_code)) LIKE LOWER(` + prefix + `) THEN 1
				ELSE 2
			END,
			meeting_count DESC,
			country ASC
		LIMIT ` + limitArg

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	suggestions := []CountrySuggestion{}
	for rows.Next() {
		var suggestion CountrySuggestion
		if err := rows.Scan(&suggestion.Country, &suggestion.CountryCode, &suggestion.MeetingCount); err != nil {
			return nil, err
		}
		suggestion.Label = suggestion.Country
		suggestions = append(suggestions, suggestion)
	}
	return suggestions, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecoveryMeeting(row rowScanner) (*RecoveryMeeting, error) {
	return scanRecoveryMeetingWithSort(row, nil)
}

func scanRecoveryMeetingWithSort(row rowScanner, sort *listCursor) (*RecoveryMeeting, error) {
	var meeting RecoveryMeeting
	dest := []any{
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
		&meeting.RegionCode,
		&meeting.PostalCode,
		&meeting.Country,
		&meeting.CountryCode,
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
	}
	if sort != nil {
		dest = append(dest, &sort.SortLocationRank, &sort.SortDay, &sort.SortTime, &sort.SortName)
		sort.ID = meeting.ID
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if sort != nil {
		sort.ID = meeting.ID
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

func normalizeSuggestionLimit(limit int) int {
	if limit < 1 {
		return 8
	}
	if limit > 15 {
		return 15
	}
	return limit
}

type listedRecoveryMeeting struct {
	Meeting RecoveryMeeting
	Sort    listCursor
}

type listCursor struct {
	SortLocationRank int       `json:"lr"`
	SortDay          int       `json:"d"`
	SortTime         string    `json:"t"`
	SortName         string    `json:"n"`
	ID               uuid.UUID `json:"id"`
}

func encodeListCursor(cursor listCursor) string {
	data, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeListCursor(raw string) (listCursor, bool) {
	if strings.TrimSpace(raw) == "" {
		return listCursor{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return listCursor{}, false
	}
	var cursor listCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return listCursor{}, false
	}
	if cursor.ID == uuid.Nil || cursor.SortLocationRank < 0 || cursor.SortLocationRank > 2 || cursor.SortDay < 0 || cursor.SortDay > 7 {
		return listCursor{}, false
	}
	return cursor, true
}
