package chats

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/cache"
)

type EventBus interface {
	PublishUserEvent(ctx context.Context, userID uuid.UUID, event ServerEvent) error
	Start(ctx context.Context, hub *RealtimeHub) error
	ReplayUserEventsSince(ctx context.Context, userID uuid.UUID, cursor string) ([]ServerEvent, bool, error)
}

type redisRealtimeBus struct {
	store      cache.Store
	instanceID string
	channel    string
}

const (
	realtimeReplayTTL      = 30 * time.Minute
	realtimeReplayBusLimit = 512
)

type realtimeBusEnvelope struct {
	SourceID string      `json:"source_id"`
	UserID   uuid.UUID   `json:"user_id"`
	Event    ServerEvent `json:"event"`
}

func NewRedisRealtimeBus(store cache.Store) EventBus {
	if store == nil {
		return nil
	}

	return &redisRealtimeBus{
		store:      store,
		instanceID: uuid.NewString(),
		channel:    store.Key("chats", "events"),
	}
}

func (b *redisRealtimeBus) PublishUserEvent(ctx context.Context, userID uuid.UUID, event ServerEvent) error {
	if b == nil || b.store == nil || !b.store.Enabled() {
		return nil
	}

	if replayStore, ok := b.store.(replayEventStore); ok {
		// Keep a short per-user replay window in Redis so reconnects that land on
		// a different app instance do not immediately require a full resync.
		if err := replayStore.AppendJSONList(ctx, b.replayKey(userID), event, realtimeReplayBusLimit, realtimeReplayTTL); err != nil {
			return err
		}
	}

	return b.store.PublishJSON(ctx, b.channel, realtimeBusEnvelope{
		SourceID: b.instanceID,
		UserID:   userID,
		Event:    event,
	})
}

func (b *redisRealtimeBus) Start(ctx context.Context, hub *RealtimeHub) error {
	if b == nil || b.store == nil || !b.store.Enabled() || hub == nil {
		return nil
	}

	subscription, err := b.store.Subscribe(ctx, b.channel)
	if err != nil {
		return err
	}

	go func() {
		defer subscription.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case payload, ok := <-subscription.Messages():
				if !ok {
					return
				}

				var envelope realtimeBusEnvelope
				if err := json.Unmarshal(payload, &envelope); err != nil {
					continue
				}
				if envelope.SourceID == b.instanceID {
					continue
				}

				hub.DeliverUserEvent(envelope.UserID, envelope.Event)
			}
		}
	}()

	return nil
}

func (b *redisRealtimeBus) ReplayUserEventsSince(ctx context.Context, userID uuid.UUID, cursor string) ([]ServerEvent, bool, error) {
	if b == nil || b.store == nil || !b.store.Enabled() {
		return nil, false, nil
	}
	if cursor == "" {
		return nil, true, nil
	}

	replayStore, ok := b.store.(replayEventStore)
	if !ok {
		return nil, false, nil
	}

	var events []ServerEvent
	found, err := replayStore.ReadJSONList(ctx, b.replayKey(userID), &events)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}

	matchIndex := -1
	// Search backward because resume cursors almost always target the newest
	// edge of the replay window.
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Cursor == cursor {
			matchIndex = index
			break
		}
	}
	if matchIndex == -1 {
		return nil, false, nil
	}

	return events[matchIndex+1:], true, nil
}

func (b *redisRealtimeBus) replayKey(userID uuid.UUID) string {
	return b.store.Key("chats", "replay", "user", userID.String())
}

type replayEventStore interface {
	AppendJSONList(ctx context.Context, key string, value any, maxLen int64, ttl time.Duration) error
	ReadJSONList(ctx context.Context, key string, dest any) (bool, error)
}
