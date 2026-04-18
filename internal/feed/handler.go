package feed

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	db *pgxpool.Pool
}

// NewHandler builds a feed handler backed by the shared database pool.
func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

type Post struct {
	ID           uuid.UUID `json:"id"`
	UserID       uuid.UUID `json:"user_id"`
	Username     string    `json:"username"`
	AvatarURL    *string   `json:"avatar_url"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
	CommentCount int       `json:"comment_count"`
	LikeCount    int       `json:"like_count"`
}

// parsePagination extracts page and limit query parameters with sane defaults and bounds.
func parsePagination(r *http.Request) (limit, offset int) {
	// Pagination is intentionally forgiving: malformed values fall back to a
	// stable default instead of producing validation errors on list endpoints.
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}

	return limit, (page - 1) * limit
}

// GetFeed returns the global post feed with author metadata and aggregate counts.
func (h *Handler) GetFeed(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)

	// Counts are projected with correlated subqueries so the API can return the
	// feed in a single round trip without separate aggregate fetches.
	rows, err := h.db.Query(r.Context(),
		`SELECT
			p.id,
			p.user_id,
			u.username,
			u.avatar_url,
			p.body,
			p.created_at,
			COALESCE((
				SELECT COUNT(*)
				FROM comments c
				WHERE c.post_id = p.id
			), 0) AS comment_count,
			COALESCE((
				SELECT COUNT(*)
				FROM post_reactions pr
				WHERE pr.post_id = p.id
					AND pr.type = 'like'
			), 0) AS like_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		ORDER BY p.created_at DESC
		LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch feed")
		return
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.UserID, &p.Username, &p.AvatarURL, &p.Body, &p.CreatedAt, &p.CommentCount, &p.LikeCount); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read feed")
			return
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read feed")
		return
	}

	response.Success(w, http.StatusOK, posts)
}

// GetUserPosts returns a single user's posts with the same shape as the main feed.
func (h *Handler) GetUserPosts(w http.ResponseWriter, r *http.Request) {
	targetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}

	limit, offset := parsePagination(r)

	// This mirrors GetFeed so profile timelines and the global feed share the
	// same response shape and derived counters.
	rows, err := h.db.Query(r.Context(),
		`SELECT
			p.id,
			p.user_id,
			u.username,
			u.avatar_url,
			p.body,
			p.created_at,
			COALESCE((
				SELECT COUNT(*)
				FROM comments c
				WHERE c.post_id = p.id
			), 0) AS comment_count,
			COALESCE((
				SELECT COUNT(*)
				FROM post_reactions pr
				WHERE pr.post_id = p.id
					AND pr.type = 'like'
			), 0) AS like_count
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE p.user_id = $1
		ORDER BY p.created_at DESC
		LIMIT $2 OFFSET $3`,
		targetID, limit, offset,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch posts")
		return
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.UserID, &p.Username, &p.AvatarURL, &p.Body, &p.CreatedAt, &p.CommentCount, &p.LikeCount); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read posts")
			return
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read posts")
		return
	}

	response.Success(w, http.StatusOK, posts)
}

