package feed

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *pgStore) SharePost(ctx context.Context, userID, postID uuid.UUID, commentary string) (uuid.UUID, error) {
	if !isFeedReshareEnabled() {
		return uuid.Nil, ErrFeedFeatureDisabled
	}

	var shareID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO post_shares (post_id, user_id, commentary)
		SELECT p.id, $1, NULLIF($3, '')
		FROM posts p
		WHERE p.id = $2
		RETURNING id`,
		userID, postID, sanitizeCommentary(commentary),
	).Scan(&shareID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}

	if _, err := s.pool.Exec(ctx,
		`UPDATE users
		SET last_active_at = GREATEST(last_active_at, NOW())
		WHERE id = $1`,
		userID,
	); err != nil {
		return uuid.Nil, err
	}

	return shareID, nil
}

func (s *pgStore) HideFeedItem(ctx context.Context, userID, itemID uuid.UUID, itemKind FeedItemKind) error {
	if !itemKind.Valid() {
		return ErrInvalidFeedItemKind
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO feed_hidden_posts (user_id, item_id, item_kind, hidden_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, item_id, item_kind) DO UPDATE
		SET hidden_at = EXCLUDED.hidden_at`,
		userID, itemID, string(itemKind),
	)
	if err != nil {
		return err
	}
	return nil
}

func (s *pgStore) UnhideFeedItem(ctx context.Context, userID, itemID uuid.UUID, itemKind FeedItemKind) error {
	if !itemKind.Valid() {
		return ErrInvalidFeedItemKind
	}

	if _, err := s.pool.Exec(ctx,
		`DELETE FROM feed_hidden_posts
		WHERE user_id = $1 AND item_id = $2 AND item_kind = $3`,
		userID, itemID, string(itemKind),
	); err != nil {
		return err
	}
	return nil
}

func (s *pgStore) MuteFeedAuthor(ctx context.Context, userID, authorID uuid.UUID) error {
	if userID == authorID {
		return nil
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO feed_muted_authors (user_id, author_id, muted_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id, author_id) DO UPDATE
		SET muted_at = EXCLUDED.muted_at`,
		userID, authorID,
	)
	return err
}

func (s *pgStore) UnmuteFeedAuthor(ctx context.Context, userID, authorID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM feed_muted_authors
		WHERE user_id = $1 AND author_id = $2`,
		userID, authorID,
	)
	return err
}

