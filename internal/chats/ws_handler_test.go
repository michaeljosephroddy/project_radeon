package chats

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/project_radeon/api/pkg/middleware"
)

func newRealtimeTestServer(t *testing.T, handler *Handler) *httptest.Server {
	t.Helper()

	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawUserID := r.Header.Get("X-Test-User")
			if rawUserID == "" {
				rawUserID = fixedUser.String()
			}

			userID, err := uuid.Parse(rawUserID)
			if err != nil {
				t.Fatalf("parse test user id: %v", err)
			}

			ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	router.Get("/chats/ws", handler.ConnectRealtime)

	return httptest.NewServer(router)
}

func TestConnectRealtimeSendsReadyEvent(t *testing.T) {
	h := NewHandler(&mockQuerier{})
	server := newRealtimeTestServer(t, h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/chats/ws"
	connection, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var event ServerEvent
	if err := connection.ReadJSON(&event); err != nil {
		t.Fatalf("read ready event: %v", err)
	}

	if event.Type != "connection.ready" {
		t.Fatalf("event type = %q, want %q", event.Type, "connection.ready")
	}

	var payload map[string]any
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if payload["version"] != "v1" {
		t.Fatalf("version = %v, want %q", payload["version"], "v1")
	}

	rawConnectionID, ok := payload["connection_id"].(string)
	if !ok || rawConnectionID == "" {
		t.Fatalf("connection_id = %v, want non-empty string", payload["connection_id"])
	}
	if _, err := uuid.Parse(rawConnectionID); err != nil {
		t.Fatalf("connection_id parse: %v", err)
	}
}

func TestConnectRealtimeSendMessageAck(t *testing.T) {
	h := NewHandler(&mockQuerier{
		insertMessage: func(_ context.Context, chatID, userID uuid.UUID, body string, clientMessageID *string) (*Message, error) {
			chatSeq := int64(7)
			return &Message{
				ID:              uuid.MustParse("00000000-0000-0000-0000-000000000099"),
				ChatID:          chatID,
				SenderID:        userID,
				Username:        "tester",
				Body:            body,
				SentAt:          time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
				ClientMessageID: clientMessageID,
				ChatSeq:         &chatSeq,
			}, nil
		},
		getChat: func(_ context.Context, userID, chatID uuid.UUID) (*Chat, error) {
			username := "other-user"
			if userID == fixedUser {
				username = "self"
			}
			return &Chat{
				ID:            chatID,
				Username:      &username,
				CreatedAt:     time.Date(2026, 4, 27, 11, 0, 0, 0, time.UTC),
				LastMessage:   stringPtr("hello realtime"),
				LastMessageAt: timePtr(time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)),
				UnreadCount:   0,
				Status:        "active",
			}, nil
		},
	})
	server := newRealtimeTestServer(t, h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/chats/ws"
	connection, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{
		"X-Test-User": []string{fixedUser.String()},
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()

	var readyEvent ServerEvent
	if err := connection.ReadJSON(&readyEvent); err != nil {
		t.Fatalf("read ready event: %v", err)
	}

	sendPayload := map[string]any{
		"type": "send_message",
		"data": map[string]any{
			"chat_id":           fixedChat.String(),
			"client_message_id": "client-123",
			"body":              "hello realtime",
		},
	}
	if err := connection.WriteJSON(sendPayload); err != nil {
		t.Fatalf("write send_message: %v", err)
	}

	var ackEvent ServerEvent
	if err := connection.ReadJSON(&ackEvent); err != nil {
		t.Fatalf("read ack event: %v", err)
	}
	if ackEvent.Type != "chat.message.ack" {
		t.Fatalf("event type = %q, want %q", ackEvent.Type, "chat.message.ack")
	}

	var payload MessageAckEnvelope
	if err := json.Unmarshal(ackEvent.Data, &payload); err != nil {
		t.Fatalf("decode ack payload: %v", err)
	}

	if payload.ClientMessageID != "client-123" {
		t.Fatalf("client message id = %q, want %q", payload.ClientMessageID, "client-123")
	}
	if payload.Message.Body != "hello realtime" {
		t.Fatalf("message body = %q, want %q", payload.Message.Body, "hello realtime")
	}
	if payload.Message.ChatID != fixedChat {
		t.Fatalf("message chat id = %q, want %q", payload.Message.ChatID, fixedChat)
	}
	if payload.Summary == nil || payload.Summary.ID != fixedChat {
		t.Fatalf("summary = %+v, want chat summary for %s", payload.Summary, fixedChat)
	}
}

func TestHandleRealtimeResumeFallsBackToSharedReplay(t *testing.T) {
	hub := NewRealtimeHub()
	connection := &RealtimeConnection{
		ID:            uuid.New(),
		UserID:        fixedUser,
		Send:          make(chan ServerEvent, 4),
		ConnectedAt:   time.Now().UTC(),
		subscriptions: make(map[uuid.UUID]struct{}),
	}
	replayedEvent, err := newServerEvent("chat.summary.updated", Chat{
		ID:        fixedChat,
		CreatedAt: time.Now().UTC(),
		Status:    "active",
	})
	if err != nil {
		t.Fatalf("new server event: %v", err)
	}

	bus := &stubEventBus{
		replayEvents: []ServerEvent{replayedEvent},
		replayOK:     true,
	}
	handler := NewHandlerWithRealtimeInfra(&mockQuerier{}, nil, hub, bus)

	if err := handler.handleRealtimeResume(context.Background(), connection, ResumeCommand{LastCursor: stringPtr("missing-local-cursor")}); err != nil {
		t.Fatalf("handleRealtimeResume: %v", err)
	}

	select {
	case event := <-connection.Send:
		if event.Type != replayedEvent.Type || event.Cursor != replayedEvent.Cursor {
			t.Fatalf("unexpected replayed event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("expected replayed event to be enqueued")
	}
}

func TestHandleRealtimeResumeEmitsResyncWhenReplayUnavailable(t *testing.T) {
	hub := NewRealtimeHub()
	connection := &RealtimeConnection{
		ID:            uuid.New(),
		UserID:        fixedUser,
		Send:          make(chan ServerEvent, 2),
		ConnectedAt:   time.Now().UTC(),
		subscriptions: make(map[uuid.UUID]struct{}),
	}
	handler := NewHandlerWithRealtimeInfra(&mockQuerier{}, nil, hub, &stubEventBus{})

	if err := handler.handleRealtimeResume(context.Background(), connection, ResumeCommand{LastCursor: stringPtr("unknown-cursor")}); err != nil {
		t.Fatalf("handleRealtimeResume: %v", err)
	}

	select {
	case event := <-connection.Send:
		if event.Type != "system.resync_required" {
			t.Fatalf("event type = %q, want %q", event.Type, "system.resync_required")
		}
	case <-time.After(time.Second):
		t.Fatal("expected resync event to be enqueued")
	}
}

type stubEventBus struct {
	replayEvents []ServerEvent
	replayOK     bool
	replayErr    error
}

func (s *stubEventBus) PublishUserEvent(context.Context, uuid.UUID, ServerEvent) error {
	return nil
}

func (s *stubEventBus) Start(context.Context, *RealtimeHub) error {
	return nil
}

func (s *stubEventBus) ReplayUserEventsSince(context.Context, uuid.UUID, string) ([]ServerEvent, bool, error) {
	if s.replayErr != nil {
		return nil, false, s.replayErr
	}
	if !s.replayOK {
		return nil, false, nil
	}
	return s.replayEvents, true, nil
}

func TestHandleRealtimeResumePropagatesReplayError(t *testing.T) {
	hub := NewRealtimeHub()
	connection := &RealtimeConnection{
		ID:            uuid.New(),
		UserID:        fixedUser,
		Send:          make(chan ServerEvent, 1),
		ConnectedAt:   time.Now().UTC(),
		subscriptions: make(map[uuid.UUID]struct{}),
	}
	handler := NewHandlerWithRealtimeInfra(&mockQuerier{}, nil, hub, &stubEventBus{replayErr: errors.New("boom")})

	if err := handler.handleRealtimeResume(context.Background(), connection, ResumeCommand{LastCursor: stringPtr("unknown-cursor")}); err == nil {
		t.Fatal("expected replay error to be returned")
	}
}

func stringPtr(value string) *string {
	return &value
}

func timePtr(value time.Time) *time.Time {
	return &value
}
