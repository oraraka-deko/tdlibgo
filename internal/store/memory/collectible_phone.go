package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"tdlibgo/internal/domain"
	"tdlibgo/internal/store"
)

// CollectiblePhoneStore provides an in-memory implementation of store.CollectiblePhoneStore.
type CollectiblePhoneStore struct {
	mu        sync.RWMutex
	byPhone   map[string]domain.CollectiblePhone
	byID      map[int64]domain.CollectiblePhone
	transfers map[int64][]domain.CollectiblePhoneTransfer
	nextID    int64
}

var _ store.CollectiblePhoneStore = (*CollectiblePhoneStore)(nil)

// NewCollectiblePhoneStore creates a new in-memory CollectiblePhoneStore.
func NewCollectiblePhoneStore() *CollectiblePhoneStore {
	return &CollectiblePhoneStore{
		byPhone:   make(map[string]domain.CollectiblePhone),
		byID:      make(map[int64]domain.CollectiblePhone),
		transfers: make(map[int64][]domain.CollectiblePhoneTransfer),
		nextID:    1,
	}
}

func (s *CollectiblePhoneStore) MintCollectiblePhone(ctx context.Context, req domain.MintCollectiblePhoneRequest) (domain.CollectiblePhone, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.byPhone[req.Phone]; exists {
		return domain.CollectiblePhone{}, false, nil
	}

	phone := domain.CollectiblePhone{
		ID:             s.nextID,
		Phone:          req.Phone,
		Tier:           req.Tier,
		Status:         domain.CollectibleUsernameStatusOwned,
		OwnerUserID:    req.OwnerUserID,
		PurchaseDate:   req.PurchaseDate,
		Currency:       req.Currency,
		Amount:         req.Amount,
		CryptoCurrency: req.CryptoCurrency,
		CryptoAmount:   req.CryptoAmount,
		URL:            req.URL,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	s.nextID++
	s.byPhone[req.Phone] = phone
	s.byID[phone.ID] = phone
	return phone, true, nil
}

func (s *CollectiblePhoneStore) UpdateCollectiblePhonePrice(ctx context.Context, req domain.UpdateCollectiblePhonePriceRequest) (domain.CollectiblePhone, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.byPhone[req.Phone]
	if !ok {
		return domain.CollectiblePhone{}, false, nil
	}
	p.Currency = req.Currency
	p.Amount = req.Amount
	p.CryptoCurrency = req.CryptoCurrency
	p.CryptoAmount = req.CryptoAmount
	p.UpdatedAt = time.Now()
	s.byPhone[req.Phone] = p
	s.byID[p.ID] = p
	return p, true, nil
}

func (s *CollectiblePhoneStore) TransferCollectiblePhone(ctx context.Context, req domain.TransferCollectiblePhoneRequest) (domain.CollectiblePhone, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.byPhone[req.Phone]
	if !ok {
		return domain.CollectiblePhone{}, false, nil
	}
	prevOwner := p.OwnerUserID
	p.OwnerUserID = req.ToUserID
	p.TransferCount++
	p.UpdatedAt = time.Now()
	s.byPhone[req.Phone] = p
	s.byID[p.ID] = p

	s.transfers[p.ID] = append(s.transfers[p.ID], domain.CollectiblePhoneTransfer{
		ID:            int64(len(s.transfers[p.ID]) + 1),
		CollectibleID: p.ID,
		FromUserID:    prevOwner,
		ToUserID:      req.ToUserID,
		Actor:         req.Actor,
		Reason:        req.Reason,
		CommandKey:    req.CommandKey,
		CreatedAt:     time.Now(),
	})

	return p, true, nil
}

func (s *CollectiblePhoneStore) RevokeCollectiblePhone(ctx context.Context, req domain.RevokeCollectiblePhoneRequest) (domain.CollectiblePhone, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.byPhone[req.Phone]
	if !ok {
		return domain.CollectiblePhone{}, false, nil
	}
	p.OwnerUserID = 0
	p.Status = domain.CollectibleUsernameStatusVault
	p.UpdatedAt = time.Now()
	s.byPhone[req.Phone] = p
	s.byID[p.ID] = p
	return p, true, nil
}

func (s *CollectiblePhoneStore) DeleteCollectiblePhone(ctx context.Context, req domain.DeleteCollectiblePhoneRequest) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.byPhone[req.Phone]
	if !ok {
		return false, nil
	}
	delete(s.byPhone, req.Phone)
	delete(s.byID, p.ID)
	return true, nil
}

func (s *CollectiblePhoneStore) CollectiblePhone(ctx context.Context, phone string) (domain.CollectiblePhone, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.byPhone[phone]
	if !ok {
		return domain.CollectiblePhone{}, fmt.Errorf("collectible phone %q not found", phone)
	}
	return p, nil
}

func (s *CollectiblePhoneStore) CollectiblePhoneByID(ctx context.Context, id int64) (domain.CollectiblePhone, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.byID[id]
	if !ok {
		return domain.CollectiblePhone{}, fmt.Errorf("collectible phone id %d not found", id)
	}
	return p, nil
}

func (s *CollectiblePhoneStore) OwnedCollectiblePhones(ctx context.Context, ownerIDs []int64) (map[int64]domain.CollectiblePhone, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	targets := make(map[int64]struct{}, len(ownerIDs))
	for _, id := range ownerIDs {
		targets[id] = struct{}{}
	}

	res := make(map[int64]domain.CollectiblePhone)
	for _, p := range s.byPhone {
		if _, ok := targets[p.OwnerUserID]; ok {
			res[p.OwnerUserID] = p
		}
	}
	return res, nil
}

func (s *CollectiblePhoneStore) ListCollectiblePhones(ctx context.Context, filter domain.CollectiblePhoneFilter) ([]domain.CollectiblePhone, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []domain.CollectiblePhone
	for _, p := range s.byPhone {
		if filter.OwnerUserID > 0 && p.OwnerUserID != filter.OwnerUserID {
			continue
		}
		list = append(list, p)
	}
	return list, nil
}

func (s *CollectiblePhoneStore) CollectiblePhoneTransfers(ctx context.Context, id int64, limit int) ([]domain.CollectiblePhoneTransfer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := s.transfers[id]
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}
