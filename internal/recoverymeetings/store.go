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

const defaultSelectedPlaceSearchRadiusKM = 50.0

func recoveryMeetingAdminAreaFromCityExpr(alias string) string {
	city := "TRIM(COALESCE(" + alias + ".city, ''))"
	return "regexp_replace(regexp_replace(" + city + ", '^(co\\.?|county|state|province|prov\\.?|region|prefecture|department)\\s+(of\\s+)?', '', 'i'), '\\s+(north|south|east|west)$', '', 'i')"
}

func recoveryMeetingHasAdminAreaInCityExpr(alias string) string {
	return "(TRIM(COALESCE(" + alias + ".region, '')) = '' AND TRIM(COALESCE(" + alias + ".city, '')) ~* '^(co\\.?|county|state|province|prov\\.?|region|prefecture|department)\\s+')"
}

func recoveryMeetingLocationExpr(alias string) string {
	return "CASE WHEN " + recoveryMeetingHasAdminAreaInCityExpr(alias) +
		" THEN " + recoveryMeetingAdminAreaFromCityExpr(alias) +
		" ELSE TRIM(COALESCE(" + alias + ".city, '')) END"
}

func recoveryMeetingRegionExpr(alias string) string {
	return "CASE" +
		" WHEN TRIM(COALESCE(" + alias + ".region, '')) <> '' THEN TRIM(" + alias + ".region)" +
		" WHEN " + recoveryMeetingHasAdminAreaInCityExpr(alias) + " THEN " + recoveryMeetingAdminAreaFromCityExpr(alias) +
		" ELSE '' END"
}

func haversineDistanceExpr(latitudeExpr, longitudeExpr string) string {
	return "(6371.0 * 2.0 * ASIN(SQRT(POWER(SIN(RADIANS((" + latitudeExpr + " - selected_place.latitude) / 2.0)), 2) + COS(RADIANS(selected_place.latitude)) * COS(RADIANS(" + latitudeExpr + ")) * POWER(SIN(RADIANS((" + longitudeExpr + " - selected_place.longitude) / 2.0)), 2))))"
}

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
	if params.PlaceID != nil {
		return buildSelectedPlaceRecoveryMeetingListQuery(params)
	}
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
	locationExpr := recoveryMeetingLocationExpr("rm")
	regionExpr := recoveryMeetingRegionExpr("rm")
	sortLocationRank := "0"
	if location != "" {
		exactLocation := arg(location)
		sortLocationRank = `CASE
				WHEN LOWER(` + locationExpr + `) = LOWER(` + exactLocation + `) THEN 0
				WHEN LOWER(` + regionExpr + `) = LOWER(` + exactLocation + `) THEN 1
				ELSE 2
			END`
	}
	sortDistanceKM := "0::double precision"
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
			` + sortDistanceKM + ` AS sort_distance_km,
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
	queryFellowship := ""
	searchTerms := []string(nil)
	if params.Query != "" {
		queryFellowship, searchTerms = parseMeetingSearchQuery(params.Query)
	}
	effectiveFellowships := append([]string{}, params.Fellowships...)
	if queryFellowship != "" && !hasFellowshipFilter(effectiveFellowships, queryFellowship) {
		effectiveFellowships = append(effectiveFellowships, queryFellowship)
	}
	if len(effectiveFellowships) > 0 {
		query += " AND rm.fellowship = ANY(" + arg(effectiveFellowships) + ")"
	}
	if params.Country != "" {
		country := arg(params.Country)
		query += " AND (LOWER(COALESCE(rm.country, '')) = LOWER(" + country + ") OR LOWER(COALESCE(rm.country_code, '')) = LOWER(" + country + "))"
	}
	if params.Region != "" {
		region := arg(params.Region)
		query += " AND (LOWER(" + regionExpr + ") = LOWER(" + region + ") OR LOWER(COALESCE(rm.region_code, '')) = LOWER(" + region + "))"
	}
	if location != "" {
		placeholder := arg("%" + location + "%")
		query += ` AND (
			` + locationExpr + ` ILIKE ` + placeholder + `
			OR ` + locationExpr + ` || ', ' || ` + regionExpr + ` ILIKE ` + placeholder + `
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
		for _, term := range searchTerms {
			placeholder := arg("%" + term + "%")
			query += ` AND (
					rm.name ILIKE ` + placeholder + `
					OR rm.meeting_type::text ILIKE ` + placeholder + `
					OR COALESCE(rm.city, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.region, '') ILIKE ` + placeholder + `
					OR ` + locationExpr + ` ILIKE ` + placeholder + `
					OR ` + regionExpr + ` ILIKE ` + placeholder + `
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
		cursorSortDistanceKM := arg(cursor.SortDistanceKM)
		sortDay := arg(cursor.SortDay)
		sortTime := arg(cursor.SortTime)
		sortName := arg(cursor.SortName)
		id := arg(cursor.ID)
		query += ` AND (
			` + sortLocationRank + `,
			` + sortDistanceKM + `,
			COALESCE(next_occ.day_of_week, 7)::int,
			COALESCE(to_char(next_occ.start_time_local, 'HH24:MI:SS'), ''),
			LOWER(rm.name),
			rm.id
		) > (` + cursorSortLocationRank + `, ` + cursorSortDistanceKM + `, ` + sortDay + `, ` + sortTime + `, ` + sortName + `, ` + id + `)`
	}
	query += `
		ORDER BY
			sort_location_rank ASC,
			sort_distance_km ASC,
			COALESCE(next_occ.day_of_week, 7)::int ASC,
			COALESCE(to_char(next_occ.start_time_local, 'HH24:MI:SS'), '') ASC,
			LOWER(rm.name) ASC,
			rm.id ASC
		LIMIT ` + arg(limit+1)
	return query, args, limit
}

