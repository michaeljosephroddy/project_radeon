package feed

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

type feedPostRow struct {
	Post
	ShareCount             int
	IsFriend               bool
	IsLiked                bool
	IsReshared             bool
	QualityScore           float64
	RecentHideCount        int
	RecentImpressionCount  int
	AuthorRecentPostCount  int
	AuthorRecentShareCount int
	AuthorRollingHideCount int
}

type feedReshareRow struct {
	ShareID                uuid.UUID
	ShareUserID            uuid.UUID
	ShareUsername          string
	ShareAvatarURL         *string
	Commentary             string
	ShareCreatedAt         time.Time
	OriginalPostID         uuid.UUID
	OriginalAuthorID       uuid.UUID
	OriginalUsername       string
	OriginalAvatarURL      *string
	OriginalBody           string
	OriginalCreatedAt      time.Time
	OriginalCommentCnt     int
	OriginalLikeCnt        int
	OriginalShareCnt       int
	IsFriend               bool
	IsLiked                bool
	ShareCommentCnt        int
	ShareLikeCnt           int
	QualityScore           float64
	RecentHideCount        int
	RecentImpressionCount  int
	AuthorRecentPostCount  int
	AuthorRecentShareCount int
	AuthorRollingHideCount int
}

func (s *pgStore) listSocialFeed(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int) ([]FeedItem, error) {
	friendIDs, err := s.friendIDSet(ctx, viewerID)
	if err != nil {
		return nil, err
	}

	postRows, err := s.listFeedPostRows(ctx, viewerID, before, limit*2, true)
	if err != nil {
		return nil, err
	}
	reshareRows := []feedReshareRow{}
	if isFeedReshareEnabled() {
		reshareRows, err = s.listFeedReshareRows(ctx, viewerID, before, limit*2, true)
		if err != nil {
			return nil, err
		}
	}

	items, err := s.hydrateFeedItems(ctx, viewerID, postRows, reshareRows, friendIDs)
	if err != nil {
		return nil, err
	}

	sortFeedItems(items)
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *pgStore) ListHomeFeed(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int) ([]FeedItem, error) {
	// Keep a socially grounded fallback so feed reads still work if ranked serving is turned off.
	if !isHomeFeedRankingEnabled() {
		return s.listSocialFeed(ctx, viewerID, before, limit)
	}
	return s.listRankedHomeFeed(ctx, viewerID, before, limit)
}

func (s *pgStore) listRankedHomeFeed(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int) ([]FeedItem, error) {
	if !isHomeFeedRankingEnabled() {
		return s.listSocialFeed(ctx, viewerID, before, limit)
	}

	friendIDs, err := s.friendIDSet(ctx, viewerID)
	if err != nil {
		return nil, err
	}

	poolLimit := limit * 6
	if poolLimit < 90 {
		poolLimit = 90
	}
	if poolLimit > 240 {
		poolLimit = 240
	}

	postRows, err := s.listFeedPostRows(ctx, viewerID, before, poolLimit, false)
	if err != nil {
		return nil, err
	}
	reshareRows := []feedReshareRow{}
	if isFeedReshareEnabled() {
		reshareRows, err = s.listFeedReshareRows(ctx, viewerID, before, poolLimit, false)
		if err != nil {
			return nil, err
		}
	}

	items, err := s.hydrateFeedItems(ctx, viewerID, postRows, reshareRows, friendIDs)
	if err != nil {
		return nil, err
	}
	items = trimFeedRankingCandidatePool(items, poolLimit)

	if err := enrichForYouRankingSignals(ctx, s, viewerID, items); err != nil {
		return nil, err
	}
	rankForYouItems(items, time.Now().UTC())
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func trimFeedRankingCandidatePool(items []FeedItem, limit int) []FeedItem {
	if limit < 1 || len(items) <= limit {
		return items
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID.String() < items[j].ID.String()
	})
	return items[:limit]
}

