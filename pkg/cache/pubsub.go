package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/project_radeon/api/pkg/observability"
	"github.com/redis/go-redis/v9"
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
	start := time.Now()
	err = c.redis.Publish(ctx, channel, payload).Err()
	observability.ObserveDuration("cache.publish_json", time.Since(start), err)
	return err
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

	start := time.Now()
	pubsub := c.redis.Subscribe(ctx, channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		close(out)
		observability.ObserveDuration("cache.subscribe", time.Since(start), err)
		return nil, err
	}
	observability.ObserveDuration("cache.subscribe", time.Since(start), nil)

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

var allowFixedWindowScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
if current > tonumber(ARGV[1]) then
  return 0
end
return 1
`)

func (c *Client) AllowFixedWindow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if !c.Enabled() {
		return true, nil
	}
	if limit <= 0 {
		return false, nil
	}
	if window <= 0 {
		window = time.Minute
	}

	start := time.Now()
	result, err := allowFixedWindowScript.Run(ctx, c.redis, []string{key}, limit, window.Milliseconds()).Int()
	observability.ObserveDuration("cache.allow_fixed_window", time.Since(start), err)
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (c *Client) AppendJSONList(ctx context.Context, key string, value any, maxLen int64, ttl time.Duration) error {
	if !c.Enabled() {
		return nil
	}
	if maxLen <= 0 {
		maxLen = 1
	}
	if ttl <= 0 {
		ttl = time.Minute
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	start := time.Now()
	pipe := c.redis.TxPipeline()
	pipe.RPush(ctx, key, payload)
	pipe.LTrim(ctx, key, -maxLen, -1)
	pipe.PExpire(ctx, key, ttl)
	_, err = pipe.Exec(ctx)
	observability.ObserveDuration("cache.append_json_list", time.Since(start), err)
	return err
}

func (c *Client) ReadJSONList(ctx context.Context, key string, dest any) (bool, error) {
	if !c.Enabled() {
		return false, nil
	}

	start := time.Now()
	values, err := c.redis.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		if err == redis.Nil {
			observability.IncrementCounter("cache.read_json_list.miss", 1)
			observability.ObserveDuration("cache.read_json_list", time.Since(start), nil)
			return false, nil
		}
		observability.ObserveDuration("cache.read_json_list", time.Since(start), err)
		return false, err
	}
	if len(values) == 0 {
		observability.IncrementCounter("cache.read_json_list.miss", 1)
		observability.ObserveDuration("cache.read_json_list", time.Since(start), nil)
		return false, nil
	}

	var builder strings.Builder
	builder.Grow(2 + len(values)*16)
	builder.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(value)
	}
	builder.WriteByte(']')

	if err := json.Unmarshal([]byte(builder.String()), dest); err != nil {
		observability.ObserveDuration("cache.read_json_list", time.Since(start), err)
		return false, fmt.Errorf("decode json list %s: %w", key, err)
	}
	observability.IncrementCounter("cache.read_json_list.hit", 1)
	observability.ObserveDuration("cache.read_json_list", time.Since(start), nil)
	return true, nil
}