func buildSelectedPlaceRecoveryMeetingListQuery(params ListParams) (string, []any, int) {
	limit := normalizeLimit(params.Limit)
	cursor, hasCursor := decodeListCursor(params.Cursor)
	args := []any{}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	selectedPlaceID := arg(*params.PlaceID)
	radiusKM := arg(defaultSelectedPlaceSearchRadiusKM)
	meetingDistance := haversineDistanceExpr("rm.latitude", "rm.longitude")
	matchedPlaceDistance := haversineDistanceExpr("matched_place.latitude", "matched_place.longitude")
	location := strings.TrimSpace(params.Location)
	if location == "" {
		location = strings.TrimSpace(params.City)
	}
	locationExpr := recoveryMeetingLocationExpr("rm")
	regionExpr := recoveryMeetingRegionExpr("rm")
	query := `
		WITH selected_place AS (
			SELECT id, latitude, longitude, country_code, country_name
			FROM places
			WHERE id = ` + selectedPlaceID + `
		),
		candidate_meetings AS (
			SELECT
				recovery_meeting_id,
				MIN(sort_location_rank)::int AS sort_location_rank,
				MIN(distance_km)::double precision AS sort_distance_km
			FROM (
				SELECT
					rmpm.recovery_meeting_id,
					0 AS sort_location_rank,
					0::double precision AS distance_km
				FROM recovery_meeting_place_matches rmpm
				JOIN selected_place ON selected_place.id = rmpm.place_id
				UNION ALL
				SELECT
					rm.id AS recovery_meeting_id,
					1 AS sort_location_rank,
					` + meetingDistance + ` AS distance_km
				FROM recovery_meetings rm
				JOIN selected_place ON true
				WHERE rm.status = 'active'
					AND rm.latitude IS NOT NULL
					AND rm.longitude IS NOT NULL
					AND rm.latitude BETWEEN selected_place.latitude - (` + radiusKM + ` / 111.0) AND selected_place.latitude + (` + radiusKM + ` / 111.0)
					AND rm.longitude BETWEEN selected_place.longitude - (` + radiusKM + ` / (111.0 * GREATEST(COS(RADIANS(selected_place.latitude)), 0.1))) AND selected_place.longitude + (` + radiusKM + ` / (111.0 * GREATEST(COS(RADIANS(selected_place.latitude)), 0.1)))
					AND ` + meetingDistance + ` <= ` + radiusKM + `
					AND (
						NULLIF(TRIM(COALESCE(rm.country_code, '')), '') IS NULL
						OR UPPER(TRIM(rm.country_code)) = selected_place.country_code
						OR LOWER(TRIM(COALESCE(rm.country, ''))) = LOWER(COALESCE(selected_place.country_name, ''))
					)
				UNION ALL
				SELECT
					rmpm.recovery_meeting_id,
					1 AS sort_location_rank,
					` + matchedPlaceDistance + ` AS distance_km
				FROM places matched_place
				JOIN selected_place ON true
				JOIN recovery_meeting_place_matches rmpm ON rmpm.place_id = matched_place.id
				WHERE matched_place.latitude BETWEEN selected_place.latitude - (` + radiusKM + ` / 111.0) AND selected_place.latitude + (` + radiusKM + ` / 111.0)
					AND matched_place.longitude BETWEEN selected_place.longitude - (` + radiusKM + ` / (111.0 * GREATEST(COS(RADIANS(selected_place.latitude)), 0.1))) AND selected_place.longitude + (` + radiusKM + ` / (111.0 * GREATEST(COS(RADIANS(selected_place.latitude)), 0.1)))
					AND ` + matchedPlaceDistance + ` <= ` + radiusKM + `
					AND matched_place.country_code = selected_place.country_code
			) candidates
			GROUP BY recovery_meeting_id
		)
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
			candidate_meetings.sort_location_rank AS sort_location_rank,
			candidate_meetings.sort_distance_km AS sort_distance_km,
			COALESCE(next_occ.day_of_week, 7)::int AS sort_day,
			COALESCE(to_char(next_occ.start_time_local, 'HH24:MI:SS'), '') AS sort_time,
			LOWER(rm.name) AS sort_name
		FROM candidate_meetings
		JOIN recovery_meetings rm ON rm.id = candidate_meetings.recovery_meeting_id
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
	queryFellowship := ""
	searchTerms := []string(nil)
	if params.Query != "" {
		queryFellowship, searchTerms = parseMeetingSearchQuery(params.Query)
	}
	effectiveFellowships := append([]string{}, params.Fellowships...)
	if queryFellowship != "" && !hasFellowshipFilter(effectiveFellowships, queryFellowship) {
		effectiveFellowships = append(effectiveFellowships, queryFellowship)
	}
	if len(effectiveFellowships) > 0 {
		query += " AND rm.fellowship = ANY(" + arg(effectiveFellowships) + ")"
	}
	if params.Country != "" {
		country := arg(params.Country)
		query += " AND (LOWER(COALESCE(rm.country, '')) = LOWER(" + country + ") OR LOWER(COALESCE(rm.country_code, '')) = LOWER(" + country + "))"
	}
	if params.Region != "" {
		region := arg(params.Region)
		query += " AND (LOWER(" + regionExpr + ") = LOWER(" + region + ") OR LOWER(COALESCE(rm.region_code, '')) = LOWER(" + region + "))"
	}
	if location != "" {
		placeholder := arg("%" + location + "%")
		query += ` AND (
			` + locationExpr + ` ILIKE ` + placeholder + `
			OR ` + locationExpr + ` || ', ' || ` + regionExpr + ` ILIKE ` + placeholder + `
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
		for _, term := range searchTerms {
			placeholder := arg("%" + term + "%")
			query += ` AND (
					rm.name ILIKE ` + placeholder + `
					OR rm.meeting_type::text ILIKE ` + placeholder + `
					OR COALESCE(rm.city, '') ILIKE ` + placeholder + `
					OR COALESCE(rm.region, '') ILIKE ` + placeholder + `
					OR ` + locationExpr + ` ILIKE ` + placeholder + `
					OR ` + regionExpr + ` ILIKE ` + placeholder + `
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
		cursorSortDistanceKM := arg(cursor.SortDistanceKM)
		sortDay := arg(cursor.SortDay)
		sortTime := arg(cursor.SortTime)
		sortName := arg(cursor.SortName)
		id := arg(cursor.ID)
		query += ` AND (
			candidate_meetings.sort_location_rank,
			candidate_meetings.sort_distance_km,
			COALESCE(next_occ.day_of_week, 7)::int,
			COALESCE(to_char(next_occ.start_time_local, 'HH24:MI:SS'), ''),
			LOWER(rm.name),
			rm.id
		) > (` + cursorSortLocationRank + `, ` + cursorSortDistanceKM + `, ` + sortDay + `, ` + sortTime + `, ` + sortName + `, ` + id + `)`
	}
	query += `
		ORDER BY
			sort_location_rank ASC,
			sort_distance_km ASC,
			COALESCE(next_occ.day_of_week, 7)::int ASC,
			COALESCE(to_char(next_occ.start_time_local, 'HH24:MI:SS'), '') ASC,
			LOWER(rm.name) ASC,
			rm.id ASC
		LIMIT ` + arg(limit+1)
	return query, args, limit
}

func hasFellowshipFilter(fellowships []string, fellowship string) bool {
	for _, item := range fellowships {
		if item == fellowship {
			return true
		}
	}
	return false
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
		dest = append(dest, &sort.SortLocationRank, &sort.SortDistanceKM, &sort.SortDay, &sort.SortTime, &sort.SortName)
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
	SortDistanceKM   float64   `json:"dist"`
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
	if cursor.ID == uuid.Nil || cursor.SortLocationRank < 0 || cursor.SortLocationRank > 2 || cursor.SortDistanceKM < 0 || cursor.SortDay < 0 || cursor.SortDay > 7 {
		return listCursor{}, false
	}
	return cursor, true
}
