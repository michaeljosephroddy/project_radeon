package chats

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ClientCommand struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type ServerEvent struct {
	Type       string          `json:"type"`
	EventID    uuid.UUID       `json:"event_id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Cursor     string          `json:"cursor"`
	Data       json.RawMessage `json:"data"`
}

type ResumeCommand struct {
	LastCursor *string `json:"last_cursor,omitempty"`
}

type SubscribeChatCommand struct {
	ChatID uuid.UUID `json:"chat_id"`
}

type UnsubscribeChatCommand struct {
	ChatID uuid.UUID `json:"chat_id"`
}

type SendMessageCommand struct {
	ChatID          uuid.UUID `json:"chat_id"`
	ClientMessageID string    `json:"client_message_id"`
	Body            string    `json:"body"`
}

type MarkReadCommand struct {
	ChatID            uuid.UUID  `json:"chat_id"`
	LastReadMessageID *uuid.UUID `json:"last_read_message_id,omitempty"`
}

type TypingCommand struct {
	ChatID uuid.UUID `json:"chat_id"`
}

type MessageEnvelope struct {
	ChatID  uuid.UUID `json:"chat_id"`
	Message Message   `json:"message"`
	Summary *Chat     `json:"summary,omitempty"`
}

type MessageAckEnvelope struct {
	ChatID          uuid.UUID `json:"chat_id"`
	ClientMessageID string    `json:"client_message_id"`
	Message         Message   `json:"message"`
	Summary         *Chat     `json:"summary,omitempty"`
}

type ReadReceiptEnvelope struct {
	ChatID            uuid.UUID  `json:"chat_id"`
	UserID            uuid.UUID  `json:"user_id"`
	LastReadMessageID *uuid.UUID `json:"last_read_message_id,omitempty"`
	ReadAt            time.Time  `json:"read_at"`
}

type MessageFailedEnvelope struct {
	ChatID          uuid.UUID `json:"chat_id"`
	ClientMessageID string    `json:"client_message_id"`
	Error           string    `json:"error"`
}
