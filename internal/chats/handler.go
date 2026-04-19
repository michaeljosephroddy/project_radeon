package chats

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project_radeon/api/pkg/middleware"
	"github.com/project_radeon/api/pkg/pagination"
	"github.com/project_radeon/api/pkg/response"
)

type Handler struct {
	db *pgxpool.Pool
}

type Chat struct {
	ID            uuid.UUID  `json:"id"`
	IsGroup       bool       `json:"is_group"`
	Name          *string    `json:"name"`
	Username      *string    `json:"username"`
	AvatarURL     *string    `json:"avatar_url"`
	CreatedAt     time.Time  `json:"created_at"`
	LastMessage   *string    `json:"last_message"`
	LastMessageAt *time.Time `json:"last_message_at"`
}

type Message struct {
	ID        uuid.UUID `json:"id"`
	SenderID  uuid.UUID `json:"sender_id"`
	Username  string    `json:"username"`
	AvatarURL *string   `json:"avatar_url"`
	Body      string    `json:"body"`
	SentAt    time.Time `json:"sent_at"`
}

type MessagePage struct {
	Items      []Message  `json:"items"`
	Limit      int        `json:"limit"`
	HasMore    bool       `json:"has_more"`
	NextBefore *time.Time `json:"next_before,omitempty"`
}

// NewHandler builds a chats handler backed by the shared database pool.
func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

// ListChats returns one page of the caller's active chats and lets the backend
// own inbox search so the client never filters an unbounded chat array.
func (h *Handler) ListChats(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	params := pagination.Parse(r, 20, 50)
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	// LATERAL joins let the query derive the "other participant" metadata for
	// direct messages and the latest message preview without extra queries.
	rows, err := h.db.Query(r.Context(),
		`SELECT
			ch.id,
			ch.is_group,
			ch.name,
			other.username,
			other.avatar_url,
			ch.created_at,
			m.body AS last_message,
			m.sent_at AS last_message_at
		FROM chats ch
		JOIN chat_members cm
			ON cm.chat_id = ch.id
			AND cm.user_id = $1
		LEFT JOIN LATERAL (
			SELECT
				u.username,
				u.avatar_url
			FROM chat_members cm2
			JOIN users u ON u.id = cm2.user_id
			WHERE cm2.chat_id = ch.id
				AND cm2.user_id != $1
			LIMIT 1
		) other ON NOT ch.is_group
		LEFT JOIN LATERAL (
			SELECT
				body,
				sent_at
			FROM messages
			WHERE chat_id = ch.id
			ORDER BY sent_at DESC
			LIMIT 1
		) m ON true
		WHERE ch.status = 'active'
			AND (
				$2 = ''
				OR (
					ch.is_group = true
					AND COALESCE(ch.name, '') ILIKE '%' || $2 || '%'
				)
				OR (
					ch.is_group = false
					AND COALESCE(other.username, '') ILIKE '%' || $2 || '%'
				)
			)
		ORDER BY COALESCE(m.sent_at, ch.created_at) DESC
		LIMIT $3 OFFSET $4`,
		userID, query, params.Limit+1, params.Offset,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch chats")
		return
	}
	defer rows.Close()

	var chats []Chat
	for rows.Next() {
		var ch Chat
		if err := rows.Scan(&ch.ID, &ch.IsGroup, &ch.Name, &ch.Username, &ch.AvatarURL, &ch.CreatedAt, &ch.LastMessage, &ch.LastMessageAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read chats")
			return
		}
		chats = append(chats, ch)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read chats")
		return
	}

	response.Success(w, http.StatusOK, pagination.Slice(chats, params))
}

// ListChatRequests returns pending direct-message requests addressed to the current user.
func (h *Handler) ListChatRequests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	// Requests are limited to chats where the caller is an addressee, which
	// prevents requesters from seeing their own pending invites in this list.
	rows, err := h.db.Query(r.Context(),
		`SELECT
			ch.id,
			ch.is_group,
			ch.name,
			other.username,
			other.avatar_url,
			ch.created_at,
			m.body AS last_message,
			m.sent_at AS last_message_at
		FROM chats ch
		JOIN chat_members cm
			ON cm.chat_id = ch.id
			AND cm.user_id = $1
			AND cm.role = 'addressee'
		LEFT JOIN LATERAL (
			SELECT
				u.username,
				u.avatar_url
			FROM chat_members cm2
			JOIN users u ON u.id = cm2.user_id
			WHERE cm2.chat_id = ch.id
				AND cm2.user_id != $1
			LIMIT 1
		) other ON NOT ch.is_group
		LEFT JOIN LATERAL (
			SELECT
				body,
				sent_at
			FROM messages
			WHERE chat_id = ch.id
			ORDER BY sent_at DESC
			LIMIT 1
		) m ON true
		WHERE ch.status = 'request'
		ORDER BY ch.created_at DESC`,
		userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch chat requests")
		return
	}
	defer rows.Close()

	var chats []Chat
	for rows.Next() {
		var ch Chat
		if err := rows.Scan(&ch.ID, &ch.IsGroup, &ch.Name, &ch.Username, &ch.AvatarURL, &ch.CreatedAt, &ch.LastMessage, &ch.LastMessageAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read chat requests")
			return
		}
		chats = append(chats, ch)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read chat requests")
		return
	}
	response.Success(w, http.StatusOK, chats)
}

