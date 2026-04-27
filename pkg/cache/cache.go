package cache

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

const missingVersion int64 = 0

// Store defines the cache operations used by domain-level cache decorators.
type Store interface {
	Enabled() bool
	Key(parts ...string) string
	GetJSON(ctx context.Context, key string, dest any) (bool, error)
	SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error
	PublishJSON(ctx context.Context, channel string, value any) error
	Subscribe(ctx context.Context, channel string) (Subscription, error)
	GetVersion(ctx context.Context, key string) (int64, error)
	BumpVersions(ctx context.Context, keys ...string) error
	WithJitter(ttl time.Duration) time.Duration
	ReadThrough(ctx context.Context, key string, ttl time.Duration, dest any, loader func(context.Context, any) error) error
}

type Config struct {
	Enabled  bool
	Addr     string
	Password string
	DB       int
	TLS      bool
	Prefix   string
}

type Client struct {
	enabled bool
	prefix  string
	redis   redis.UniversalClient
	group   singleflight.Group
	randMu  sync.Mutex
	randSrc *rand.Rand
}

func New(ctx context.Context, cfg Config) (Store, error) {
	if !cfg.Enabled {
		return NewDisabled(), nil
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, fmt.Errorf("REDIS_ADDR not set")
	}

	options := &redis.Options{
		Addr:         strings.TrimSpace(cfg.Addr),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
		PoolTimeout:  1 * time.Second,
	}
	if cfg.TLS {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	client := redis.NewClient(options)
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	prefix := strings.TrimSpace(cfg.Prefix)
	if prefix == "" {
		prefix = "pr"
	}

	return &Client{
		enabled: true,
		prefix:  prefix,
		redis:   client,
		randSrc: rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

func NewDisabled() Store {
	return &Client{
		enabled: false,
		randSrc: rand.New(rand.NewSource(1)),
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.enabled
}

func (c *Client) Key(parts ...string) string {
	filtered := make([]string, 0, len(parts)+1)
	if c != nil && strings.TrimSpace(c.prefix) != "" {
		filtered = append(filtered, c.prefix)
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, ":")
}

func (c *Client) GetJSON(ctx context.Context, key string, dest any) (bool, error) {
	if !c.Enabled() {
		return false, nil
	}

	payload, err := c.redis.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	if !c.Enabled() {
		return nil
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return c.redis.Set(ctx, key, payload, c.WithJitter(ttl)).Err()
}

func (c *Client) GetVersion(ctx context.Context, key string) (int64, error) {
	if !c.Enabled() {
		return missingVersion, nil
	}

	version, err := c.redis.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			return missingVersion, nil
		}
		return missingVersion, err
	}
	if version < missingVersion {
		return missingVersion, nil
	}
	return version, nil
}

func (c *Client) BumpVersions(ctx context.Context, keys ...string) error {
	if !c.Enabled() || len(keys) == 0 {
		return nil
	}

	pipe := c.redis.Pipeline()
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		pipe.Incr(ctx, key)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (c *Client) WithJitter(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return ttl
	}
	if c == nil || c.randSrc == nil {
		return ttl
	}

	jitterWindow := ttl / 10
	if jitterWindow <= 0 {
		return ttl
	}

	c.randMu.Lock()
	defer c.randMu.Unlock()

	return ttl + time.Duration(c.randSrc.Int63n(int64(jitterWindow)))
}

func (c *Client) ReadThrough(ctx context.Context, key string, ttl time.Duration, dest any, loader func(context.Context, any) error) error {
	if !c.Enabled() {
		return loader(ctx, dest)
	}

	found, err := c.GetJSON(ctx, key, dest)
	if err == nil && found {
		return nil
	}
	if err != nil {
		log.Printf("cache get failed for %s: %v", key, err)
	}

	result, err, _ := c.group.Do(key, func() (any, error) {
		loaded := cloneDestination(dest)
		if err := loader(ctx, loaded); err != nil {
			return nil, err
		}

		payload, err := json.Marshal(loaded)
		if err != nil {
			return nil, err
		}
		if setErr := c.redis.Set(ctx, key, payload, c.WithJitter(ttl)).Err(); setErr != nil {
			log.Printf("cache set failed for %s: %v", key, setErr)
		}
		return payload, nil
	})
	if err != nil {
		return err
	}

	payload, ok := result.([]byte)
	if !ok {
		return fmt.Errorf("cache load for %s returned unexpected payload type %T", key, result)
	}

	return json.Unmarshal(payload, dest)
}

func cloneDestination(dest any) any {
	destType := reflect.TypeOf(dest)
	if destType == nil || destType.Kind() != reflect.Pointer {
		panic("cache destination must be a non-nil pointer")
	}
	return reflect.New(destType.Elem()).Interface()
}
