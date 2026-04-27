package chats

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/project_radeon/api/pkg/middleware"
)

const (
	realtimeReadLimit    = 8 * 1024
	realtimeWriteTimeout = 10 * time.Second
	realtimePongTimeout  = 60 * time.Second
	realtimePingEvery    = 25 * time.Second
)

var realtimeUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type connectionReadyPayload struct {
	ConnectionID string `json:"connection_id"`
	Version      string `json:"version"`
}

type resyncRequiredPayload struct {
	Reason string `json:"reason"`
}

// ConnectRealtime upgrades an authenticated chat request into a shared realtime socket.
func (h *Handler) ConnectRealtime(w http.ResponseWriter, r *http.Request) {
	if h.realtime == nil {
		http.Error(w, "chat realtime unavailable", http.StatusServiceUnavailable)
		return
	}

	userID := middleware.CurrentUserID(r)
	socket, err := realtimeUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	connection := h.realtime.Register(userID)
	defer h.realtime.Unregister(connection)
	defer close(connection.Send)
	defer socket.Close()

	socket.SetReadLimit(realtimeReadLimit)
	socket.SetReadDeadline(time.Now().Add(realtimePongTimeout))
	socket.SetPongHandler(func(string) error {
		socket.SetReadDeadline(time.Now().Add(realtimePongTimeout))
		return nil
	})

	if err := h.enqueueServerEvent(connection, "connection.ready", connectionReadyPayload{
		ConnectionID: connection.ID.String(),
		Version:      "v1",
	}); err != nil {
		return
	}

	readErrs := make(chan error, 1)
	go h.readRealtimeCommands(r.Context(), socket, connection, readErrs)

	ticker := time.NewTicker(realtimePingEvery)
	defer ticker.Stop()

	for {
		select {
		case event, ok := <-connection.Send:
			if !ok {
				return
			}
			if err := writeRealtimeEvent(socket, event); err != nil {
				return
			}
		case err := <-readErrs:
			if err == nil || websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return
			}
			return
		case <-ticker.C:
			if err := writeRealtimePing(socket); err != nil {
				return
			}
		}
	}
}

func (h *Handler) readRealtimeCommands(ctx context.Context, socket *websocket.Conn, connection *RealtimeConnection, errs chan<- error) {
	for {
		_, payload, err := socket.ReadMessage()
		if err != nil {
			errs <- err
			return
		}

		var command ClientCommand
		if err := json.Unmarshal(payload, &command); err != nil {
			errs <- err
			return
		}

		if err := h.handleRealtimeCommand(ctx, connection, command); err != nil {
			errs <- err
			return
		}
	}
}

func (h *Handler) handleRealtimeCommand(ctx context.Context, connection *RealtimeConnection, command ClientCommand) error {
	switch command.Type {
	case "typing_start", "typing_stop":
		return nil
	case "resume":
		var payload ResumeCommand
		if err := decodeCommandPayload(command.Data, &payload); err != nil {
			return err
		}
		return h.handleRealtimeResume(connection, payload)
	case "subscribe_chat":
		var payload SubscribeChatCommand
		if err := decodeCommandPayload(command.Data, &payload); err != nil {
			return err
		}
		connection.Subscribe(payload.ChatID)
		return nil
	case "unsubscribe_chat":
		var payload UnsubscribeChatCommand
		if err := decodeCommandPayload(command.Data, &payload); err != nil {
			return err
		}
		connection.Unsubscribe(payload.ChatID)
		return nil
	case "mark_read":
		var payload MarkReadCommand
		if err := decodeCommandPayload(command.Data, &payload); err != nil {
			return err
		}
		return h.handleRealtimeMarkRead(ctx, connection, payload)
	case "send_message":
		var payload SendMessageCommand
		if err := decodeCommandPayload(command.Data, &payload); err != nil {
			return err
		}
		return h.handleRealtimeSendMessage(ctx, connection, payload)
	case "":
		return errors.New("missing realtime command type")
	default:
		return nil
	}
}

func decodeCommandPayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, target)
}

func (h *Handler) enqueueServerEvent(connection *RealtimeConnection, eventType string, payload any) error {
	event, err := newServerEvent(eventType, payload)
	if err != nil {
		return err
	}

	select {
	case connection.Send <- event:
		return nil
	default:
		return errors.New("realtime connection send buffer full")
	}
}

func newServerEvent(eventType string, payload any) (ServerEvent, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return ServerEvent{}, err
	}

	eventID := uuid.New()
	occurredAt := time.Now().UTC()
	return ServerEvent{
		Type:       eventType,
		EventID:    eventID,
		OccurredAt: occurredAt,
		Cursor:     occurredAt.Format(time.RFC3339Nano) + "|" + eventID.String(),
		Data:       payloadJSON,
	}, nil
}

func writeRealtimeEvent(socket *websocket.Conn, event ServerEvent) error {
	socket.SetWriteDeadline(time.Now().Add(realtimeWriteTimeout))
	return socket.WriteJSON(event)
}

func writeRealtimePing(socket *websocket.Conn) error {
	return socket.WriteControl(
		websocket.PingMessage,
		[]byte("ping"),
		time.Now().Add(realtimeWriteTimeout),
	)
}

func (h *Handler) handleRealtimeSendMessage(ctx context.Context, connection *RealtimeConnection, command SendMessageCommand) error {
	body := strings.TrimSpace(command.Body)
	clientMessageID := strings.TrimSpace(command.ClientMessageID)
	if command.ChatID == uuid.Nil || body == "" || clientMessageID == "" {
		return h.emitUserEvent(ctx, connection.UserID, "chat.message.failed", MessageFailedEnvelope{
			ChatID:          command.ChatID,
			ClientMessageID: clientMessageID,
			Error:           "chat_id, client_message_id, and body are required",
		})
	}

	isMember, err := h.db.IsMemberOfChat(ctx, command.ChatID, connection.UserID)
	if err != nil {
		return err
	}
	if !isMember {
		return h.emitUserEvent(ctx, connection.UserID, "chat.message.failed", MessageFailedEnvelope{
			ChatID:          command.ChatID,
			ClientMessageID: clientMessageID,
			Error:           "not a member of this chat",
		})
	}

	status, err := h.db.GetChatStatus(ctx, command.ChatID)
	if err != nil {
		return err
	}
	if status != "active" {
		return h.emitUserEvent(ctx, connection.UserID, "chat.message.failed", MessageFailedEnvelope{
			ChatID:          command.ChatID,
			ClientMessageID: clientMessageID,
			Error:           "chat is not open for messaging",
		})
	}

	message, err := h.db.InsertMessage(ctx, command.ChatID, connection.UserID, body, &clientMessageID)
	if err != nil {
		return err
	}

	if h.notifier != nil {
		_ = h.notifier.NotifyChatMessage(ctx, command.ChatID, message.ID, connection.UserID, message.Body)
		_ = h.notifier.MarkChatRead(ctx, command.ChatID, connection.UserID, &message.ID, time.Now().UTC())
	}

	senderSummary, err := h.db.GetChat(ctx, connection.UserID, command.ChatID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	if err := h.emitAckEvent(ctx, connection.UserID, MessageAckEnvelope{
		ChatID:          command.ChatID,
		ClientMessageID: clientMessageID,
		Message:         *message,
		Summary:         senderSummary,
	}); err != nil {
		return err
	}

	return h.broadcastMessageCreated(ctx, command.ChatID, message)
}

func (h *Handler) handleRealtimeResume(connection *RealtimeConnection, command ResumeCommand) error {
	lastCursor := ""
	if command.LastCursor != nil {
		lastCursor = strings.TrimSpace(*command.LastCursor)
	}
	events, ok := h.realtime.ReplaySince(connection.UserID, lastCursor)
	if !ok {
		return h.enqueueServerEvent(connection, "system.resync_required", resyncRequiredPayload{
			Reason: "resume cursor unavailable",
		})
	}
	for _, event := range events {
		select {
		case connection.Send <- event:
		default:
			return errors.New("realtime connection send buffer full")
		}
	}
	return nil
}

func (h *Handler) emitUserEvent(ctx context.Context, userID uuid.UUID, eventType string, payload any) error {
	event, err := newServerEvent(eventType, payload)
	if err != nil {
		return err
	}
	h.realtime.DeliverUserEvent(userID, event)
	if h.bus != nil {
		_ = h.bus.PublishUserEvent(ctx, userID, event)
	}
	return nil
}

func (h *Handler) emitAckEvent(ctx context.Context, userID uuid.UUID, payload MessageAckEnvelope) error {
	return h.emitUserEvent(ctx, userID, "chat.message.ack", payload)
}

func (h *Handler) emitSummaryEvent(ctx context.Context, userID uuid.UUID, summary *Chat) error {
	if summary == nil {
		return nil
	}
	return h.emitUserEvent(ctx, userID, "chat.summary.updated", summary)
}

func (h *Handler) emitReadEvent(ctx context.Context, userID uuid.UUID, payload ReadReceiptEnvelope) error {
	return h.emitUserEvent(ctx, userID, "chat.read.updated", payload)
}

func (h *Handler) emitCreatedEvent(ctx context.Context, userID uuid.UUID, payload MessageEnvelope) error {
	return h.emitUserEvent(ctx, userID, "chat.message.created", payload)
}

func (h *Handler) handleRealtimeMarkRead(ctx context.Context, connection *RealtimeConnection, command MarkReadCommand) error {
	if h.notifier == nil || command.ChatID == uuid.Nil {
		return nil
	}

	isMember, err := h.db.IsMemberOfChat(ctx, command.ChatID, connection.UserID)
	if err != nil {
		return err
	}
	if !isMember {
		return nil
	}

	readAt := time.Now().UTC()
	if err := h.notifier.MarkChatRead(ctx, command.ChatID, connection.UserID, command.LastReadMessageID, readAt); err != nil {
		return err
	}

	return h.broadcastReadUpdated(ctx, command.ChatID, connection.UserID, command.LastReadMessageID, readAt)
}

func (h *Handler) broadcastMessageCreated(ctx context.Context, chatID uuid.UUID, message *Message) error {
	if h.realtime == nil {
		return nil
	}

	memberIDs, err := h.db.ListChatMemberIDs(ctx, chatID)
	if err != nil {
		return err
	}

	for _, memberID := range memberIDs {
		summary, err := h.db.GetChat(ctx, memberID, chatID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}

		payload := MessageEnvelope{
			ChatID:  chatID,
			Message: *message,
			Summary: summary,
		}

		if err := h.emitCreatedEvent(ctx, memberID, payload); err != nil {
			return err
		}
	}

	return nil
}

func (h *Handler) broadcastReadUpdated(ctx context.Context, chatID, userID uuid.UUID, lastReadMessageID *uuid.UUID, readAt time.Time) error {
	if h.realtime == nil {
		return nil
	}

	memberIDs, err := h.db.ListChatMemberIDs(ctx, chatID)
	if err != nil {
		return err
	}

	readPayload := ReadReceiptEnvelope{
		ChatID:            chatID,
		UserID:            userID,
		LastReadMessageID: lastReadMessageID,
		ReadAt:            readAt,
	}

	for _, memberID := range memberIDs {
		if memberID == userID {
			continue
		}
		if err := h.emitReadEvent(ctx, memberID, readPayload); err != nil {
			return err
		}
	}

	currentUserSummary, err := h.db.GetChat(ctx, userID, chatID)
	if err == nil {
		return h.emitSummaryEvent(ctx, userID, currentUserSummary)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}

	return nil
}
