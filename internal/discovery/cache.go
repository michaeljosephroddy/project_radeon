package discovery

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type suggestionCache struct {
	mu    sync.RWMutex
	items map[uuid.UUID]cacheEntry
}

type cacheEntry struct {
	suggestions []ScoredUser
	expiresAt   time.Time
}

func newSuggestionCache() *suggestionCache {
	return &suggestionCache{items: make(map[uuid.UUID]cacheEntry)}
}

func (c *suggestionCache) get(userID uuid.UUID) ([]ScoredUser, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.items[userID]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.suggestions, true
}

func (c *suggestionCache) set(userID uuid.UUID, suggestions []ScoredUser) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[userID] = cacheEntry{
		suggestions: suggestions,
		expiresAt:   time.Now().Add(24 * time.Hour),
	}
}

func (c *suggestionCache) invalidate(userID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, userID)
}
