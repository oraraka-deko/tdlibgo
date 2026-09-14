package memory

import (
	"context"
	"sync"
	"time"

	"tdlibgo/internal/domain"
	"tdlibgo/internal/store"
)

// AccountLifecycleStore provides an in-memory implementation of store.AccountLifecycleStore.
type AccountLifecycleStore struct {
	mu        sync.RWMutex
	users     *UserStore
	passwords *PasswordStore
	pending   map[int64]domain.AccountDeletionRequest
	nextID    int64
}

var _ store.AccountLifecycleStore = (*AccountLifecycleStore)(nil)

// NewAccountLifecycleStore creates an in-memory AccountLifecycleStore.
func NewAccountLifecycleStore(users *UserStore, passwords *PasswordStore) *AccountLifecycleStore {
	return &AccountLifecycleStore{
		users:     users,
		passwords: passwords,
		pending:   make(map[int64]domain.AccountDeletionRequest),
		nextID:    1,
	}
}

func (s *AccountLifecycleStore) AccountDeletionSnapshot(ctx context.Context, userID int64) (domain.AccountDeletionSnapshot, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, found, err := s.users.ByID(ctx, userID)
	if err != nil || !found {
		return domain.AccountDeletionSnapshot{}, false, err
	}

	snap := domain.AccountDeletionSnapshot{
		User:        user,
		HasPassword: false,
	}

	if s.passwords != nil {
		pw, ok, _ := s.passwords.GetByUser(ctx, userID)
		if ok && pw.HasPassword {
			snap.HasPassword = true
			snap.PasswordUpdatedAt = time.Now().Add(-14 * 24 * time.Hour)
		}
	}

	if req, ok := s.pending[userID]; ok {
		snap.Pending = &req
	}

	return snap, true, nil
}

func (s *AccountLifecycleStore) ScheduleAccountDeletion(ctx context.Context, req domain.ScheduleAccountDeletion) (domain.AccountDeletionRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	del := domain.AccountDeletionRequest{
		ID:                 s.nextID,
		UserID:             req.UserID,
		RequesterAuthKeyID: req.RequesterAuthKeyID,
		State:              domain.AccountDeletionPending,
		Reason:             req.Reason,
		ConfirmHashDigest:  req.ConfirmHashDigest,
		RequestedAt:        req.RequestedAt,
		ExecuteAt:          req.ExecuteAt,
	}
	s.nextID++
	s.pending[req.UserID] = del
	return del, true, nil
}

func (s *AccountLifecycleStore) PendingAccountDeletionByHash(ctx context.Context, userID int64, digest [32]byte) (domain.AccountDeletionRequest, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	del, ok := s.pending[userID]
	if !ok || del.ConfirmHashDigest != digest || del.State != domain.AccountDeletionPending {
		return domain.AccountDeletionRequest{}, false, nil
	}
	return del, true, nil
}

func (s *AccountLifecycleStore) ExecuteAccountDeletion(ctx context.Context, userID int64, source domain.AccountDeletionSource, reason string, now time.Time) (domain.AccountDeletionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.pending, userID)
	u, _, _ := s.users.ByID(ctx, userID)
	return domain.AccountDeletionResult{
		User:    u,
		Changed: true,
	}, nil
}

func (s *AccountLifecycleStore) CancelAccountDeletion(ctx context.Context, userID int64, digest [32]byte, now time.Time) ([]domain.Authorization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.pending, userID)
	return nil, nil
}

func (s *AccountLifecycleStore) DueAccountDeletions(ctx context.Context, now time.Time, limit int) ([]domain.AccountDeletionCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var res []domain.AccountDeletionCandidate
	for id, d := range s.pending {
		if !d.ExecuteAt.After(now) && d.State == domain.AccountDeletionPending {
			res = append(res, domain.AccountDeletionCandidate{
				UserID: id,
				Source: domain.AccountDeletionManual,
				DueAt:  d.ExecuteAt,
			})
			if limit > 0 && len(res) >= limit {
				break
			}
		}
	}
	return res, nil
}
