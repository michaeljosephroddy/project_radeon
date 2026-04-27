package cachetest

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/project_radeon/api/pkg/cache"
)

type Store struct {
	mu       sync.Mutex
	prefix   string
	payloads map[string][]byte
	versions map[string]int64
}

type subscription struct {
	messages chan []byte
	once     sync.Once
}

func NewStore() *Store {
	return &Store{
		prefix:   "test",
		payloads: make(map[string][]byte),
		versions: make(map[string]int64),
	}
}

func (s *Store) Enabled() bool {
	return true
}

func (s *Store) Key(parts ...string) string {
	filtered := []string{s.prefix}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, ":")
}

func (s *Store) GetJSON(_ context.Context, key string, dest any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, ok := s.payloads[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(payload, dest)
}

func (s *Store) SetJSON(_ context.Context, key string, value any, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.payloads[key] = payload
	return nil
}

func (s *Store) PublishJSON(_ context.Context, _ string, _ any) error {
	return nil
}

func (s *Store) Subscribe(_ context.Context, _ string) (cache.Subscription, error) {
	return &subscription{messages: make(chan []byte)}, nil
}

func (s *Store) GetVersion(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	version, ok := s.versions[key]
	if !ok || version < 0 {
		return 0, nil
	}
	return version, nil
}

func (s *Store) BumpVersions(_ context.Context, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, key := range keys {
		s.versions[key]++
		if s.versions[key] < 0 {
			s.versions[key] = 0
		}
	}
	return nil
}

func (s *Store) WithJitter(ttl time.Duration) time.Duration {
	return ttl
}

func (s *Store) ReadThrough(ctx context.Context, key string, ttl time.Duration, dest any, loader func(context.Context, any) error) error {
	found, err := s.GetJSON(ctx, key, dest)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	if err := loader(ctx, dest); err != nil {
		return err
	}
	return s.SetJSON(ctx, key, dest, ttl)
}

func (s *subscription) Messages() <-chan []byte {
	return s.messages
}

func (s *subscription) Close() error {
	s.once.Do(func() {
		if s.messages != nil {
			close(s.messages)
			s.messages = nil
		}
	})
	return nil
}
