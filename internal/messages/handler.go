package messages

import (
	"encoding/json"
	"net/http"
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

type Conversation struct {
	ID            uuid.UUID  `json:"id"`
	IsGroup       bool       `json:"is_group"`
	Name          *string    `json:"name"`
	FirstName     *string    `json:"first_name"`
	LastName      *string    `json:"last_name"`
	CreatedAt     time.Time  `json:"created_at"`
	LastMessage   *string    `json:"last_message"`
	LastMessageAt *time.Time `json:"last_message_at"`
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

// GET /conversations
func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)

	rows, err := h.db.Query(r.Context(),
		`SELECT c.id, c.is_group, c.name,
       other.first_name, other.last_name,
       c.created_at,
       m.body AS last_message, m.sent_at AS last_message_at
     FROM conversations c
     JOIN conversation_members cm ON cm.conversation_id = c.id AND cm.user_id = $1
     LEFT JOIN LATERAL (
       SELECT u.first_name, u.last_name
       FROM conversation_members cm2
       JOIN users u ON u.id = cm2.user_id
       WHERE cm2.conversation_id = c.id AND cm2.user_id != $1
       LIMIT 1
     ) other ON NOT c.is_group
     LEFT JOIN LATERAL (
       SELECT body, sent_at FROM messages
       WHERE conversation_id = c.id
       ORDER BY sent_at DESC LIMIT 1
     ) m ON true
     WHERE c.status = 'active'
     ORDER BY COALESCE(m.sent_at, c.created_at) DESC`, userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch conversations")
		return
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		rows.Scan(&c.ID, &c.IsGroup, &c.Name, &c.FirstName, &c.LastName, &c.CreatedAt, &c.LastMessage, &c.LastMessageAt)
		convs = append(convs, c)
	}

	response.Success(w, http.StatusOK, convs)
}

// GET /conversations/requests — incoming message requests
func (h *Handler) ListMessageRequests(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	rows, err := h.db.Query(r.Context(),
		`SELECT c.id, c.is_group, c.name,
           other.first_name, other.last_name,
           c.created_at,
           m.body AS last_message, m.sent_at AS last_message_at
         FROM conversations c
         JOIN conversation_members cm ON cm.conversation_id = c.id
           AND cm.user_id = $1 AND cm.role = 'addressee'
         LEFT JOIN LATERAL (
           SELECT u.first_name, u.last_name
           FROM conversation_members cm2
           JOIN users u ON u.id = cm2.user_id
           WHERE cm2.conversation_id = c.id AND cm2.user_id != $1
           LIMIT 1
         ) other ON NOT c.is_group
         LEFT JOIN LATERAL (
           SELECT body, sent_at FROM messages
           WHERE conversation_id = c.id
           ORDER BY sent_at DESC LIMIT 1
         ) m ON true
         WHERE c.status = 'request'
         ORDER BY c.created_at DESC`, userID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch requests")
		return
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		rows.Scan(&c.ID, &c.IsGroup, &c.Name, &c.FirstName, &c.LastName, &c.CreatedAt, &c.LastMessage, &c.LastMessageAt)
		convs = append(convs, c)
	}
	response.Success(w, http.StatusOK, convs)
}

// POST /conversations — start a DM or group chat
// POST /conversations
func (h *Handler) CreateConversation(w http.ResponseWriter, r *http.Request) {
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

	// Check for existing DM
	if !isGroup && len(input.MemberIDs) == 1 {
		var existingID uuid.UUID
		err := h.db.QueryRow(r.Context(),
			`SELECT c.id FROM conversations c
             JOIN conversation_members cm1 ON cm1.conversation_id = c.id AND cm1.user_id = $1
             JOIN conversation_members cm2 ON cm2.conversation_id = c.id AND cm2.user_id = $2
             WHERE c.is_group = false
             LIMIT 1`,
			userID, input.MemberIDs[0],
		).Scan(&existingID)
		if err == nil {
			response.Success(w, http.StatusOK, map[string]any{"id": existingID, "is_group": false})
			return
		}
	}

	// New DM starts as a request, group chats are immediately active
	status := "active"
	if !isGroup {
		status = "request"
	}

	var convID uuid.UUID
	h.db.QueryRow(r.Context(),
		`INSERT INTO conversations (is_group, name, status) VALUES ($1, $2, $3) RETURNING id`,
		isGroup, input.Name, status,
	).Scan(&convID)

	// Creator is the requester, others are addressees
	h.db.Exec(r.Context(),
		`INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1, $2, 'requester')`,
		convID, userID,
	)
	for _, memberID := range input.MemberIDs {
		h.db.Exec(r.Context(),
			`INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1, $2, 'addressee')`,
			convID, memberID,
		)
	}

	response.Success(w, http.StatusCreated, map[string]any{"id": convID, "is_group": isGroup, "status": status})
}

