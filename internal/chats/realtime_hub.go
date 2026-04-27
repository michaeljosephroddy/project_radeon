package chats

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const realtimeWriteBufferSize = 16
const realtimeReplayLimit = 512

type RealtimeUserEvent struct {
	UserID uuid.UUID
	Event  ServerEvent
}

type RealtimeConnection struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Send          chan ServerEvent
	ConnectedAt   time.Time
	subscriptions map[uuid.UUID]struct{}
}

type RealtimeHub struct {
	mu          sync.RWMutex
	connections map[uuid.UUID]map[uuid.UUID]*RealtimeConnection
	replay      []RealtimeUserEvent
}

func NewRealtimeHub() *RealtimeHub {
	return &RealtimeHub{
		connections: make(map[uuid.UUID]map[uuid.UUID]*RealtimeConnection),
		replay:      make([]RealtimeUserEvent, 0, realtimeReplayLimit),
	}
}

func (h *RealtimeHub) Register(userID uuid.UUID) *RealtimeConnection {
	connection := &RealtimeConnection{
		ID:            uuid.New(),
		UserID:        userID,
		Send:          make(chan ServerEvent, realtimeWriteBufferSize),
		ConnectedAt:   time.Now().UTC(),
		subscriptions: make(map[uuid.UUID]struct{}),
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	userConnections := h.connections[userID]
	if userConnections == nil {
		userConnections = make(map[uuid.UUID]*RealtimeConnection)
		h.connections[userID] = userConnections
	}
	userConnections[connection.ID] = connection
	return connection
}

func (h *RealtimeHub) Unregister(connection *RealtimeConnection) {
	if connection == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	userConnections := h.connections[connection.UserID]
	if userConnections == nil {
		return
	}

	delete(userConnections, connection.ID)
	if len(userConnections) == 0 {
		delete(h.connections, connection.UserID)
	}
}

func (h *RealtimeHub) UserConnections(userID uuid.UUID) []*RealtimeConnection {
	h.mu.RLock()
	defer h.mu.RUnlock()

	userConnections := h.connections[userID]
	if len(userConnections) == 0 {
		return nil
	}

	connections := make([]*RealtimeConnection, 0, len(userConnections))
	for _, connection := range userConnections {
		connections = append(connections, connection)
	}
	return connections
}

func (h *RealtimeHub) DeliverUserEvent(userID uuid.UUID, event ServerEvent) {
	h.mu.Lock()
	h.replay = append(h.replay, RealtimeUserEvent{
		UserID: userID,
		Event:  event,
	})
	if len(h.replay) > realtimeReplayLimit {
		h.replay = h.replay[len(h.replay)-realtimeReplayLimit:]
	}

	userConnections := h.connections[userID]
	connections := make([]*RealtimeConnection, 0, len(userConnections))
	for _, connection := range userConnections {
		connections = append(connections, connection)
	}
	h.mu.Unlock()

	for _, connection := range connections {
		select {
		case connection.Send <- event:
		default:
			// Slow consumers will recover via resume/resync instead of blocking fanout.
		}
	}
}

func (h *RealtimeHub) ReplaySince(userID uuid.UUID, cursor string) ([]ServerEvent, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if cursor == "" {
		return nil, true
	}

	matchIndex := -1
	for index := len(h.replay) - 1; index >= 0; index-- {
		entry := h.replay[index]
		if entry.UserID == userID && entry.Event.Cursor == cursor {
			matchIndex = index
			break
		}
	}
	if matchIndex == -1 {
		return nil, false
	}

	events := make([]ServerEvent, 0)
	for _, entry := range h.replay[matchIndex+1:] {
		if entry.UserID == userID {
			events = append(events, entry.Event)
		}
	}
	return events, true
}

func (c *RealtimeConnection) Subscribe(chatID uuid.UUID) {
	c.subscriptions[chatID] = struct{}{}
}

func (c *RealtimeConnection) Unsubscribe(chatID uuid.UUID) {
	delete(c.subscriptions, chatID)
}
