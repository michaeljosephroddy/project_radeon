package places

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Querier interface {
	AutocompletePlaces(ctx context.Context, params AutocompleteParams) ([]PlaceSuggestion, error)
}

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) Querier {
	return &pgStore{pool: pool}
}

const refreshRecoveryMeetingPlaceMatchesSQL = `
	WITH active_meetings AS (
		SELECT
			rm.id,
			NULLIF(TRIM(COALESCE(rm.city, '')), '') AS city,
			NULLIF(TRIM(COALESCE(rm.region, '')), '') AS region,
			NULLIF(TRIM(COALESCE(rm.region_code, '')), '') AS region_code,
			NULLIF(TRIM(COALESCE(rm.country, '')), '') AS country,
			NULLIF(TRIM(COALESCE(rm.country_code, '')), '') AS country_code,
			rm.latitude,
			rm.longitude
		FROM recovery_meetings rm
		WHERE rm.status = 'active'
	),
	text_matches AS (
		SELECT DISTINCT ON (am.id)
			am.id AS recovery_meeting_id,
			p.id AS place_id,
			CASE
				WHEN am.region_code IS NOT NULL AND am.region_code = p.admin1_code THEN 'city_region_country'
				WHEN am.region IS NOT NULL AND LOWER(am.region) = LOWER(COALESCE(p.admin1_name, '')) THEN 'city_region_country'
				ELSE 'city_country'
			END AS match_level,
			CASE
				WHEN am.region_code IS NOT NULL AND am.region_code = p.admin1_code THEN 95
				WHEN am.region IS NOT NULL AND LOWER(am.region) = LOWER(COALESCE(p.admin1_name, '')) THEN 90
				ELSE 80
			END AS confidence,
			CONCAT_WS(', ', am.city, am.region, am.country) AS matched_text
		FROM active_meetings am
		JOIN places p ON (
			p.name_normalized = LOWER(am.city)
			OR p.ascii_name_normalized = LOWER(am.city)
		)
		WHERE am.city IS NOT NULL
			AND (
				(am.country_code IS NOT NULL AND p.country_code = am.country_code)
				OR (am.country IS NOT NULL AND LOWER(COALESCE(p.country_name, '')) = LOWER(am.country))
			)
		ORDER BY am.id, confidence DESC, p.population DESC
	),
	coordinate_matches AS (
		SELECT DISTINCT ON (am.id)
			am.id AS recovery_meeting_id,
			p.id AS place_id,
			'coordinate_nearest'::text AS match_level,
			70 AS confidence,
			CONCAT_WS(', ', am.latitude::text, am.longitude::text) AS matched_text
		FROM active_meetings am
		JOIN places p ON p.latitude BETWEEN am.latitude - 0.25 AND am.latitude + 0.25
			AND p.longitude BETWEEN am.longitude - 0.25 AND am.longitude + 0.25
		WHERE am.latitude IS NOT NULL
			AND am.longitude IS NOT NULL
			AND NOT EXISTS (
				SELECT 1 FROM text_matches tm WHERE tm.recovery_meeting_id = am.id
			)
			AND (
				(am.country_code IS NOT NULL AND p.country_code = am.country_code)
				OR (am.country_code IS NULL AND am.country IS NOT NULL AND LOWER(COALESCE(p.country_name, '')) = LOWER(am.country))
				OR (am.country_code IS NULL AND am.country IS NULL)
			)
		ORDER BY
			am.id,
			((p.latitude - am.latitude) * (p.latitude - am.latitude))
				+ ((p.longitude - am.longitude) * (p.longitude - am.longitude)) ASC,
			p.population DESC
	),
	chosen_matches AS (
		SELECT * FROM text_matches
		UNION ALL
		SELECT * FROM coordinate_matches
	),
	deleted_stale_matches AS (
		DELETE FROM recovery_meeting_place_matches existing
		USING recovery_meetings rm
		WHERE existing.recovery_meeting_id = rm.id
			AND rm.status = 'active'
			AND existing.match_level <> 'manual'
			AND NOT EXISTS (
				SELECT 1
				FROM chosen_matches chosen
				WHERE chosen.recovery_meeting_id = existing.recovery_meeting_id
			)
		RETURNING existing.recovery_meeting_id
	),
	deleted_inactive_matches AS (
		DELETE FROM recovery_meeting_place_matches existing
		USING recovery_meetings rm
		WHERE existing.recovery_meeting_id = rm.id
			AND rm.status <> 'active'
			AND existing.match_level <> 'manual'
		RETURNING existing.recovery_meeting_id
	)
	INSERT INTO recovery_meeting_place_matches (
		recovery_meeting_id,
		place_id,
		match_level,
		confidence,
		matched_text,
		updated_at
	)
	SELECT
		recovery_meeting_id,
		place_id,
		match_level,
		confidence,
		matched_text,
		NOW()
	FROM chosen_matches
	ON CONFLICT (recovery_meeting_id) DO UPDATE SET
		place_id = EXCLUDED.place_id,
		match_level = EXCLUDED.match_level,
		confidence = EXCLUDED.confidence,
		matched_text = EXCLUDED.matched_text,
		updated_at = NOW()
	WHERE recovery_meeting_place_matches.match_level <> 'manual'
`

