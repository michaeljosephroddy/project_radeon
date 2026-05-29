package reports

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

func (s *pgStore) CreateContentReport(ctx context.Context, input ContentReportInput) error {
	contextJSON := []byte(`{}`)
	if len(input.Context) > 0 {
		if data, err := json.Marshal(input.Context); err == nil {
			contextJSON = data
		}
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO content_reports (
			reporter_id,
			target_type,
			target_id,
			reason,
			details,
			context
		)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		input.ReporterID,
		input.TargetType,
		input.TargetID,
		input.Reason,
		input.Details,
		contextJSON,
	)
	return err
}