func (s *pgStore) friendIDSet(ctx context.Context, viewerID uuid.UUID) (map[uuid.UUID]struct{}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT CASE
			WHEN user_a_id = $1 THEN user_b_id
			ELSE user_a_id
		END AS friend_id
		FROM friendships
		WHERE (user_a_id = $1 OR user_b_id = $1)
			AND status = 'accepted'
	`, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	friendIDs := map[uuid.UUID]struct{}{viewerID: {}}
	for rows.Next() {
		var friendID uuid.UUID
		if err := rows.Scan(&friendID); err != nil {
			return nil, err
		}
		friendIDs[friendID] = struct{}{}
	}
	return friendIDs, rows.Err()
}

func (s *pgStore) listFeedPostRows(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int, friendsOnly bool) ([]feedPostRow, error) {
	rows, err := s.pool.Query(ctx, `
		WITH friend_ids AS (
			SELECT CASE
				WHEN f.user_a_id = $1 THEN f.user_b_id
				ELSE f.user_a_id
			END AS friend_id
			FROM friendships f
			WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
				AND f.status = 'accepted'
		)
		SELECT
			p.id,
			p.user_id,
			u.username,
			u.avatar_url,
			COALESCE(p.body, ''),
			p.source_type,
			p.source_id,
			p.source_label,
			p.created_at,
			COALESCE(pqf.total_comment_count, 0) AS comment_count,
			COALESCE(pqf.total_like_count, 0) AS like_count,
			COALESCE(pqf.total_share_count, 0) AS share_count,
			EXISTS(SELECT 1 FROM friend_ids fi WHERE fi.friend_id = p.user_id) AS is_friend,
			EXISTS(
				SELECT 1 FROM post_reactions pr
				WHERE pr.post_id = p.id AND pr.user_id = $1 AND pr.type = 'like'
			) AS is_liked,
			EXISTS(
				SELECT 1 FROM post_shares psv
				WHERE psv.post_id = p.id AND psv.user_id = $1
			) AS is_reshared,
			COALESCE(pqf.quality_score, 0) AS quality_score,
			COALESCE(pqf.recent_hide_count, 0) AS recent_hide_count,
			COALESCE(pqf.recent_impression_count, 0) AS recent_impression_count,
			COALESCE(afs.recent_post_count, 0) AS author_recent_post_count,
			COALESCE(afs.recent_share_count, 0) AS author_recent_share_count,
			COALESCE(afs.rolling_hide_count, 0) AS author_rolling_hide_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN post_quality_features pqf ON pqf.post_id = p.id
		LEFT JOIN author_feed_stats afs ON afs.author_id = p.user_id
		WHERE ($2::timestamptz IS NULL OR p.created_at < $2)
			AND (
				NOT $4
				OR p.user_id = $1
				OR EXISTS(SELECT 1 FROM friend_ids fi WHERE fi.friend_id = p.user_id)
			)
			AND NOT EXISTS (
				SELECT 1 FROM feed_hidden_posts fh
				WHERE fh.user_id = $1 AND fh.item_id = p.id AND fh.item_kind = 'post'
			)
			AND NOT EXISTS (
				SELECT 1 FROM feed_muted_authors fma
				WHERE fma.user_id = $1 AND fma.author_id = p.user_id
			)
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $3
	`, viewerID, before, limit, friendsOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]feedPostRow, 0, limit)
	for rows.Next() {
		var row feedPostRow
		if err := rows.Scan(
			&row.ID,
			&row.UserID,
			&row.Username,
			&row.AvatarURL,
			&row.Body,
			&row.SourceType,
			&row.SourceID,
			&row.SourceLabel,
			&row.CreatedAt,
			&row.CommentCount,
			&row.LikeCount,
			&row.ShareCount,
			&row.IsFriend,
			&row.IsLiked,
			&row.IsReshared,
			&row.QualityScore,
			&row.RecentHideCount,
			&row.RecentImpressionCount,
			&row.AuthorRecentPostCount,
			&row.AuthorRecentShareCount,
			&row.AuthorRollingHideCount,
		); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachFeedPostImages(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *pgStore) listFeedReshareRows(ctx context.Context, viewerID uuid.UUID, before *time.Time, limit int, friendsOnly bool) ([]feedReshareRow, error) {
	rows, err := s.pool.Query(ctx, `
		WITH friend_ids AS (
			SELECT CASE
				WHEN f.user_a_id = $1 THEN f.user_b_id
				ELSE f.user_a_id
			END AS friend_id
			FROM friendships f
			WHERE (f.user_a_id = $1 OR f.user_b_id = $1)
				AND f.status = 'accepted'
		)
		SELECT
			ps.id,
			ps.user_id,
			su.username,
			su.avatar_url,
			COALESCE(ps.commentary, ''),
			ps.created_at,
			p.id,
			p.user_id,
			ou.username,
			ou.avatar_url,
			COALESCE(p.body, ''),
			p.created_at,
			COALESCE(opqf.total_comment_count, 0) AS original_comment_count,
			COALESCE(opqf.total_like_count, 0) AS original_like_count,
			COALESCE(opqf.total_share_count, 0) AS original_share_count,
			EXISTS(SELECT 1 FROM friend_ids fi WHERE fi.friend_id = ps.user_id) AS is_friend,
			EXISTS(
				SELECT 1 FROM share_reactions sr
				WHERE sr.share_id = ps.id AND sr.user_id = $1 AND sr.type = 'like'
			) AS is_liked,
			COALESCE(sqf.total_comment_count, 0) AS share_comment_count,
			COALESCE(sqf.total_like_count, 0) AS share_like_count,
			COALESCE(sqf.quality_score, 0) AS quality_score,
			COALESCE(sqf.recent_hide_count, 0) AS recent_hide_count,
			COALESCE(sqf.recent_impression_count, 0) AS recent_impression_count,
			COALESCE(afs.recent_post_count, 0) AS author_recent_post_count,
			COALESCE(afs.recent_share_count, 0) AS author_recent_share_count,
			COALESCE(afs.rolling_hide_count, 0) AS author_rolling_hide_count
		FROM post_shares ps
		JOIN posts p ON p.id = ps.post_id
		JOIN users su ON su.id = ps.user_id
		JOIN users ou ON ou.id = p.user_id
		LEFT JOIN share_quality_features sqf ON sqf.share_id = ps.id
		LEFT JOIN post_quality_features opqf ON opqf.post_id = p.id
		LEFT JOIN author_feed_stats afs ON afs.author_id = ps.user_id
		WHERE ($2::timestamptz IS NULL OR ps.created_at < $2)
			AND (
				NOT $4
				OR ps.user_id = $1
				OR EXISTS(SELECT 1 FROM friend_ids fi WHERE fi.friend_id = ps.user_id)
			)
			AND NOT EXISTS (
				SELECT 1 FROM feed_hidden_posts fh
				WHERE fh.user_id = $1 AND fh.item_id = ps.id AND fh.item_kind = 'reshare'
			)
			AND NOT EXISTS (
				SELECT 1 FROM feed_muted_authors fma
				WHERE fma.user_id = $1 AND (fma.author_id = ps.user_id OR fma.author_id = p.user_id)
			)
		ORDER BY ps.created_at DESC, ps.id DESC
		LIMIT $3
	`, viewerID, before, limit, friendsOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]feedReshareRow, 0, limit)
	for rows.Next() {
		var row feedReshareRow
		if err := rows.Scan(
			&row.ShareID,
			&row.ShareUserID,
			&row.ShareUsername,
			&row.ShareAvatarURL,
			&row.Commentary,
			&row.ShareCreatedAt,
			&row.OriginalPostID,
			&row.OriginalAuthorID,
			&row.OriginalUsername,
			&row.OriginalAvatarURL,
			&row.OriginalBody,
			&row.OriginalCreatedAt,
			&row.OriginalCommentCnt,
			&row.OriginalLikeCnt,
			&row.OriginalShareCnt,
			&row.IsFriend,
			&row.IsLiked,
			&row.ShareCommentCnt,
			&row.ShareLikeCnt,
			&row.QualityScore,
			&row.RecentHideCount,
			&row.RecentImpressionCount,
			&row.AuthorRecentPostCount,
			&row.AuthorRecentShareCount,
			&row.AuthorRollingHideCount,
		); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func (s *pgStore) hydrateFeedItems(ctx context.Context, viewerID uuid.UUID, posts []feedPostRow, reshares []feedReshareRow, friendIDs map[uuid.UUID]struct{}) ([]FeedItem, error) {
	postIDs := collectPostIDs(posts, reshares)
	postImages, err := s.loadPostImagesByID(ctx, postIDs)
	if err != nil {
		return nil, err
	}
	postTags, err := s.loadPostTagsByID(ctx, postIDs)
	if err != nil {
		return nil, err
	}

	items := make([]FeedItem, 0, len(posts)+len(reshares))
	for _, post := range posts {
		_, isFriend := friendIDs[post.UserID]
		item := FeedItem{
			ID:          post.ID,
			Kind:        FeedItemKindPost,
			ServedAtKey: post.CreatedAt,
			Author: FeedActor{
				UserID:    post.UserID,
				Username:  post.Username,
				AvatarURL: post.AvatarURL,
			},
			Body:         post.Body,
			SourceType:   post.SourceType,
			SourceID:     post.SourceID,
			SourceLabel:  post.SourceLabel,
			Images:       postImages[post.ID],
			Tags:         tagsForPost(postTags, post.ID),
			CreatedAt:    post.CreatedAt,
			LikeCount:    post.LikeCount,
			CommentCount: post.CommentCount,
			ShareCount:   post.ShareCount,
			ViewerState: ViewerFeedState{
				IsFriend:   isFriend && post.UserID != viewerID,
				IsLiked:    post.IsLiked,
				IsReshared: post.IsReshared,
				IsOwnPost:  post.UserID == viewerID,
			},
		}
		item.rankingSignals = feedRankingSignals{
			QualityScore:           post.QualityScore,
			RecentHideCount:        post.RecentHideCount,
			RecentImpressionCount:  post.RecentImpressionCount,
			AuthorRecentPostCount:  post.AuthorRecentPostCount,
			AuthorRecentShareCount: post.AuthorRecentShareCount,
			AuthorRollingHideCount: post.AuthorRollingHideCount,
		}
		items = append(items, item)
	}

	for _, reshare := range reshares {
		_, isFriend := friendIDs[reshare.ShareUserID]
		item := FeedItem{
			ID:          reshare.ShareID,
			Kind:        FeedItemKindReshare,
			ServedAtKey: reshare.ShareCreatedAt,
			Author: FeedActor{
				UserID:    reshare.ShareUserID,
				Username:  reshare.ShareUsername,
				AvatarURL: reshare.ShareAvatarURL,
			},
			Body:         reshare.Commentary,
			CreatedAt:    reshare.ShareCreatedAt,
			LikeCount:    reshare.ShareLikeCnt,
			CommentCount: reshare.ShareCommentCnt,
			ViewerState: ViewerFeedState{
				IsFriend:   isFriend && reshare.ShareUserID != viewerID,
				IsLiked:    reshare.IsLiked,
				IsOwnShare: reshare.ShareUserID == viewerID,
			},
			OriginalPost: &EmbeddedPost{
				PostID: reshare.OriginalPostID,
				Author: FeedActor{
					UserID:    reshare.OriginalAuthorID,
					Username:  reshare.OriginalUsername,
					AvatarURL: reshare.OriginalAvatarURL,
				},
				Body:         reshare.OriginalBody,
				Images:       postImages[reshare.OriginalPostID],
				Tags:         tagsForPost(postTags, reshare.OriginalPostID),
				CreatedAt:    reshare.OriginalCreatedAt,
				LikeCount:    reshare.OriginalLikeCnt,
				CommentCount: reshare.OriginalCommentCnt,
				ShareCount:   reshare.OriginalShareCnt,
			},
			ReshareMetadata: &ReshareMetadata{
				ShareID:      reshare.ShareID,
				OriginalPost: reshare.OriginalPostID,
				Commentary:   reshare.Commentary,
				CreatedAt:    reshare.ShareCreatedAt,
			},
		}
		item.rankingSignals = feedRankingSignals{
			QualityScore:           reshare.QualityScore,
			RecentHideCount:        reshare.RecentHideCount,
			RecentImpressionCount:  reshare.RecentImpressionCount,
			AuthorRecentPostCount:  reshare.AuthorRecentPostCount,
			AuthorRecentShareCount: reshare.AuthorRecentShareCount,
			AuthorRollingHideCount: reshare.AuthorRollingHideCount,
		}
		items = append(items, item)
	}

	return items, nil
}

func collectPostIDs(posts []feedPostRow, reshares []feedReshareRow) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(posts)+len(reshares))
	ids := make([]uuid.UUID, 0, len(posts)+len(reshares))
	for _, post := range posts {
		if _, ok := seen[post.ID]; ok {
			continue
		}
		seen[post.ID] = struct{}{}
		ids = append(ids, post.ID)
	}
	for _, reshare := range reshares {
		if _, ok := seen[reshare.OriginalPostID]; ok {
			continue
		}
		seen[reshare.OriginalPostID] = struct{}{}
		ids = append(ids, reshare.OriginalPostID)
	}
	return ids
}

func (s *pgStore) loadPostImagesByID(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID][]PostImage, error) {
	imagesByID := make(map[uuid.UUID][]PostImage, len(postIDs))
	if len(postIDs) == 0 {
		return imagesByID, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, post_id, image_url, width, height, sort_order
		FROM post_images
		WHERE post_id = ANY($1::uuid[])
		ORDER BY post_id ASC, sort_order ASC, created_at ASC`,
		postIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var image PostImage
		var postID uuid.UUID
		if err := rows.Scan(&image.ID, &postID, &image.ImageURL, &image.Width, &image.Height, &image.SortOrder); err != nil {
			return nil, err
		}
		imagesByID[postID] = append(imagesByID[postID], image)
	}
	return imagesByID, rows.Err()
}