func normalizeAutocompleteLimit(limit int) int {
	if limit < 1 {
		return 8
	}
	if limit > 10 {
		return 10
	}
	return limit
}

func (s *pgStore) AutocompletePlaces(ctx context.Context, params AutocompleteParams) ([]PlaceSuggestion, error) {
	query := strings.ToLower(strings.TrimSpace(params.Query))
	if len([]rune(query)) < 2 {
		return []PlaceSuggestion{}, nil
	}
	limit := normalizeAutocompleteLimit(params.Limit)
	args := []any{query, query + "%", "%" + query + "%", limit}
	where := `
		WHERE (
			name_normalized LIKE $2
			OR ascii_name_normalized LIKE $2
			OR search_text_normalized ILIKE $3
		)
	`
	if countryCode := strings.TrimSpace(strings.ToUpper(params.CountryCode)); countryCode != "" {
		args = append(args, countryCode)
		where += fmt.Sprintf(" AND country_code = $%d", len(args))
	}

	rows, err := s.pool.Query(ctx, `
		SELECT
			id,
			CONCAT_WS(', ', name, NULLIF(admin1_name, ''), NULLIF(country_name, country_code)) AS label,
			name,
			COALESCE(NULLIF(country_name, ''), country_code) AS country,
			country_code,
			NULLIF(admin1_name, '') AS region,
			NULLIF(admin1_code, '') AS region_code,
			latitude,
			longitude,
			population,
			source
		FROM places
	`+where+`
		ORDER BY
			CASE
				WHEN name_normalized = $1 THEN 0
				WHEN ascii_name_normalized = $1 THEN 0
				WHEN name_normalized LIKE $2 THEN 1
				WHEN ascii_name_normalized LIKE $2 THEN 1
				ELSE 2
			END,
			population DESC,
			name ASC,
			country_code ASC
		LIMIT $4
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	suggestions := []PlaceSuggestion{}
	for rows.Next() {
		var suggestion PlaceSuggestion
		if err := rows.Scan(
			&suggestion.ID,
			&suggestion.Label,
			&suggestion.Name,
			&suggestion.Country,
			&suggestion.CountryCode,
			&suggestion.Region,
			&suggestion.RegionCode,
			&suggestion.Latitude,
			&suggestion.Longitude,
			&suggestion.Population,
			&suggestion.Source,
		); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, suggestion)
	}
	return suggestions, rows.Err()
}

func RefreshRecoveryMeetingPlaceMatches(ctx context.Context, pool *pgxpool.Pool) (*MatchRefreshResult, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, refreshRecoveryMeetingPlaceMatchesSQL)
	if err != nil {
		return nil, err
	}
	var scanned int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*)::int FROM recovery_meetings WHERE status = 'active'`).Scan(&scanned); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &MatchRefreshResult{
		MeetingsScanned: scanned,
		MatchesWritten:  int(tag.RowsAffected()),
	}, nil
}