// CreateChat creates a new direct or group chat unless an equivalent direct chat already exists.
func (h *Handler) CreateChat(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	var input struct {
		MemberIDs []uuid.UUID `json:"member_ids"`
		Name      *string     `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	isGroup := len(input.MemberIDs) > 1

	if !isGroup && len(input.MemberIDs) == 1 {
		// Direct chats are reused instead of duplicated so message history stays
		// attached to a single conversation between two members.
		var existingID uuid.UUID
		err := h.db.QueryRow(r.Context(),
			`SELECT ch.id
			FROM chats ch
			JOIN chat_members cm1
				ON cm1.chat_id = ch.id
				AND cm1.user_id = $1
			JOIN chat_members cm2
				ON cm2.chat_id = ch.id
				AND cm2.user_id = $2
			WHERE ch.is_group = false
			LIMIT 1`,
			userID, input.MemberIDs[0],
		).Scan(&existingID)
		if err == nil {
			response.Success(w, http.StatusOK, map[string]any{"id": existingID, "is_group": false})
			return
		}
	}

	status := "active"

	var chatID uuid.UUID
	if err := h.db.QueryRow(r.Context(),
		`INSERT INTO chats (
			is_group,
			name,
			status
		)
		VALUES ($1, $2, $3)
		RETURNING id`,
		isGroup, input.Name, status,
	).Scan(&chatID); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not create chat")
		return
	}

	if _, err := h.db.Exec(r.Context(),
		`INSERT INTO chat_members (
			chat_id,
			user_id,
			role
		)
		VALUES ($1, $2, 'requester')`,
		chatID, userID,
	); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not add members")
		return
	}
	for _, memberID := range input.MemberIDs {
		if _, err := h.db.Exec(r.Context(),
			`INSERT INTO chat_members (
				chat_id,
				user_id,
				role
			)
			VALUES ($1, $2, 'addressee')`,
			chatID, memberID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not add members")
			return
		}
	}

	response.Success(w, http.StatusCreated, map[string]any{"id": chatID, "is_group": isGroup, "status": status})
}

// UpdateChatStatus lets an addressee accept or decline a pending chat request.
func (h *Handler) UpdateChatStatus(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	chatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chat id")
		return
	}

	var input struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Status != "active" && input.Status != "declined" {
		response.Error(w, http.StatusBadRequest, "status must be 'active' or 'declined'")
		return
	}

	var isMember bool
	if err := h.db.QueryRow(r.Context(),
		`SELECT EXISTS(
			SELECT 1
			FROM chat_members
			WHERE chat_id = $1
				AND user_id = $2
				AND role = 'addressee'
		)`,
		chatID, userID,
	).Scan(&isMember); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check chat membership")
		return
	}
	if !isMember {
		response.Error(w, http.StatusForbidden, "not authorised")
		return
	}

	if input.Status == "declined" {
		// Declining removes the chat outright because pending requests have no
		// independent message history worth preserving yet.
		if _, err := h.db.Exec(r.Context(),
			`DELETE FROM chats
			WHERE id = $1`,
			chatID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not update chat")
			return
		}
	} else {
		if _, err := h.db.Exec(r.Context(),
			`UPDATE chats
			SET status = 'active'
			WHERE id = $1`,
			chatID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not update chat")
			return
		}
	}

	response.Success(w, http.StatusOK, map[string]string{"status": input.Status})
}

// GetMessages pages backwards through a chat transcript using an optional
// "before" cursor so long histories stay incremental on the client.
func (h *Handler) GetMessages(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	chatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chat id")
		return
	}

	var isMember bool
	if err := h.db.QueryRow(r.Context(),
		`SELECT EXISTS(
			SELECT 1
			FROM chat_members
			WHERE chat_id = $1
				AND user_id = $2
		)`,
		chatID, userID,
	).Scan(&isMember); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check chat membership")
		return
	}
	if !isMember {
		response.Error(w, http.StatusForbidden, "not a member of this chat")
		return
	}

	limit := 50
	if parsed := pagination.Parse(r, 50, 100); parsed.Limit > 0 {
		limit = parsed.Limit
	}

	var before *time.Time
	if beforeRaw := strings.TrimSpace(r.URL.Query().Get("before")); beforeRaw != "" {
		parsed, parseErr := time.Parse(time.RFC3339, beforeRaw)
		if parseErr != nil {
			response.Error(w, http.StatusBadRequest, "before must be an RFC3339 timestamp")
			return
		}
		before = &parsed
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT
			m.id,
			m.sender_id,
			u.username,
			u.avatar_url,
			m.body,
			m.sent_at
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.chat_id = $1
			AND ($2::timestamptz IS NULL OR m.sent_at < $2)
		ORDER BY m.sent_at DESC
		LIMIT $3`,
		chatID, before, limit+1,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch messages")
		return
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.Username, &m.AvatarURL, &m.Body, &m.SentAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not read messages")
			return
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not read messages")
		return
	}

	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}
	for left, right := 0, len(msgs)-1; left < right; left, right = left+1, right-1 {
		msgs[left], msgs[right] = msgs[right], msgs[left]
	}

	var nextBefore *time.Time
	if hasMore && len(msgs) > 0 {
		oldest := msgs[0].SentAt
		nextBefore = &oldest
	}

	response.Success(w, http.StatusOK, MessagePage{
		Items:      msgs,
		Limit:      limit,
		HasMore:    hasMore,
		NextBefore: nextBefore,
	})
}

