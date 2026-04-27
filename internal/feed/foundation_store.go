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

	if err := s.refreshFeedAggregatesForPosts(ctx, []uuid.UUID{postID}); err != nil {
		return uuid.Nil, err
	}
	if err := s.refreshFeedAggregatesForShares(ctx, []uuid.UUID{shareID}); err != nil {
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
	if itemKind == FeedItemKindPost {
		return s.refreshFeedAggregatesForPosts(ctx, []uuid.UUID{itemID})
	}
	return s.refreshFeedAggregatesForShares(ctx, []uuid.UUID{itemID})
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
	if itemKind == FeedItemKindPost {
		return s.refreshFeedAggregatesForPosts(ctx, []uuid.UUID{itemID})
	}
	return s.refreshFeedAggregatesForShares(ctx, []uuid.UUID{itemID})
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

func (s *pgStore) LogFeedImpressions(ctx context.Context, userID uuid.UUID, impressions []FeedImpressionInput) error {
	if len(impressions) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	now := time.Now().UTC()
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

		batch.Queue(
			`INSERT INTO feed_impressions (
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
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (user_id, item_id, item_kind, feed_mode, session_id, served_at) DO UPDATE SET
				position = LEAST(feed_impressions.position, EXCLUDED.position),
				viewed_at = GREATEST(feed_impressions.viewed_at, EXCLUDED.viewed_at),
				view_ms = GREATEST(feed_impressions.view_ms, EXCLUDED.view_ms),
				was_clicked = feed_impressions.was_clicked OR EXCLUDED.was_clicked,
				was_liked = feed_impressions.was_liked OR EXCLUDED.was_liked,
				was_commented = feed_impressions.was_commented OR EXCLUDED.was_commented`,
			userID,
			impression.ItemID,
			string(impression.ItemKind),
			string(impression.FeedMode),
			strings.TrimSpace(impression.SessionID),
			impression.Position,
			servedAt.UTC(),
			viewedAt.UTC(),
			impression.ViewMS,
			impression.WasClicked,
			impression.WasLiked,
			impression.WasCommented,
		)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range impressions {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	if err := results.Close(); err != nil {
		return err
	}

	postIDs := make([]uuid.UUID, 0, len(impressions))
	shareIDs := make([]uuid.UUID, 0, len(impressions))
	for _, impression := range impressions {
		if impression.ItemKind == FeedItemKindPost {
			postIDs = append(postIDs, impression.ItemID)
			continue
		}
		shareIDs = append(shareIDs, impression.ItemID)
	}
	if err := s.refreshFeedAggregatesForPosts(ctx, postIDs); err != nil {
		return err
	}
	return s.refreshFeedAggregatesForShares(ctx, shareIDs)
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
