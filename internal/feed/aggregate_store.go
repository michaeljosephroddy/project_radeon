package feed

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type shareAggregateContext struct {
	ShareID        uuid.UUID
	ShareAuthorID  uuid.UUID
	OriginalPostID uuid.UUID
}

func dedupeUUIDs(ids []uuid.UUID) []uuid.UUID {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *pgStore) refreshFeedAggregatesForPosts(ctx context.Context, postIDs []uuid.UUID) error {
	postIDs = dedupeUUIDs(postIDs)
	if len(postIDs) == 0 {
		return nil
	}

	if err := s.refreshPostQualityFeatures(ctx, postIDs); err != nil {
		return err
	}

	authorIDs, err := s.lookupPostAuthorIDs(ctx, postIDs)
	if err != nil {
		return err
	}
	return s.refreshAuthorFeedStats(ctx, authorIDs)
}

func (s *pgStore) refreshFeedAggregatesForShares(ctx context.Context, shareIDs []uuid.UUID) error {
	shareIDs = dedupeUUIDs(shareIDs)
	if len(shareIDs) == 0 {
		return nil
	}

	contexts, err := s.lookupShareAggregateContext(ctx, shareIDs)
	if err != nil {
		return err
	}
	if len(contexts) == 0 {
		return nil
	}

	if err := s.refreshShareQualityFeatures(ctx, shareIDs); err != nil {
		return err
	}

	postIDs := make([]uuid.UUID, 0, len(contexts))
	authorIDs := make([]uuid.UUID, 0, len(contexts))
	for _, item := range contexts {
		postIDs = append(postIDs, item.OriginalPostID)
		authorIDs = append(authorIDs, item.ShareAuthorID)
	}
	if err := s.refreshPostQualityFeatures(ctx, postIDs); err != nil {
		return err
	}

	postAuthorIDs, err := s.lookupPostAuthorIDs(ctx, postIDs)
	if err != nil {
		return err
	}
	authorIDs = append(authorIDs, postAuthorIDs...)
	return s.refreshAuthorFeedStats(ctx, authorIDs)
}