func tagsForPost(tagsByID map[uuid.UUID][]string, postID uuid.UUID) []string {
	tags := tagsByID[postID]
	if tags == nil {
		return []string{}
	}
	return tags
}

func (s *pgStore) attachFeedPostImages(ctx context.Context, posts []feedPostRow) error {
	postIDs := make([]uuid.UUID, 0, len(posts))
	for _, post := range posts {
		postIDs = append(postIDs, post.ID)
	}
	imagesByID, err := s.loadPostImagesByID(ctx, postIDs)
	if err != nil {
		return err
	}
	for index := range posts {
		posts[index].Images = imagesByID[posts[index].ID]
	}
	return nil
}

func rankForYouItems(items []FeedItem, now time.Time) {
	for index := range items {
		score := 100.0
		ageHours := now.Sub(items[index].CreatedAt).Hours()
		if ageHours > 0 {
			score -= math.Min(ageHours, 240) * 0.35
		}
		score += math.Min(float64(items[index].LikeCount), 40) * 0.85
		score += math.Min(float64(items[index].CommentCount), 20) * 1.4
		score += math.Min(float64(items[index].ShareCount), 20) * 1.2
		score += items[index].rankingSignals.QualityScore * 0.8
		score += items[index].rankingSignals.AffinityScore * 5
		score += float64(items[index].rankingSignals.SharedInterestCount) * 4
		score += math.Min(float64(items[index].rankingSignals.RecentImpressionCount), 50) * 0.12
		if items[index].ViewerState.IsFriend {
			score += 24
		}
		if items[index].Kind == FeedItemKindReshare {
			score += 7
			if items[index].OriginalPost != nil && items[index].OriginalPost.Author.UserID != items[index].Author.UserID {
				score += 3
			}
		}
		if len(items[index].Images) > 0 {
			score += 4
		}
		if items[index].OriginalPost != nil && len(items[index].OriginalPost.Images) > 0 {
			score += 3
		}
		if items[index].ViewerState.IsOwnPost || items[index].ViewerState.IsOwnShare {
			score += 6
		}
		score -= float64(items[index].rankingSignals.RecentHideCount) * 9
		score -= float64(items[index].rankingSignals.AuthorRollingHideCount) * 0.7
		score -= float64(items[index].rankingSignals.AuthorRecentPostCount) * 1.1
		score -= float64(items[index].rankingSignals.AuthorRecentShareCount) * 0.8
		items[index].Score = score
	}

	sort.SliceStable(items, func(i, j int) bool {
		if math.Abs(items[i].Score-items[j].Score) > 0.001 {
			return items[i].Score > items[j].Score
		}
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID.String() < items[j].ID.String()
	})

	// Re-rank greedily so one author or a chain of reshares cannot dominate the page.
	authorSeen := make(map[string]int, len(items))
	reshareRun := 0
	ranked := make([]FeedItem, 0, len(items))
	remaining := append([]FeedItem(nil), items...)
	for len(remaining) > 0 {
		bestIndex := 0
		bestScore := adjustedFeedScore(remaining[0], authorSeen, reshareRun)
		for idx := 1; idx < len(remaining); idx++ {
			adjusted := adjustedFeedScore(remaining[idx], authorSeen, reshareRun)
			if adjusted > bestScore+0.001 {
				bestIndex = idx
				bestScore = adjusted
			}
		}
		chosen := remaining[bestIndex]
		ranked = append(ranked, chosen)
		authorSeen[chosen.Author.UserID.String()]++
		if chosen.Kind == FeedItemKindReshare {
			reshareRun++
		} else {
			reshareRun = 0
		}
		remaining = append(remaining[:bestIndex], remaining[bestIndex+1:]...)
	}
	copy(items, ranked)
}