// CreatePost validates and inserts a new post for the authenticated user.
func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}

	input.Body = strings.TrimSpace(input.Body)
	if input.Body == "" {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}

	var postID uuid.UUID
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO posts (
			user_id,
			body
		)
		VALUES ($1, $2)
		RETURNING id`,
		userID, input.Body,
	).Scan(&postID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create post")
		return
	}

	response.Success(w, http.StatusCreated, map[string]any{"id": postID})
}

// DeletePost removes a post only when it belongs to the authenticated user.
func (h *Handler) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	result, err := h.db.Exec(r.Context(),
		"DELETE FROM posts WHERE id = $1 AND user_id = $2", postID, userID,
	)
	if err != nil || result.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "post not found or not yours")
		return
	}

	response.Success(w, http.StatusOK, map[string]bool{"deleted": true})
}

// GetReactions returns all recorded reactions for a post with reacting user details.
func (h *Handler) GetReactions(w http.ResponseWriter, r *http.Request) {
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT
			pr.id,
			pr.user_id,
			u.username,
			u.avatar_url,
			pr.type
		FROM post_reactions pr
		JOIN users u ON u.id = pr.user_id
		WHERE pr.post_id = $1
		ORDER BY pr.type ASC`,
		postID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch reactions")
		return
	}
	defer rows.Close()

	type Reaction struct {
		ID        uuid.UUID `json:"id"`
		UserID    uuid.UUID `json:"user_id"`
		Username  string    `json:"username"`
		AvatarURL *string   `json:"avatar_url"`
		Type      string    `json:"type"`
	}

	var reactions []Reaction
	for rows.Next() {
		var re Reaction
		if err := rows.Scan(&re.ID, &re.UserID, &re.Username, &re.AvatarURL, &re.Type); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read reactions")
			return
		}
		reactions = append(reactions, re)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read reactions")
		return
	}

	response.Success(w, http.StatusOK, reactions)
}

// ReactToPost toggles a specific reaction type for the authenticated user on a post.
func (h *Handler) ReactToPost(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	var input struct {
		Type string `json:"type"` // e.g. "like", "heart"
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Type == "" {
		input.Type = "like"
	}

	// Re-sending the same reaction toggles it off. Different reaction types are
	// stored as separate rows, so the check stays scoped to post/user/type.
	var exists bool
	if err := h.db.QueryRow(r.Context(),
		`SELECT EXISTS(
			SELECT 1
			FROM post_reactions
			WHERE post_id = $1
				AND user_id = $2
				AND type = $3
		)`,
		postID, userID, input.Type,
	).Scan(&exists); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check reaction")
		return
	}

	if exists {
		if _, err := h.db.Exec(r.Context(),
			`DELETE FROM post_reactions
			WHERE post_id = $1
				AND user_id = $2
				AND type = $3`,
			postID, userID, input.Type,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not remove reaction")
			return
		}
		response.Success(w, http.StatusOK, map[string]bool{"reacted": false})
	} else {
		if _, err := h.db.Exec(r.Context(),
			`INSERT INTO post_reactions (
				post_id,
				user_id,
				type
			)
			VALUES ($1, $2, $3)`,
			postID, userID, input.Type,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not add reaction")
			return
		}
		response.Success(w, http.StatusOK, map[string]bool{"reacted": true})
	}
}

// AddComment validates and inserts a new comment on the requested post.
func (h *Handler) AddComment(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	var input struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Body == "" {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}

	// Comments only return the new ID; clients are expected to refresh or append
	// locally rather than relying on the server to hydrate the full record here.
	var commentID uuid.UUID
	if err := h.db.QueryRow(r.Context(),
		`INSERT INTO comments (
			post_id,
			user_id,
			body
		)
		VALUES ($1, $2, $3)
		RETURNING id`,
		postID, userID, input.Body,
	).Scan(&commentID); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not add comment")
		return
	}

	response.Success(w, http.StatusCreated, map[string]any{"id": commentID})
}

// GetComments returns a post's comments in conversation order with author details.
func (h *Handler) GetComments(w http.ResponseWriter, r *http.Request) {
	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid post id")
		return
	}

	rows, err := h.db.Query(r.Context(),
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
		ORDER BY c.created_at ASC`,
		postID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch comments")
		return
	}
	defer rows.Close()

	type Comment struct {
		ID        uuid.UUID `json:"id"`
		UserID    uuid.UUID `json:"user_id"`
		Username  string    `json:"username"`
		AvatarURL *string   `json:"avatar_url"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
	}

	var comments []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.UserID, &c.Username, &c.AvatarURL, &c.Body, &c.CreatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read comments")
			return
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read comments")
		return
	}

	response.Success(w, http.StatusOK, comments)
}
