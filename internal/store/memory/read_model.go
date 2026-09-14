package memory

import (
	"context"
	"sync"

	"tdlibgo/internal/domain"
	"tdlibgo/internal/store"
)

// ReadModelVersionStore provides an in-memory implementation of store.ReadModelVersionStore.
type ReadModelVersionStore struct {
	mu     sync.RWMutex
	hashes map[store.ReadModelKey]int64
}

var _ store.ReadModelVersionStore = (*ReadModelVersionStore)(nil)

// NewReadModelVersionStore creates a new in-memory ReadModelVersionStore.
func NewReadModelVersionStore() *ReadModelVersionStore {
	return &ReadModelVersionStore{
		hashes: make(map[store.ReadModelKey]int64),
	}
}

func (s *ReadModelVersionStore) ReadModelHash(ctx context.Context, model string, ownerUserID int64, peerType domain.PeerType, peerID int64) (int64, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	k := store.ReadModelKey{
		Model:       model,
		OwnerUserID: ownerUserID,
		PeerType:    peerType,
		PeerID:      peerID,
	}
	h, ok := s.hashes[k]
	return h, ok, nil
}

func (s *ReadModelVersionStore) ReadModelHashes(ctx context.Context, keys []store.ReadModelKey) (map[store.ReadModelKey]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make(map[store.ReadModelKey]int64, len(keys))
	for _, k := range keys {
		if h, ok := s.hashes[k]; ok {
			res[k] = h
		}
	}
	return res, nil
}

func (s *ReadModelVersionStore) SetHash(k store.ReadModelKey, hash int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hashes[k] = hash
}