func adjustedFeedScore(item FeedItem, authorSeen map[string]int, reshareRun int) float64 {
	score := item.Score
	if count := authorSeen[item.Author.UserID.String()]; count > 0 {
		score -= float64(count) * 9
	}
	if item.Kind == FeedItemKindReshare && reshareRun > 0 {
		score -= float64(reshareRun) * 8
	}
	return score
}

func enrichForYouRankingSignals(ctx context.Context, store *pgStore, viewerID uuid.UUID, items []FeedItem) error {
	if len(items) == 0 {
		return nil
	}

	authorIDs := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		authorIDs = append(authorIDs, item.Author.UserID)
	}
	authorIDs = dedupeUUIDs(authorIDs)
	if len(authorIDs) == 0 {
		return nil
	}

	sharedInterestCounts := make(map[uuid.UUID]int, len(authorIDs))
	interestRows, err := store.pool.Query(ctx, `
		WITH viewer_interests AS (
			SELECT interest_id
			FROM user_interests
			WHERE user_id = $1
		)
		SELECT ui.user_id, COUNT(*)::int AS shared_interest_count
		FROM user_interests ui
		JOIN viewer_interests vi ON vi.interest_id = ui.interest_id
		WHERE ui.user_id = ANY($2::uuid[])
		GROUP BY ui.user_id
	`, viewerID, authorIDs)
	if err != nil {
		return err
	}
	for interestRows.Next() {
		var authorID uuid.UUID
		var sharedCount int
		if err := interestRows.Scan(&authorID, &sharedCount); err != nil {
			interestRows.Close()
			return err
		}
		sharedInterestCounts[authorID] = sharedCount
	}
	if err := interestRows.Err(); err != nil {
		interestRows.Close()
		return err
	}
	interestRows.Close()

	affinityScores := make(map[uuid.UUID]float64, len(authorIDs))
	affinityRows, err := store.pool.Query(ctx, `
		WITH scores AS (
			SELECT p.user_id AS author_id, 1.25::double precision AS weight
			FROM post_reactions pr
			JOIN posts p ON p.id = pr.post_id
			WHERE pr.user_id = $1
				AND pr.type = 'like'
				AND p.user_id = ANY($2::uuid[])
			UNION ALL
			SELECT p.user_id AS author_id, 2.5::double precision AS weight
			FROM comments c
			JOIN posts p ON p.id = c.post_id
			WHERE c.user_id = $1
				AND p.user_id = ANY($2::uuid[])
			UNION ALL
			SELECT ps.user_id AS author_id, 1.25::double precision AS weight
			FROM share_reactions sr
			JOIN post_shares ps ON ps.id = sr.share_id
			WHERE sr.user_id = $1
				AND sr.type = 'like'
				AND ps.user_id = ANY($2::uuid[])
			UNION ALL
			SELECT ps.user_id AS author_id, 2.5::double precision AS weight
			FROM share_comments sc
			JOIN post_shares ps ON ps.id = sc.share_id
			WHERE sc.user_id = $1
				AND ps.user_id = ANY($2::uuid[])
			UNION ALL
			SELECT p.user_id AS author_id, 0.6::double precision AS weight
			FROM feed_impressions fi
			JOIN posts p ON p.id = fi.item_id
			WHERE fi.user_id = $1
				AND fi.item_kind = 'post'
				AND fi.was_clicked = TRUE
				AND p.user_id = ANY($2::uuid[])
			UNION ALL
			SELECT ps.user_id AS author_id, 0.8::double precision AS weight
			FROM feed_impressions fi
			JOIN post_shares ps ON ps.id = fi.item_id
			WHERE fi.user_id = $1
				AND fi.item_kind = 'reshare'
				AND fi.was_clicked = TRUE
				AND ps.user_id = ANY($2::uuid[])
		)
		SELECT author_id, COALESCE(SUM(weight), 0)::double precision AS affinity_score
		FROM scores
		GROUP BY author_id
	`, viewerID, authorIDs)
	if err != nil {
		return err
	}
	for affinityRows.Next() {
		var authorID uuid.UUID
		var affinity float64
		if err := affinityRows.Scan(&authorID, &affinity); err != nil {
			affinityRows.Close()
			return err
		}
		affinityScores[authorID] = affinity
	}
	if err := affinityRows.Err(); err != nil {
		affinityRows.Close()
		return err
	}
	affinityRows.Close()

	for index := range items {
		signals := items[index].rankingSignals
		signals.SharedInterestCount = sharedInterestCounts[items[index].Author.UserID]
		signals.AffinityScore = affinityScores[items[index].Author.UserID]
		items[index].rankingSignals = signals
	}
	return nil
}