func (s *pgStore) lookupPostAuthorIDs(ctx context.Context, postIDs []uuid.UUID) ([]uuid.UUID, error) {
	postIDs = dedupeUUIDs(postIDs)
	if len(postIDs) == 0 {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx, `SELECT DISTINCT user_id FROM posts WHERE id = ANY($1::uuid[])`, postIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	authorIDs := make([]uuid.UUID, 0, len(postIDs))
	for rows.Next() {
		var authorID uuid.UUID
		if err := rows.Scan(&authorID); err != nil {
			return nil, err
		}
		authorIDs = append(authorIDs, authorID)
	}
	return authorIDs, rows.Err()
}

func (s *pgStore) lookupShareAggregateContext(ctx context.Context, shareIDs []uuid.UUID) ([]shareAggregateContext, error) {
	shareIDs = dedupeUUIDs(shareIDs)
	if len(shareIDs) == 0 {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, post_id
		FROM post_shares
		WHERE id = ANY($1::uuid[])`,
		shareIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]shareAggregateContext, 0, len(shareIDs))
	for rows.Next() {
		var item shareAggregateContext
		if err := rows.Scan(&item.ShareID, &item.ShareAuthorID, &item.OriginalPostID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *pgStore) refreshPostQualityFeatures(ctx context.Context, postIDs []uuid.UUID) error {
	postIDs = dedupeUUIDs(postIDs)
	if len(postIDs) == 0 {
		return nil
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO post_quality_features (
			post_id,
			author_id,
			has_body,
			has_image,
			body_length,
			total_impression_count,
			total_like_count,
			total_comment_count,
			total_share_count,
			total_hide_count,
			recent_impression_count,
			recent_like_count,
			recent_comment_count,
			recent_share_count,
			recent_hide_count,
			quality_score,
			last_engagement_at,
			updated_at
		)
		SELECT
			p.id,
			p.user_id,
			COALESCE(NULLIF(BTRIM(p.body), ''), '') <> '' AS has_body,
			EXISTS(SELECT 1 FROM post_images pi WHERE pi.post_id = p.id) AS has_image,
			CHAR_LENGTH(COALESCE(p.body, '')) AS body_length,
			COALESCE(total_impressions.cnt, 0) AS total_impression_count,
			COALESCE(total_likes.cnt, 0) AS total_like_count,
			COALESCE(total_comments.cnt, 0) AS total_comment_count,
			COALESCE(total_shares.cnt, 0) AS total_share_count,
			COALESCE(total_hides.cnt, 0) AS total_hide_count,
			COALESCE(recent_impressions.cnt, 0) AS recent_impression_count,
			COALESCE(recent_likes.cnt, 0) AS recent_like_count,
			COALESCE(recent_comments.cnt, 0) AS recent_comment_count,
			COALESCE(recent_shares.cnt, 0) AS recent_share_count,
			COALESCE(recent_hides.cnt, 0) AS recent_hide_count,
			(
				LEAST(COALESCE(total_likes.cnt, 0), 100) * 0.8
				+ LEAST(COALESCE(total_comments.cnt, 0), 100) * 1.3
				+ LEAST(COALESCE(total_shares.cnt, 0), 100) * 1.1
				+ CASE WHEN EXISTS(SELECT 1 FROM post_images pi WHERE pi.post_id = p.id) THEN 4 ELSE 0 END
				- LEAST(COALESCE(total_hides.cnt, 0), 100) * 2.0
			)::double precision AS quality_score,
			GREATEST(
				p.created_at,
				COALESCE(total_comments.last_at, p.created_at),
				COALESCE(total_shares.last_at, p.created_at),
				COALESCE(total_impressions.last_at, p.created_at),
				COALESCE(total_hides.last_at, p.created_at)
			) AS last_engagement_at,
			$2::timestamptz
		FROM posts p
		LEFT JOIN (
			SELECT item_id AS post_id, COUNT(*)::int AS cnt, MAX(viewed_at) AS last_at
			FROM feed_impressions
			WHERE item_kind = 'post'
			GROUP BY item_id
		) total_impressions ON total_impressions.post_id = p.id
		LEFT JOIN (
			SELECT item_id AS post_id, COUNT(*)::int AS cnt
			FROM feed_impressions
			WHERE item_kind = 'post' AND viewed_at >= NOW() - INTERVAL '14 days'
			GROUP BY item_id
		) recent_impressions ON recent_impressions.post_id = p.id
		LEFT JOIN (
			SELECT post_id, COUNT(*)::int AS cnt
			FROM post_reactions
			WHERE type = 'like'
			GROUP BY post_id
		) total_likes ON total_likes.post_id = p.id
		LEFT JOIN (
			SELECT item_id AS post_id, COUNT(*)::int AS cnt
			FROM feed_events
			WHERE item_kind = 'post' AND event_type = 'like' AND event_at >= NOW() - INTERVAL '14 days'
			GROUP BY item_id
		) recent_likes ON recent_likes.post_id = p.id
		LEFT JOIN (
			SELECT post_id, COUNT(*)::int AS cnt, MAX(created_at) AS last_at
			FROM comments
			GROUP BY post_id
		) total_comments ON total_comments.post_id = p.id
		LEFT JOIN (
			SELECT post_id, COUNT(*)::int AS cnt
			FROM comments
			WHERE created_at >= NOW() - INTERVAL '14 days'
			GROUP BY post_id
		) recent_comments ON recent_comments.post_id = p.id
		LEFT JOIN (
			SELECT post_id, COUNT(*)::int AS cnt, MAX(created_at) AS last_at
			FROM post_shares
			GROUP BY post_id
		) total_shares ON total_shares.post_id = p.id
		LEFT JOIN (
			SELECT post_id, COUNT(*)::int AS cnt
			FROM post_shares
			WHERE created_at >= NOW() - INTERVAL '14 days'
			GROUP BY post_id
		) recent_shares ON recent_shares.post_id = p.id
		LEFT JOIN (
			SELECT item_id AS post_id, COUNT(*)::int AS cnt, MAX(hidden_at) AS last_at
			FROM feed_hidden_posts
			WHERE item_kind = 'post'
			GROUP BY item_id
		) total_hides ON total_hides.post_id = p.id
		LEFT JOIN (
			SELECT item_id AS post_id, COUNT(*)::int AS cnt
			FROM feed_hidden_posts
			WHERE item_kind = 'post' AND hidden_at >= NOW() - INTERVAL '14 days'
			GROUP BY item_id
		) recent_hides ON recent_hides.post_id = p.id
		WHERE p.id = ANY($1::uuid[])
		ON CONFLICT (post_id) DO UPDATE SET
			author_id = EXCLUDED.author_id,
			has_body = EXCLUDED.has_body,
			has_image = EXCLUDED.has_image,
			body_length = EXCLUDED.body_length,
			total_impression_count = EXCLUDED.total_impression_count,
			total_like_count = EXCLUDED.total_like_count,
			total_comment_count = EXCLUDED.total_comment_count,
			total_share_count = EXCLUDED.total_share_count,
			total_hide_count = EXCLUDED.total_hide_count,
			recent_impression_count = EXCLUDED.recent_impression_count,
			recent_like_count = EXCLUDED.recent_like_count,
			recent_comment_count = EXCLUDED.recent_comment_count,
			recent_share_count = EXCLUDED.recent_share_count,
			recent_hide_count = EXCLUDED.recent_hide_count,
			quality_score = EXCLUDED.quality_score,
			last_engagement_at = EXCLUDED.last_engagement_at,
			updated_at = EXCLUDED.updated_at
	`, postIDs, time.Now().UTC())
	return err
}

func (s *pgStore) refreshShareQualityFeatures(ctx context.Context, shareIDs []uuid.UUID) error {
	shareIDs = dedupeUUIDs(shareIDs)
	if len(shareIDs) == 0 {
		return nil
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO share_quality_features (
			share_id,
			author_id,
			original_post_id,
			has_commentary,
			commentary_length,
			total_impression_count,
			total_like_count,
			total_comment_count,
			total_hide_count,
			recent_impression_count,
			recent_like_count,
			recent_comment_count,
			recent_hide_count,
			quality_score,
			last_engagement_at,
			updated_at
		)
		SELECT
			ps.id,
			ps.user_id,
			ps.post_id,
			COALESCE(NULLIF(BTRIM(ps.commentary), ''), '') <> '' AS has_commentary,
			CHAR_LENGTH(COALESCE(ps.commentary, '')) AS commentary_length,
			COALESCE(total_impressions.cnt, 0) AS total_impression_count,
			COALESCE(total_likes.cnt, 0) AS total_like_count,
			COALESCE(total_comments.cnt, 0) AS total_comment_count,
			COALESCE(total_hides.cnt, 0) AS total_hide_count,
			COALESCE(recent_impressions.cnt, 0) AS recent_impression_count,
			COALESCE(recent_likes.cnt, 0) AS recent_like_count,
			COALESCE(recent_comments.cnt, 0) AS recent_comment_count,
			COALESCE(recent_hides.cnt, 0) AS recent_hide_count,
			(
				LEAST(COALESCE(total_likes.cnt, 0), 100) * 0.8
				+ LEAST(COALESCE(total_comments.cnt, 0), 100) * 1.1
				+ CASE WHEN COALESCE(NULLIF(BTRIM(ps.commentary), ''), '') <> '' THEN 3 ELSE 0 END
				- LEAST(COALESCE(total_hides.cnt, 0), 100) * 1.8
			)::double precision AS quality_score,
			GREATEST(
				ps.created_at,
				COALESCE(total_comments.last_at, ps.created_at),
				COALESCE(total_likes.last_at, ps.created_at),
				COALESCE(total_impressions.last_at, ps.created_at),
				COALESCE(total_hides.last_at, ps.created_at)
			) AS last_engagement_at,
			$2::timestamptz
		FROM post_shares ps
		LEFT JOIN (
			SELECT item_id AS share_id, COUNT(*)::int AS cnt, MAX(viewed_at) AS last_at
			FROM feed_impressions
			WHERE item_kind = 'reshare'
			GROUP BY item_id
		) total_impressions ON total_impressions.share_id = ps.id
		LEFT JOIN (
			SELECT item_id AS share_id, COUNT(*)::int AS cnt
			FROM feed_impressions
			WHERE item_kind = 'reshare' AND viewed_at >= NOW() - INTERVAL '14 days'
			GROUP BY item_id
		) recent_impressions ON recent_impressions.share_id = ps.id
		LEFT JOIN (
			SELECT share_id, COUNT(*)::int AS cnt, MAX(created_at) AS last_at
			FROM share_reactions
			WHERE type = 'like'
			GROUP BY share_id
		) total_likes ON total_likes.share_id = ps.id
		LEFT JOIN (
			SELECT item_id AS share_id, COUNT(*)::int AS cnt
			FROM feed_events
			WHERE item_kind = 'reshare' AND event_type = 'like' AND event_at >= NOW() - INTERVAL '14 days'
			GROUP BY item_id
		) recent_likes ON recent_likes.share_id = ps.id
		LEFT JOIN (
			SELECT share_id, COUNT(*)::int AS cnt, MAX(created_at) AS last_at
			FROM share_comments
			GROUP BY share_id
		) total_comments ON total_comments.share_id = ps.id
		LEFT JOIN (
			SELECT share_id, COUNT(*)::int AS cnt
			FROM share_comments
			WHERE created_at >= NOW() - INTERVAL '14 days'
			GROUP BY share_id
		) recent_comments ON recent_comments.share_id = ps.id
		LEFT JOIN (
			SELECT item_id AS share_id, COUNT(*)::int AS cnt, MAX(hidden_at) AS last_at
			FROM feed_hidden_posts
			WHERE item_kind = 'reshare'
			GROUP BY item_id
		) total_hides ON total_hides.share_id = ps.id
		LEFT JOIN (
			SELECT item_id AS share_id, COUNT(*)::int AS cnt
			FROM feed_hidden_posts
			WHERE item_kind = 'reshare' AND hidden_at >= NOW() - INTERVAL '14 days'
			GROUP BY item_id
		) recent_hides ON recent_hides.share_id = ps.id
		WHERE ps.id = ANY($1::uuid[])
		ON CONFLICT (share_id) DO UPDATE SET
			author_id = EXCLUDED.author_id,
			original_post_id = EXCLUDED.original_post_id,
			has_commentary = EXCLUDED.has_commentary,
			commentary_length = EXCLUDED.commentary_length,
			total_impression_count = EXCLUDED.total_impression_count,
			total_like_count = EXCLUDED.total_like_count,
			total_comment_count = EXCLUDED.total_comment_count,
			total_hide_count = EXCLUDED.total_hide_count,
			recent_impression_count = EXCLUDED.recent_impression_count,
			recent_like_count = EXCLUDED.recent_like_count,
			recent_comment_count = EXCLUDED.recent_comment_count,
			recent_hide_count = EXCLUDED.recent_hide_count,
			quality_score = EXCLUDED.quality_score,
			last_engagement_at = EXCLUDED.last_engagement_at,
			updated_at = EXCLUDED.updated_at
	`, shareIDs, time.Now().UTC())
	return err
}

func (s *pgStore) refreshAuthorFeedStats(ctx context.Context, authorIDs []uuid.UUID) error {
	authorIDs = dedupeUUIDs(authorIDs)
	if len(authorIDs) == 0 {
		return nil
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO author_feed_stats (
			author_id,
			recent_post_count,
			recent_share_count,
			rolling_impression_count,
			rolling_like_count,
			rolling_comment_count,
			rolling_hide_count,
			last_post_at,
			last_share_at,
			updated_at
		)
		SELECT
			u.id,
			COALESCE(posts_14d.cnt, 0) AS recent_post_count,
			COALESCE(shares_14d.cnt, 0) AS recent_share_count,
			COALESCE(post_impressions.cnt, 0) + COALESCE(share_impressions.cnt, 0) AS rolling_impression_count,
			COALESCE(post_likes.cnt, 0) + COALESCE(share_likes.cnt, 0) AS rolling_like_count,
			COALESCE(post_comments.cnt, 0) + COALESCE(share_comments.cnt, 0) AS rolling_comment_count,
			COALESCE(post_hides.cnt, 0) + COALESCE(share_hides.cnt, 0) AS rolling_hide_count,
			posts_last.last_at AS last_post_at,
			shares_last.last_at AS last_share_at,
			$2::timestamptz
		FROM users u
		LEFT JOIN (
			SELECT user_id, COUNT(*)::int AS cnt
			FROM posts
			WHERE created_at >= NOW() - INTERVAL '14 days'
			GROUP BY user_id
		) posts_14d ON posts_14d.user_id = u.id
		LEFT JOIN (
			SELECT user_id, MAX(created_at) AS last_at
			FROM posts
			GROUP BY user_id
		) posts_last ON posts_last.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*)::int AS cnt
			FROM post_shares
			WHERE created_at >= NOW() - INTERVAL '14 days'
			GROUP BY user_id
		) shares_14d ON shares_14d.user_id = u.id
		LEFT JOIN (
			SELECT user_id, MAX(created_at) AS last_at
			FROM post_shares
			GROUP BY user_id
		) shares_last ON shares_last.user_id = u.id
		LEFT JOIN (
			SELECT p.user_id, COUNT(*)::int AS cnt
			FROM posts p
			JOIN feed_impressions fi ON fi.item_id = p.id AND fi.item_kind = 'post'
			WHERE fi.viewed_at >= NOW() - INTERVAL '30 days'
			GROUP BY p.user_id
		) post_impressions ON post_impressions.user_id = u.id
		LEFT JOIN (
			SELECT ps.user_id, COUNT(*)::int AS cnt
			FROM post_shares ps
			JOIN feed_impressions fi ON fi.item_id = ps.id AND fi.item_kind = 'reshare'
			WHERE fi.viewed_at >= NOW() - INTERVAL '30 days'
			GROUP BY ps.user_id
		) share_impressions ON share_impressions.user_id = u.id
		LEFT JOIN (
			SELECT p.user_id, COUNT(*)::int AS cnt
			FROM posts p
			JOIN post_reactions pr ON pr.post_id = p.id AND pr.type = 'like'
			GROUP BY p.user_id
		) post_likes ON post_likes.user_id = u.id
		LEFT JOIN (
			SELECT ps.user_id, COUNT(*)::int AS cnt
			FROM post_shares ps
			JOIN share_reactions sr ON sr.share_id = ps.id AND sr.type = 'like'
			GROUP BY ps.user_id
		) share_likes ON share_likes.user_id = u.id
		LEFT JOIN (
			SELECT p.user_id, COUNT(*)::int AS cnt
			FROM posts p
			JOIN comments c ON c.post_id = p.id
			GROUP BY p.user_id
		) post_comments ON post_comments.user_id = u.id
		LEFT JOIN (
			SELECT ps.user_id, COUNT(*)::int AS cnt
			FROM post_shares ps
			JOIN share_comments sc ON sc.share_id = ps.id
			GROUP BY ps.user_id
		) share_comments ON share_comments.user_id = u.id
		LEFT JOIN (
			SELECT p.user_id, COUNT(*)::int AS cnt
			FROM posts p
			JOIN feed_hidden_posts fh ON fh.item_id = p.id AND fh.item_kind = 'post'
			GROUP BY p.user_id
		) post_hides ON post_hides.user_id = u.id
		LEFT JOIN (
			SELECT ps.user_id, COUNT(*)::int AS cnt
			FROM post_shares ps
			JOIN feed_hidden_posts fh ON fh.item_id = ps.id AND fh.item_kind = 'reshare'
			GROUP BY ps.user_id
		) share_hides ON share_hides.user_id = u.id
		WHERE u.id = ANY($1::uuid[])
		ON CONFLICT (author_id) DO UPDATE SET
			recent_post_count = EXCLUDED.recent_post_count,
			recent_share_count = EXCLUDED.recent_share_count,
			rolling_impression_count = EXCLUDED.rolling_impression_count,
			rolling_like_count = EXCLUDED.rolling_like_count,
			rolling_comment_count = EXCLUDED.rolling_comment_count,
			rolling_hide_count = EXCLUDED.rolling_hide_count,
			last_post_at = EXCLUDED.last_post_at,
			last_share_at = EXCLUDED.last_share_at,
			updated_at = EXCLUDED.updated_at
	`, authorIDs, time.Now().UTC())
	return err
}
