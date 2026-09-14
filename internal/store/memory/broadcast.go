package memory

import (
	"context"
	"sync"
	"time"

	"tdlibgo/internal/domain"
	"tdlibgo/internal/store"
)

// BroadcastStore provides an in-memory implementation of store.BroadcastStore.
type BroadcastStore struct {
	mu         sync.RWMutex
	broadcasts []domain.Broadcast
	nextID     int64
}

var _ store.BroadcastStore = (*BroadcastStore)(nil)

// NewBroadcastStore creates an in-memory BroadcastStore.
func NewBroadcastStore() *BroadcastStore {
	return &BroadcastStore{nextID: 1}
}

func (s *BroadcastStore) PreviewBroadcastRecipients(ctx context.Context, mode domain.BroadcastTargetMode, selectedUserIDs []int64) (int64, error) {
	return int64(len(selectedUserIDs)), nil
}

func (s *BroadcastStore) CreateBroadcast(ctx context.Context, message string, entities []domain.MessageEntity, mode domain.BroadcastTargetMode, selectedUserIDs []int64, createdBy string) (domain.Broadcast, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := domain.Broadcast{
		ID:         s.nextID,
		Message:    message,
		Entities:   entities,
		TargetMode: mode,
		CreatedBy:  createdBy,
		CreatedAt:  time.Now(),
	}
	s.nextID++
	s.broadcasts = append(s.broadcasts, b)
	return b, nil
}

func (s *BroadcastStore) MaterializeBroadcastRecipients(ctx context.Context, limit int) (int, error) {
	return 0, nil
}

func (s *BroadcastStore) ClaimBroadcastRecipients(ctx context.Context, leaseToken string, limit int, lease time.Duration) ([]store.BroadcastRecipientClaim, error) {
	return nil, nil
}

func (s *BroadcastStore) ReleaseBroadcastRecipient(ctx context.Context, claim store.BroadcastRecipientClaim, cause string) error {
	return nil
}

func (s *BroadcastStore) ListBroadcasts(ctx context.Context, beforeID int64, limit int) ([]domain.Broadcast, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var res []domain.Broadcast
	for _, b := range s.broadcasts {
		if beforeID == 0 || b.ID < beforeID {
			res = append(res, b)
		}
		if limit > 0 && len(res) >= limit {
			break
		}
	}
	return res, false, nil
}
