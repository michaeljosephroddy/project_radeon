package cache

import (
	"context"
	"encoding/json"
	"sync"
)

type Subscription interface {
	Messages() <-chan []byte
	Close() error
}

type redisSubscription struct {
	cancel func() error
	once   sync.Once
	out    chan []byte
}

func (s *redisSubscription) Messages() <-chan []byte {
	return s.out
}

func (s *redisSubscription) Close() error {
	var err error
	s.once.Do(func() {
		err = s.cancel()
		close(s.out)
	})
	return err
}

func (c *Client) PublishJSON(ctx context.Context, channel string, value any) error {
	if !c.Enabled() {
		return nil
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.redis.Publish(ctx, channel, payload).Err()
}

func (c *Client) Subscribe(ctx context.Context, channel string) (Subscription, error) {
	out := make(chan []byte, 32)
	if !c.Enabled() {
		close(out)
		return &redisSubscription{
			cancel: func() error { return nil },
			out:    out,
		}, nil
	}

	pubsub := c.redis.Subscribe(ctx, channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		close(out)
		return nil, err
	}

	subscription := &redisSubscription{
		cancel: pubsub.Close,
		out:    out,
	}

	go func() {
		defer subscription.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-pubsub.Channel():
				if !ok {
					return
				}
				out <- []byte(message.Payload)
			}
		}
	}()

	return subscription, nil
}

var _ Subscription = (*redisSubscription)(nil)
