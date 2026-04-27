package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *pgStore) ToggleFeedItemReaction(ctx context.Context, itemID, userID uuid.UUID, itemKind FeedItemKind, reactionType string) (bool, error) {
	switch itemKind {
	case FeedItemKindPost:
		return s.ToggleReaction(ctx, itemID, userID, reactionType)
	case FeedItemKindReshare:
		return s.toggleShareReaction(ctx, itemID, userID, reactionType)
	default:
		return false, ErrInvalidFeedItemKind
	}
}

func (s *pgStore) AddFeedItemComment(ctx context.Context, itemID, userID uuid.UUID, itemKind FeedItemKind, body string, mentions []CommentMention) (*Comment, error) {
	switch itemKind {
	case FeedItemKindPost:
		return s.AddComment(ctx, itemID, userID, body, mentions)
	case FeedItemKindReshare:
		return s.addShareComment(ctx, itemID, userID, body, mentions)
	default:
		return nil, ErrInvalidFeedItemKind
	}
}

func (s *pgStore) ListFeedItemComments(ctx context.Context, itemID uuid.UUID, itemKind FeedItemKind, after *time.Time, limit int) ([]Comment, error) {
	switch itemKind {
	case FeedItemKindPost:
		return s.ListComments(ctx, itemID, after, limit)
	case FeedItemKindReshare:
		return s.listShareComments(ctx, itemID, after, limit)
	default:
		return nil, ErrInvalidFeedItemKind
	}
}

func (s *pgStore) toggleShareReaction(ctx context.Context, shareID, userID uuid.UUID, reactionType string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM share_reactions
			WHERE share_id = $1 AND user_id = $2 AND type = $3
		)`,
		shareID, userID, reactionType,
	).Scan(&exists); err != nil {
		return false, err
	}

	if exists {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM share_reactions WHERE share_id = $1 AND user_id = $2 AND type = $3`,
			shareID, userID, reactionType,
		); err != nil {
			return false, err
		}
		if err := s.refreshFeedAggregatesForShares(ctx, []uuid.UUID{shareID}); err != nil {
			return false, err
		}
		return false, nil
	}

	if _, err := s.pool.Exec(ctx,
		`INSERT INTO share_reactions (share_id, user_id, type) VALUES ($1, $2, $3)`,
		shareID, userID, reactionType,
	); err != nil {
		return false, err
	}
	if err := s.refreshFeedAggregatesForShares(ctx, []uuid.UUID{shareID}); err != nil {
		return false, err
	}
	return true, nil
}

func (s *pgStore) addShareComment(ctx context.Context, shareID, userID uuid.UUID, body string, mentions []CommentMention) (*Comment, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var comment Comment
	if err := tx.QueryRow(ctx,
		`INSERT INTO share_comments (share_id, user_id, body)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`,
		shareID, userID, body,
	).Scan(&comment.ID, &comment.CreatedAt); err != nil {
		return nil, err
	}

	if len(mentions) > 0 {
		rows := make([][]any, 0, len(mentions))
		for _, mention := range mentions {
			rows = append(rows, []any{comment.ID, mention.UserID})
		}
		if _, err := tx.CopyFrom(
			ctx,
			pgx.Identifier{"share_comment_mentions"},
			[]string{"share_comment_id", "user_id"},
			pgx.CopyFromRows(rows),
		); err != nil {
			return nil, err
		}
	}

	if err := tx.QueryRow(ctx,
		`SELECT u.username, u.avatar_url
		FROM users u
		WHERE u.id = $1`,
		userID,
	).Scan(&comment.Username, &comment.AvatarURL); err != nil {
		return nil, err
	}
	comment.UserID = userID
	comment.Body = body
	comment.Mentions = mentions

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if err := s.refreshFeedAggregatesForShares(ctx, []uuid.UUID{shareID}); err != nil {
		return nil, err
	}
	return &comment, nil
}

func (s *pgStore) listShareComments(ctx context.Context, shareID uuid.UUID, after *time.Time, limit int) ([]Comment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			c.id,
			c.user_id,
			u.username,
			u.avatar_url,
			c.body,
			c.created_at
		FROM share_comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.share_id = $1
			AND ($2::timestamptz IS NULL OR c.created_at > $2)
		ORDER BY c.created_at ASC
		LIMIT $3`,
		shareID, after, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comments := make([]Comment, 0, limit)
	for rows.Next() {
		var comment Comment
		if err := rows.Scan(&comment.ID, &comment.UserID, &comment.Username, &comment.AvatarURL, &comment.Body, &comment.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachShareCommentMentions(ctx, comments); err != nil {
		return nil, err
	}
	return comments, nil
}

func (s *pgStore) attachShareCommentMentions(ctx context.Context, comments []Comment) error {
	if len(comments) == 0 {
		return nil
	}

	commentIDs := make([]uuid.UUID, 0, len(comments))
	commentByID := make(map[uuid.UUID]*Comment, len(comments))
	for index := range comments {
		commentIDs = append(commentIDs, comments[index].ID)
		commentByID[comments[index].ID] = &comments[index]
	}

	rows, err := s.pool.Query(ctx,
		`SELECT scm.share_comment_id, u.id, u.username
		FROM share_comment_mentions scm
		JOIN users u ON u.id = scm.user_id
		WHERE scm.share_comment_id = ANY($1::uuid[])
		ORDER BY scm.share_comment_id ASC, u.username ASC`,
		commentIDs,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var commentID uuid.UUID
		var mention CommentMention
		if err := rows.Scan(&commentID, &mention.UserID, &mention.Username); err != nil {
			return err
		}
		comment, ok := commentByID[commentID]
		if !ok {
			return fmt.Errorf("share comment mention references unknown comment %s", commentID)
		}
		comment.Mentions = append(comment.Mentions, mention)
	}
	return rows.Err()
}