func (s *pgStore) ListMutedFeedAuthors(ctx context.Context, userID uuid.UUID, before *MutedFeedAuthorsCursor, limit int) ([]MutedFeedAuthor, error) {
	var beforeAt *time.Time
	var beforeID *uuid.UUID
	if before != nil {
		normalized := before.MutedAt.UTC()
		beforeAt = &normalized
		beforeID = &before.AuthorID
	}

	rows, err := s.pool.Query(ctx,
		`SELECT
			fma.author_id,
			fma.muted_at,
			u.id,
			u.username,
			u.avatar_url,
			u.city,
			u.country
		FROM feed_muted_authors fma
		JOIN users u ON u.id = fma.author_id
		WHERE fma.user_id = $1
			AND u.deleted_at IS NULL
			AND (
				$2::timestamptz IS NULL
				OR fma.muted_at < $2
				OR (fma.muted_at = $2 AND fma.author_id < $3::uuid)
			)
		ORDER BY fma.muted_at DESC, fma.author_id DESC
		LIMIT $4`,
		userID, beforeAt, beforeID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	authors := []MutedFeedAuthor{}
	for rows.Next() {
		var item MutedFeedAuthor
		if err := rows.Scan(
			&item.AuthorID,
			&item.MutedAt,
			&item.Author.ID,
			&item.Author.Username,
			&item.Author.AvatarURL,
			&item.Author.City,
			&item.Author.Country,
		); err != nil {
			return nil, err
		}
		authors = append(authors, item)
	}
	return authors, rows.Err()
}

func (s *pgStore) LogFeedImpressions(ctx context.Context, userID uuid.UUID, impressions []FeedImpressionInput) error {
	if len(impressions) == 0 {
		return nil
	}

	now := time.Now().UTC()
	itemIDs := make([]uuid.UUID, 0, len(impressions))
	itemKinds := make([]string, 0, len(impressions))
	feedModes := make([]string, 0, len(impressions))
	sessionIDs := make([]string, 0, len(impressions))
	positions := make([]int, 0, len(impressions))
	servedAts := make([]time.Time, 0, len(impressions))
	viewedAts := make([]time.Time, 0, len(impressions))
	viewMS := make([]int, 0, len(impressions))
	wasClicked := make([]bool, 0, len(impressions))
	wasLiked := make([]bool, 0, len(impressions))
	wasCommented := make([]bool, 0, len(impressions))

	for _, impression := range impressions {
		if !impression.ItemKind.Valid() {
			return ErrInvalidFeedItemKind
		}
		if !impression.FeedMode.Valid() {
			return ErrInvalidFeedMode
		}

		servedAt := impression.ServedAt
		if servedAt.IsZero() {
			servedAt = now
		}
		viewedAt := impression.ViewedAt
		if viewedAt.IsZero() {
			viewedAt = now
		}

		itemIDs = append(itemIDs, impression.ItemID)
		itemKinds = append(itemKinds, string(impression.ItemKind))
		feedModes = append(feedModes, string(impression.FeedMode))
		sessionIDs = append(sessionIDs, strings.TrimSpace(impression.SessionID))
		positions = append(positions, impression.Position)
		servedAts = append(servedAts, servedAt.UTC())
		viewedAts = append(viewedAts, viewedAt.UTC())
		viewMS = append(viewMS, impression.ViewMS)
		wasClicked = append(wasClicked, impression.WasClicked)
		wasLiked = append(wasLiked, impression.WasLiked)
		wasCommented = append(wasCommented, impression.WasCommented)
	}

	_, err := s.pool.Exec(ctx,
		`WITH input_rows AS (
			SELECT *
			FROM unnest(
				$2::uuid[],
				$3::text[],
				$4::text[],
				$5::text[],
				$6::int[],
				$7::timestamptz[],
				$8::timestamptz[],
				$9::int[],
				$10::boolean[],
				$11::boolean[],
				$12::boolean[]
			) AS t(
				item_id,
				item_kind,
				feed_mode,
				session_id,
				position,
				served_at,
				viewed_at,
				view_ms,
				was_clicked,
				was_liked,
				was_commented
			)
		),
		upserted AS (
			INSERT INTO feed_impressions (
				user_id,
				item_id,
				item_kind,
				feed_mode,
				session_id,
				position,
				served_at,
				viewed_at,
				view_ms,
				was_clicked,
				was_liked,
				was_commented
			)
			SELECT
				$1,
				item_id,
				item_kind,
				feed_mode,
				session_id,
				position,
				served_at,
				viewed_at,
				view_ms,
				was_clicked,
				was_liked,
				was_commented
			FROM input_rows
			ON CONFLICT (user_id, item_id, item_kind, feed_mode, session_id, served_at) DO UPDATE SET
				position = LEAST(feed_impressions.position, EXCLUDED.position),
				viewed_at = GREATEST(feed_impressions.viewed_at, EXCLUDED.viewed_at),
				view_ms = GREATEST(feed_impressions.view_ms, EXCLUDED.view_ms),
				was_clicked = feed_impressions.was_clicked OR EXCLUDED.was_clicked,
				was_liked = feed_impressions.was_liked OR EXCLUDED.was_liked,
				was_commented = feed_impressions.was_commented OR EXCLUDED.was_commented
			RETURNING item_id, item_kind
		),
		targets AS (
			SELECT DISTINCT
				CASE item_kind
					WHEN 'post' THEN 'post'
					WHEN 'reshare' THEN 'share'
				END AS target_kind,
				item_id AS target_id
			FROM upserted
		)
		INSERT INTO feed_aggregate_jobs (
			target_kind,
			target_id,
			queued_at,
			available_at,
			claimed_at,
			last_error
		)
		SELECT target_kind, target_id, NOW(), NOW(), NULL, NULL
		FROM targets
		WHERE target_kind IS NOT NULL
		ON CONFLICT (target_kind, target_id) DO UPDATE
		SET queued_at = EXCLUDED.queued_at,
			available_at = EXCLUDED.available_at,
			last_error = NULL`,
		userID,
		itemIDs,
		itemKinds,
		feedModes,
		sessionIDs,
		positions,
		servedAts,
		viewedAts,
		viewMS,
		wasClicked,
		wasLiked,
		wasCommented,
	)
	return err
}

func (s *pgStore) LogFeedEvents(ctx context.Context, userID uuid.UUID, events []FeedEventInput) error {
	if len(events) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	now := time.Now().UTC()
	for _, event := range events {
		if !event.ItemKind.Valid() {
			return ErrInvalidFeedItemKind
		}
		if !event.FeedMode.Valid() {
			return ErrInvalidFeedMode
		}
		if !event.EventType.Valid() {
			return ErrInvalidFeedEvent
		}

		eventAt := event.EventAt
		if eventAt.IsZero() {
			eventAt = now
		}

		var position any
		if event.Position != nil {
			position = *event.Position
		}

		payload := []byte("{}")
		if len(event.Payload) > 0 {
			payload = event.Payload
		}

		batch.Queue(
			`INSERT INTO feed_events (
				user_id,
				item_id,
				item_kind,
				feed_mode,
				event_type,
				position,
				event_at,
				payload
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			userID,
			event.ItemID,
			string(event.ItemKind),
			string(event.FeedMode),
			string(event.EventType),
			position,
			eventAt.UTC(),
			payload,
		)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range events {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return results.Close()
}
