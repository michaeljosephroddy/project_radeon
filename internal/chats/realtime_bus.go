package chats

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/project_radeon/api/pkg/cache"
)

type EventBus interface {
	PublishUserEvent(ctx context.Context, userID uuid.UUID, event ServerEvent) error
	Start(ctx context.Context, hub *RealtimeHub) error
}

type redisRealtimeBus struct {
	store      cache.Store
	instanceID string
	channel    string
}

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