// SendMessage appends a new text message to a chat for an authorised member.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	chatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chat id")
		return
	}

	var isMember bool
	if err := h.db.QueryRow(r.Context(),
		`SELECT EXISTS(
			SELECT 1
			FROM chat_members
			WHERE chat_id = $1
				AND user_id = $2
		)`,
		chatID, userID,
	).Scan(&isMember); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not check chat membership")
		return
	}
	if !isMember {
		response.Error(w, http.StatusForbidden, "not a member of this chat")
		return
	}

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

	// SendMessage only persists plain text content; any delivery state or unread
	// tracking would need to be layered on top of this schema.
	var msgID uuid.UUID
	if err := h.db.QueryRow(r.Context(),
		`INSERT INTO messages (
			chat_id,
			sender_id,
			body
		)
		VALUES ($1, $2, $3)
		RETURNING id`,
		chatID, userID, input.Body,
	).Scan(&msgID); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not send message")
		return
	}

	response.Success(w, http.StatusCreated, map[string]any{"id": msgID})
}

// DeleteChat deletes a direct chat or removes the caller from a group chat.
func (h *Handler) DeleteChat(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	chatID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid chat id")
		return
	}

	var isGroup bool
	var isMember bool
	if err := h.db.QueryRow(r.Context(),
		`SELECT
			ch.is_group,
			EXISTS(
				SELECT 1
				FROM chat_members cm
				WHERE cm.chat_id = ch.id
					AND cm.user_id = $2
			) AS is_member
		FROM chats ch
		WHERE ch.id = $1`,
		chatID, userID,
	).Scan(&isGroup, &isMember); err != nil {
		response.Error(w, http.StatusNotFound, "chat not found")
		return
	}
	if !isMember {
		response.Error(w, http.StatusForbidden, "not a member of this chat")
		return
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update chat")
		return
	}
	defer tx.Rollback(r.Context())

	if isGroup {
		if _, err := tx.Exec(r.Context(),
			`DELETE FROM chat_members
			WHERE chat_id = $1
				AND user_id = $2`,
			chatID, userID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not leave group")
			return
		}

		var remainingMembers int
		if err := tx.QueryRow(r.Context(),
			`SELECT COUNT(*)
			FROM chat_members
			WHERE chat_id = $1`,
			chatID,
		).Scan(&remainingMembers); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not leave group")
			return
		}

		if remainingMembers == 0 {
			if _, err := tx.Exec(r.Context(),
				`DELETE FROM messages
				WHERE chat_id = $1`,
				chatID,
			); err != nil {
				response.Error(w, http.StatusInternalServerError, "could not clean up group")
				return
			}
			if _, err := tx.Exec(r.Context(),
				`DELETE FROM chats
				WHERE id = $1`,
				chatID,
			); err != nil {
				response.Error(w, http.StatusInternalServerError, "could not clean up group")
				return
			}
		}
	} else {
		if _, err := tx.Exec(r.Context(),
			`DELETE FROM messages
			WHERE chat_id = $1`,
			chatID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not delete chat")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`DELETE FROM chat_members
			WHERE chat_id = $1`,
			chatID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not delete chat")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`DELETE FROM chats
			WHERE id = $1`,
			chatID,
		); err != nil {
			response.Error(w, http.StatusInternalServerError, "could not delete chat")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		response.Error(w, http.StatusInternalServerError, "could not update chat")
		return
	}

	action := "deleted"
	if isGroup {
		action = "left"
	}
	response.Success(w, http.StatusOK, map[string]string{"action": action})
}