func sortFeedItems(items []FeedItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].ID.String() < items[j].ID.String()
	})
}

func (s *pgStore) ListHiddenFeedItems(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]HiddenFeedItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			fh.item_id,
			fh.item_kind,
			fh.hidden_at,
			CASE WHEN fh.item_kind = 'post' THEN p.user_id ELSE ps.user_id END AS author_id,
			CASE WHEN fh.item_kind = 'post' THEN pu.username ELSE su.username END AS author_username,
			CASE WHEN fh.item_kind = 'post' THEN pu.avatar_url ELSE su.avatar_url END AS author_avatar_url,
			CASE WHEN fh.item_kind = 'post' THEN COALESCE(p.body, '') ELSE COALESCE(ps.commentary, '') END AS item_body,
			CASE WHEN fh.item_kind = 'post' THEN p.created_at ELSE ps.created_at END AS item_created_at,
			CASE WHEN fh.item_kind = 'post' THEN COALESCE(pqf.total_like_count, 0) ELSE COALESCE(sqf.total_like_count, 0) END AS like_count,
			CASE WHEN fh.item_kind = 'post' THEN COALESCE(pqf.total_comment_count, 0) ELSE COALESCE(sqf.total_comment_count, 0) END AS comment_count,
			CASE WHEN fh.item_kind = 'post' THEN COALESCE(pqf.total_share_count, 0) ELSE 0 END AS share_count,
			p.id AS original_post_id,
			p.user_id AS original_author_id,
			pu.username AS original_author_username,
			pu.avatar_url AS original_author_avatar_url,
			COALESCE(p.body, '') AS original_body,
			p.created_at AS original_created_at,
			COALESCE(opqf.total_like_count, COALESCE(pqf.total_like_count, 0), 0) AS original_like_count,
			COALESCE(opqf.total_comment_count, COALESCE(pqf.total_comment_count, 0), 0) AS original_comment_count,
			COALESCE(opqf.total_share_count, COALESCE(pqf.total_share_count, 0), 0) AS original_share_count
		FROM feed_hidden_posts fh
		LEFT JOIN posts p ON fh.item_kind = 'post' AND p.id = fh.item_id
		LEFT JOIN users pu ON pu.id = p.user_id
		LEFT JOIN post_quality_features pqf ON pqf.post_id = p.id
		LEFT JOIN post_shares ps ON fh.item_kind = 'reshare' AND ps.id = fh.item_id
		LEFT JOIN users su ON su.id = ps.user_id
		LEFT JOIN posts op ON op.id = ps.post_id
		LEFT JOIN users opu ON opu.id = op.user_id
		LEFT JOIN share_quality_features sqf ON sqf.share_id = ps.id
		LEFT JOIN post_quality_features opqf ON opqf.post_id = op.id
		WHERE fh.user_id = $1
			AND ($2::timestamptz IS NULL OR fh.hidden_at < $2)
			AND (
				(fh.item_kind = 'post' AND p.id IS NOT NULL)
				OR (fh.item_kind = 'reshare' AND ps.id IS NOT NULL AND op.id IS NOT NULL)
			)
		ORDER BY fh.hidden_at DESC, fh.item_id DESC
		LIMIT $3
	`, userID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hiddenItems := make([]HiddenFeedItem, 0, limit)
	for rows.Next() {
		var hidden HiddenFeedItem
		var authorID uuid.UUID
		var authorUsername string
		var authorAvatarURL *string
		var body string
		var createdAt time.Time
		var likeCount, commentCount, shareCount int
		var originalPostID *uuid.UUID
		var originalAuthorID *uuid.UUID
		var originalAuthorUsername *string
		var originalAuthorAvatarURL *string
		var originalBody *string
		var originalCreatedAt *time.Time
		var originalLikeCount, originalCommentCount, originalShareCount *int
		if err := rows.Scan(
			&hidden.ItemID,
			&hidden.ItemKind,
			&hidden.HiddenAt,
			&authorID,
			&authorUsername,
			&authorAvatarURL,
			&body,
			&createdAt,
			&likeCount,
			&commentCount,
			&shareCount,
			&originalPostID,
			&originalAuthorID,
			&originalAuthorUsername,
			&originalAuthorAvatarURL,
			&originalBody,
			&originalCreatedAt,
			&originalLikeCount,
			&originalCommentCount,
			&originalShareCount,
		); err != nil {
			return nil, err
		}

		item := FeedItem{
			ID:           hidden.ItemID,
			Kind:         hidden.ItemKind,
			Author:       FeedActor{UserID: authorID, Username: authorUsername, AvatarURL: authorAvatarURL},
			Body:         body,
			CreatedAt:    createdAt,
			ServedAtKey:  createdAt,
			LikeCount:    likeCount,
			CommentCount: commentCount,
			ShareCount:   shareCount,
		}
		if hidden.ItemKind == FeedItemKindReshare && originalPostID != nil && originalAuthorID != nil && originalAuthorUsername != nil && originalBody != nil && originalCreatedAt != nil {
			item.OriginalPost = &EmbeddedPost{
				PostID: *originalPostID,
				Author: FeedActor{
					UserID:    *originalAuthorID,
					Username:  *originalAuthorUsername,
					AvatarURL: originalAuthorAvatarURL,
				},
				Body:         *originalBody,
				CreatedAt:    *originalCreatedAt,
				LikeCount:    valueOrZero(originalLikeCount),
				CommentCount: valueOrZero(originalCommentCount),
				ShareCount:   valueOrZero(originalShareCount),
			}
			item.ReshareMetadata = &ReshareMetadata{
				ShareID:      hidden.ItemID,
				OriginalPost: *originalPostID,
				Commentary:   body,
				CreatedAt:    createdAt,
			}
		}
		hidden.Item = item
		hiddenItems = append(hiddenItems, hidden)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachHiddenFeedPostMedia(ctx, hiddenItems); err != nil {
		return nil, err
	}
	return hiddenItems, nil
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func (s *pgStore) attachHiddenFeedPostMedia(ctx context.Context, hiddenItems []HiddenFeedItem) error {
	postIDs := make([]uuid.UUID, 0, len(hiddenItems))
	seen := make(map[uuid.UUID]struct{}, len(hiddenItems))
	for _, hidden := range hiddenItems {
		if hidden.Item.Kind == FeedItemKindPost {
			if _, ok := seen[hidden.Item.ID]; !ok {
				seen[hidden.Item.ID] = struct{}{}
				postIDs = append(postIDs, hidden.Item.ID)
			}
		}
		if hidden.Item.OriginalPost != nil {
			postID := hidden.Item.OriginalPost.PostID
			if _, ok := seen[postID]; !ok {
				seen[postID] = struct{}{}
				postIDs = append(postIDs, postID)
			}
		}
	}
	imagesByID, err := s.loadPostImagesByID(ctx, postIDs)
	if err != nil {
		return err
	}
	tagsByID, err := s.loadPostTagsByID(ctx, postIDs)
	if err != nil {
		return err
	}
	for index := range hiddenItems {
		if hiddenItems[index].Item.Kind == FeedItemKindPost {
			postID := hiddenItems[index].Item.ID
			hiddenItems[index].Item.Images = imagesByID[postID]
			hiddenItems[index].Item.Tags = tagsForPost(tagsByID, postID)
		} else {
			hiddenItems[index].Item.Tags = []string{}
		}
		if hiddenItems[index].Item.OriginalPost != nil {
			postID := hiddenItems[index].Item.OriginalPost.PostID
			hiddenItems[index].Item.OriginalPost.Images = imagesByID[postID]
			hiddenItems[index].Item.OriginalPost.Tags = tagsForPost(tagsByID, postID)
		}
	}
	return nil
}
