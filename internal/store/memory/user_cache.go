package memory

import (
	"context"
	"sync"

	"tdlibgo/internal/domain"
	"tdlibgo/internal/store"
)

// UserCache provides a thread-safe in-memory cache for user base data.
type UserCache struct {
	mu    sync.RWMutex
	users map[int64]domain.User
}

var _ store.UserCache = (*UserCache)(nil)

// NewUserCache creates a new in-memory UserCache.
func NewUserCache() *UserCache {
	return &UserCache{
		users: make(map[int64]domain.User),
	}
}

func (c *UserCache) GetByIDs(_ context.Context, ids []int64) (map[int64]domain.User, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make(map[int64]domain.User, len(ids))
	for _, id := range ids {
		if u, ok := c.users[id]; ok {
			out[id] = u
		}
	}
	return out, nil
}

func (c *UserCache) PutMany(_ context.Context, users []domain.User) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, u := range users {
		if u.ID != 0 {
			c.users[u.ID] = u
		}
	}
	return nil
}

func (c *UserCache) Delete(_ context.Context, ids []int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, id := range ids {
		delete(c.users, id)
	}
	return nil
}
