package discovery

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// rebuildVector recomputes and persists the IDF-weighted, L2-normalised interest
// vector for a single user. Safe to call from a goroutine.
func rebuildVector(ctx context.Context, db *pgxpool.Pool, userID uuid.UUID) error {
	// Fetch all interests (ordered by ID for a stable vector layout) together
	// with per-tag user counts and the total number of users for IDF.
	rows, err := db.Query(ctx,
		`SELECT i.id, COUNT(ui.user_id), (SELECT COUNT(*) FROM users)
		 FROM interests i
		 LEFT JOIN user_interests ui ON ui.interest_id = i.id
		 GROUP BY i.id
		 ORDER BY i.id ASC`,
	)
	if err != nil {
		return fmt.Errorf("fetch interest stats: %w", err)
	}
	defer rows.Close()

	type stat struct {
		id    uuid.UUID
		count int64
		total int64
	}
	var stats []stat
	for rows.Next() {
		var s stat
		if err := rows.Scan(&s.id, &s.count, &s.total); err != nil {
			return fmt.Errorf("scan interest stat: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate interest stats: %w", err)
	}
	if len(stats) == 0 {
		return nil
	}

	// Build IDF weights and a stable index mapping from interest ID → position.
	idf := make([]float64, len(stats))
	idxByID := make(map[uuid.UUID]int, len(stats))
	for i, s := range stats {
		idxByID[s.id] = i
		idf[i] = math.Log(float64(s.total) / float64(s.count+1))
	}

	// Fetch this user's interests to populate the vector.
	iRows, err := db.Query(ctx,
		`SELECT interest_id FROM user_interests WHERE user_id = $1`, userID,
	)
	if err != nil {
		return fmt.Errorf("fetch user interests: %w", err)
	}
	defer iRows.Close()

	vec := make([]float64, len(stats))
	for iRows.Next() {
		var id uuid.UUID
		if err := iRows.Scan(&id); err != nil {
			return fmt.Errorf("scan user interest: %w", err)
		}
		if idx, ok := idxByID[id]; ok {
			vec[idx] = idf[idx]
		}
	}
	if err := iRows.Err(); err != nil {
		return fmt.Errorf("iterate user interests: %w", err)
	}

	vec = l2Normalize(vec)

	if _, err := db.Exec(ctx,
		`UPDATE users SET interest_vec = $1::float8[] WHERE id = $2`, vec, userID,
	); err != nil {
		return fmt.Errorf("update interest_vec: %w", err)
	}

	return nil
}

func l2Normalize(vec []float64) []float64 {
	var sum float64
	for _, v := range vec {
		sum += v * v
	}
	if sum == 0 {
		return vec
	}
	norm := math.Sqrt(sum)
	out := make([]float64, len(vec))
	for i, v := range vec {
		out[i] = v / norm
	}
	return out
}
