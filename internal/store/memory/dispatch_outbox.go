package memory

import (
	"context"
	"sync"
	"time"

	"tdlibgo/internal/store"
)

// DispatchOutboxStore provides an in-memory transactional outbox store.
type DispatchOutboxStore struct {
	mu      sync.Mutex
	pending []store.DispatchOutboxItem
}

var _ store.DispatchOutboxStore = (*DispatchOutboxStore)(nil)

// NewDispatchOutboxStore creates a new in-memory DispatchOutboxStore.
func NewDispatchOutboxStore() *DispatchOutboxStore {
	return &DispatchOutboxStore{
		pending: make([]store.DispatchOutboxItem, 0),
	}
}

func (s *DispatchOutboxStore) AddItem(item store.DispatchOutboxItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, item)
}

func (s *DispatchOutboxStore) ClaimPending(ctx context.Context, limit int) ([]store.DispatchOutboxItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pending) == 0 {
		return nil, nil
	}
	n := limit
	if n > len(s.pending) || n <= 0 {
		n = len(s.pending)
	}
	batch := make([]store.DispatchOutboxItem, n)
	copy(batch, s.pending[:n])
	s.pending = s.pending[n:]
	return batch, nil
}

func (s *DispatchOutboxStore) MarkDelivered(ctx context.Context, item store.DispatchOutboxItem) error {
	return nil
}

func (s *DispatchOutboxStore) MarkFailed(ctx context.Context, item store.DispatchOutboxItem, lastError string) error {
	return nil
}

func (s *DispatchOutboxStore) DeleteFailed(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	return 0, nil
}