// PATCH /conversations/{id}/status
func (h *Handler) UpdateConversationStatus(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	convID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid conversation id")
		return
	}

	var input struct {
		Status string `json:"status"` // "active" or "declined"
	}
	json.NewDecoder(r.Body).Decode(&input)
	if input.Status != "active" && input.Status != "declined" {
		response.Error(w, http.StatusBadRequest, "status must be 'active' or 'declined'")
		return
	}

	// Only the addressee can accept/decline
	var isMember bool
	h.db.QueryRow(r.Context(),
		`SELECT EXISTS(
            SELECT 1 FROM conversation_members
            WHERE conversation_id=$1 AND user_id=$2 AND role='addressee'
        )`, convID, userID,
	).Scan(&isMember)
	if !isMember {
		response.Error(w, http.StatusForbidden, "not authorised")
		return
	}

	if input.Status == "declined" {
		// Delete entirely so sender can try again
		h.db.Exec(r.Context(), `DELETE FROM conversations WHERE id=$1`, convID)
	} else {
		h.db.Exec(r.Context(),
			`UPDATE conversations SET status='active' WHERE id=$1`, convID,
		)
	}

	response.Success(w, http.StatusOK, map[string]string{"status": input.Status})
}

// GET /conversations/{id}/messages
func (h *Handler) GetMessages(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	convID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid conversation id")
		return
	}

	// Ensure user is a member
	var isMember bool
	h.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND user_id=$2)`,
		convID, userID,
	).Scan(&isMember)
	if !isMember {
		response.Error(w, http.StatusForbidden, "not a member of this conversation")
		return
	}

	rows, err := h.db.Query(r.Context(),
		`SELECT m.id, m.sender_id, u.first_name, u.last_name, u.avatar_url, m.body, m.sent_at
		 FROM messages m
		 JOIN users u ON u.id = m.sender_id
		 WHERE m.conversation_id = $1
		 ORDER BY m.sent_at ASC`, convID,
	)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "could not fetch messages")
		return
	}
	defer rows.Close()

	type Message struct {
		ID        uuid.UUID `json:"id"`
		SenderID  uuid.UUID `json:"sender_id"`
		FirstName string    `json:"first_name"`
		LastName  string    `json:"last_name"`
		AvatarURL *string   `json:"avatar_url"`
		Body      string    `json:"body"`
		SentAt    time.Time `json:"sent_at"`
	}

	var msgs []Message
	for rows.Next() {
		var m Message
		rows.Scan(&m.ID, &m.SenderID, &m.FirstName, &m.LastName, &m.AvatarURL, &m.Body, &m.SentAt)
		msgs = append(msgs, m)
	}

	response.Success(w, http.StatusOK, msgs)
}

// POST /conversations/{id}/messages
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CurrentUserID(r)
	convID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid conversation id")
		return
	}

	// Ensure user is a member
	var isMember bool
	h.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND user_id=$2)`,
		convID, userID,
	).Scan(&isMember)
	if !isMember {
		response.Error(w, http.StatusForbidden, "not a member of this conversation")
		return
	}

	var input struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Body == "" {
		response.Error(w, http.StatusBadRequest, "body is required")
		return
	}

	var msgID uuid.UUID
	h.db.QueryRow(r.Context(),
		`INSERT INTO messages (conversation_id, sender_id, body) VALUES ($1, $2, $3) RETURNING id`,
		convID, userID, input.Body,
	).Scan(&msgID)

	response.Success(w, http.StatusCreated, map[string]any{"id": msgID})
}
