package feed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type pgStore struct {
	pool *pgxpool.Pool
}

// NewPgStore wraps a pgxpool.Pool as the production Querier implementation.
func NewPgStore(pool *pgxpool.Pool) Querier {
	return &pgStore{pool: pool}
}

func (s *pgStore) ListFeed(ctx context.Context, before *time.Time, limit int) ([]Post, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			p.id,
			p.user_id,
			u.username,
			u.avatar_url,
			p.body,
			p.created_at,
			COALESCE(cc.cnt, 0) AS comment_count,
			COALESCE(lc.cnt, 0) AS like_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt FROM comments WHERE post_id = p.id
		) cc ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt FROM post_reactions WHERE post_id = p.id AND type = 'like'
		) lc ON true
		WHERE ($1::timestamptz IS NULL OR p.created_at < $1)
		ORDER BY p.created_at DESC
		LIMIT $2`,
		before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPosts(rows)
}

func (s *pgStore) ListUserPosts(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Post, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			p.id,
			p.user_id,
			u.username,
			u.avatar_url,
			p.body,
			p.created_at,
			COALESCE(cc.cnt, 0) AS comment_count,
			COALESCE(lc.cnt, 0) AS like_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt FROM comments WHERE post_id = p.id
		) cc ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt FROM post_reactions WHERE post_id = p.id AND type = 'like'
		) lc ON true
		WHERE p.user_id = $1
			AND ($2::timestamptz IS NULL OR p.created_at < $2)
		ORDER BY p.created_at DESC
		LIMIT $3`,
		userID, before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPosts(rows)
}

func (s *pgStore) CreatePost(ctx context.Context, userID uuid.UUID, body string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO posts (user_id, body) VALUES ($1, $2) RETURNING id`,
		userID, body,
	).Scan(&id)
	return id, err
}

func (s *pgStore) DeletePost(ctx context.Context, postID, userID uuid.UUID) error {
	result, err := s.pool.Exec(ctx, "DELETE FROM posts WHERE id = $1 AND user_id = $2", postID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgStore) ListReactions(ctx context.Context, postID uuid.UUID, limit, offset int) ([]Reaction, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			pr.id,
			pr.user_id,
			u.username,
			u.avatar_url,
			pr.type
		FROM post_reactions pr
		JOIN users u ON u.id = pr.user_id
		WHERE pr.post_id = $1
		ORDER BY pr.type ASC, pr.id ASC
		LIMIT $2 OFFSET $3`,
		postID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reactions []Reaction
	for rows.Next() {
		var re Reaction
		if err := rows.Scan(&re.ID, &re.UserID, &re.Username, &re.AvatarURL, &re.Type); err != nil {
			return nil, err
		}
		reactions = append(reactions, re)
	}
	return reactions, rows.Err()
}

func (s *pgStore) ToggleReaction(ctx context.Context, postID, userID uuid.UUID, reactionType string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM post_reactions
			WHERE post_id = $1 AND user_id = $2 AND type = $3
		)`,
		postID, userID, reactionType,
	).Scan(&exists); err != nil {
		return false, err
	}

	if exists {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM post_reactions WHERE post_id = $1 AND user_id = $2 AND type = $3`,
			postID, userID, reactionType,
		); err != nil {
			return false, err
		}
		return false, nil
	}

	if _, err := s.pool.Exec(ctx,
		`INSERT INTO post_reactions (post_id, user_id, type) VALUES ($1, $2, $3)`,
		postID, userID, reactionType,
	); err != nil {
		return false, err
	}
	return true, nil
}

func (s *pgStore) ResolveMentionUsers(ctx context.Context, userIDs []uuid.UUID) ([]MentionedUser, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, username
		FROM users
		WHERE id = ANY($1::uuid[])`,
		userIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]MentionedUser, 0, len(userIDs))
	for rows.Next() {
		var user MentionedUser
		if err := rows.Scan(&user.UserID, &user.Username); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *pgStore) AddComment(ctx context.Context, postID, userID uuid.UUID, body string, mentions []CommentMention) (*Comment, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var comment Comment
	if err := tx.QueryRow(ctx,
		`INSERT INTO comments (post_id, user_id, body)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`,
		postID, userID, body,
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
			pgx.Identifier{"comment_mentions"},
			[]string{"comment_id", "user_id"},
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
	return &comment, nil
}

func (s *pgStore) ListComments(ctx context.Context, postID uuid.UUID, after *time.Time, limit int) ([]Comment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT
			c.id,
			c.user_id,
			u.username,
			u.avatar_url,
			c.body,
			c.created_at
		FROM comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.post_id = $1
			AND ($2::timestamptz IS NULL OR c.created_at > $2)
		ORDER BY c.created_at ASC
		LIMIT $3`,
		postID, after, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.UserID, &c.Username, &c.AvatarURL, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachCommentMentions(ctx, comments); err != nil {
		return nil, err
	}
	return comments, nil
}

func scanPosts(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Post, error) {
	var posts []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.UserID, &p.Username, &p.AvatarURL, &p.Body, &p.CreatedAt, &p.CommentCount, &p.LikeCount); err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

func (s *pgStore) attachCommentMentions(ctx context.Context, comments []Comment) error {
	if len(comments) == 0 {
		return nil
	}

	commentIDs := make([]uuid.UUID, 0, len(comments))
	commentByID := make(map[uuid.UUID]*Comment, len(comments))
	for i := range comments {
		commentIDs = append(commentIDs, comments[i].ID)
		commentByID[comments[i].ID] = &comments[i]
	}

	rows, err := s.pool.Query(ctx,
		`SELECT cm.comment_id, u.id, u.username
		FROM comment_mentions cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.comment_id = ANY($1::uuid[])
		ORDER BY cm.comment_id ASC, u.username ASC`,
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
			return fmt.Errorf("comment mention references unknown comment %s", commentID)
		}
		comment.Mentions = append(comment.Mentions, mention)
	}
	return rows.Err()
}
