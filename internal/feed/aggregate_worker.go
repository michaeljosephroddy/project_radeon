package feed

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/observability"
)

type aggregateRefreshTargetKind string

const (
	aggregateRefreshTargetPost   aggregateRefreshTargetKind = "post"
	aggregateRefreshTargetShare  aggregateRefreshTargetKind = "share"
	aggregateRefreshTargetAuthor aggregateRefreshTargetKind = "author"
)

const aggregateRefreshClaimTimeout = 2 * time.Minute

type aggregateRefreshJob struct {
	TargetKind   aggregateRefreshTargetKind
	TargetID     uuid.UUID
	AttemptCount int
}

type AggregateRefreshWorker struct {
	store *pgStore
	now   func() time.Time
}

// NewAggregateRefreshWorker builds the background worker that drains the
// durable aggregate-refresh queue introduced to keep feed telemetry off the
// synchronous request path.
func NewAggregateRefreshWorker(pool *pgxpool.Pool) *AggregateRefreshWorker {
	return &AggregateRefreshWorker{
		store: &pgStore{pool: pool},
		now:   time.Now,
	}
}

// ProcessPendingJobs claims a bounded batch, refreshes the affected aggregate
// rows grouped by target type, and either deletes the jobs or reschedules them
// with a retry window if refresh fails.
func (w *AggregateRefreshWorker) ProcessPendingJobs(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 100
	}

	now := w.now().UTC()
	jobs, err := w.store.claimAggregateRefreshJobs(ctx, now, aggregateRefreshClaimTimeout, limit)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}

	observability.IncrementCounter("feed.aggregate_jobs.claimed", int64(len(jobs)))
	startedAt := time.Now()
	refreshErr := w.store.processAggregateRefreshJobs(ctx, jobs)
	observability.ObserveDuration("feed.aggregate_jobs.process", time.Since(startedAt), refreshErr)
	if refreshErr != nil {
		observability.IncrementCounter("feed.aggregate_jobs.failed", int64(len(jobs)))
		retryAt := w.now().UTC().Add(30 * time.Second)
		if markErr := w.store.markAggregateRefreshJobsFailed(ctx, jobs, refreshErr.Error(), retryAt); markErr != nil {
			return fmt.Errorf("refresh feed aggregate jobs: %w (mark failed: %v)", refreshErr, markErr)
		}
		return refreshErr
	}

	if err := w.store.completeAggregateRefreshJobs(ctx, jobs); err != nil {
		return err
	}
	observability.IncrementCounter("feed.aggregate_jobs.completed", int64(len(jobs)))
	return nil
}

func RunAggregateRefreshWorker(ctx context.Context, logger *log.Logger, pool *pgxpool.Pool, pollInterval time.Duration, batchSize int) {
	if pool == nil {
		return
	}
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 100
	}

	worker := NewAggregateRefreshWorker(pool)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if err := worker.ProcessPendingJobs(ctx, batchSize); err != nil && logger != nil {
			logger.Printf("feed aggregate worker: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *pgStore) claimAggregateRefreshJobs(ctx context.Context, now time.Time, claimTimeout time.Duration, limit int) ([]aggregateRefreshJob, error) {
	// SKIP LOCKED lets multiple workers drain the queue concurrently without
	// double-claiming the same aggregate target.
	rows, err := s.pool.Query(ctx, `
		WITH candidates AS (
			SELECT target_kind, target_id
			FROM feed_aggregate_jobs
			WHERE available_at <= $1
				AND (claimed_at IS NULL OR claimed_at <= $2)
			ORDER BY available_at ASC, queued_at ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE feed_aggregate_jobs jobs
		SET claimed_at = $1,
			attempt_count = jobs.attempt_count + 1
		FROM candidates
		WHERE jobs.target_kind = candidates.target_kind
			AND jobs.target_id = candidates.target_id
		RETURNING jobs.target_kind, jobs.target_id, jobs.attempt_count
	`, now, now.Add(-claimTimeout), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]aggregateRefreshJob, 0, limit)
	for rows.Next() {
		var job aggregateRefreshJob
		var targetKind string
		if err := rows.Scan(&targetKind, &job.TargetID, &job.AttemptCount); err != nil {
			return nil, err
		}
		job.TargetKind = aggregateRefreshTargetKind(targetKind)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *pgStore) processAggregateRefreshJobs(ctx context.Context, jobs []aggregateRefreshJob) error {
	// Refreshing by target type keeps the heavy aggregate SQL set-based instead
	// of replaying one job at a time.
	postIDs := make([]uuid.UUID, 0, len(jobs))
	shareIDs := make([]uuid.UUID, 0, len(jobs))
	authorIDs := make([]uuid.UUID, 0, len(jobs))

	for _, job := range jobs {
		switch job.TargetKind {
		case aggregateRefreshTargetPost:
			postIDs = append(postIDs, job.TargetID)
		case aggregateRefreshTargetShare:
			shareIDs = append(shareIDs, job.TargetID)
		case aggregateRefreshTargetAuthor:
			authorIDs = append(authorIDs, job.TargetID)
		default:
			return fmt.Errorf("unknown feed aggregate target kind %q", job.TargetKind)
		}
	}

	if err := s.refreshFeedAggregatesForPosts(ctx, postIDs); err != nil {
		return err
	}
	if err := s.refreshFeedAggregatesForShares(ctx, shareIDs); err != nil {
		return err
	}
	if err := s.refreshAuthorFeedStats(ctx, authorIDs); err != nil {
		return err
	}
	return nil
}

func (s *pgStore) completeAggregateRefreshJobs(ctx context.Context, jobs []aggregateRefreshJob) error {
	query, args := buildAggregateRefreshJobsWhereClause(
		`DELETE FROM feed_aggregate_jobs WHERE `,
		jobs,
		0,
	)
	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

func (s *pgStore) markAggregateRefreshJobsFailed(ctx context.Context, jobs []aggregateRefreshJob, message string, retryAt time.Time) error {
	query, args := buildAggregateRefreshJobsWhereClause(
		`UPDATE feed_aggregate_jobs
		SET claimed_at = NULL,
			available_at = $1,
			last_error = $2
		WHERE `,
		jobs,
		2,
	)
	args = append([]any{retryAt, truncateAggregateRefreshError(message)}, args...)
	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

func buildAggregateRefreshJobsWhereClause(prefix string, jobs []aggregateRefreshJob, placeholderOffset int) (string, []any) {
	args := make([]any, 0, len(jobs)*2)
	var builder strings.Builder
	builder.WriteString(prefix)
	builder.WriteString("(target_kind, target_id) IN (")
	for index, job := range jobs {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(fmt.Sprintf("($%d, $%d)", placeholderOffset+(index*2)+1, placeholderOffset+(index*2)+2))
		args = append(args, string(job.TargetKind), job.TargetID)
	}
	builder.WriteString(")")
	return builder.String(), args
}

func truncateAggregateRefreshError(message string) string {
	const maxLen = 512
	if len(message) <= maxLen {
		return message
	}
	return message[:maxLen]
}
