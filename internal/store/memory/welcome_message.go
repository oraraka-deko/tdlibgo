package memory

import (
	"context"
	"fmt"
	"sync"

	"tdlibgo/internal/domain"
	"tdlibgo/internal/store"
)

// WelcomeMessageStore provides an in-memory implementation of store.WelcomeMessageStore.
type WelcomeMessageStore struct {
	mu       sync.RWMutex
	messages map[domain.Peer][]domain.WelcomeMessage
	nextID   int
}

var _ store.WelcomeMessageStore = (*WelcomeMessageStore)(nil)

// NewWelcomeMessageStore creates an in-memory WelcomeMessageStore.
func NewWelcomeMessageStore() *WelcomeMessageStore {
	return &WelcomeMessageStore{
		messages: make(map[domain.Peer][]domain.WelcomeMessage),
		nextID:   1,
	}
}

func (s *WelcomeMessageStore) CreateWelcomeMessage(ctx context.Context, req domain.CreateWelcomeMessageRequest) (domain.WelcomeMessage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg := domain.WelcomeMessage{
		ID:                s.nextID,
		Peer:              req.Peer,
		CreatorUserID:     req.CreatorUserID,
		Date:              req.Date,
		RandomID:          req.RandomID,
		Content:           req.Content,
		CreateFingerprint: req.CreateFingerprint,
		Version:           1,
	}
	s.nextID++
	s.messages[req.Peer] = append(s.messages[req.Peer], msg)
	return msg, true, nil
}

func (s *WelcomeMessageStore) EditWelcomeMessage(ctx context.Context, req domain.EditWelcomeMessageRequest) (domain.WelcomeMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.messages[req.Peer]
	for i, msg := range list {
		if msg.ID == req.ID {
			updated, err := req.Fields.Apply(msg.Content)
			if err != nil {
				return domain.WelcomeMessage{}, err
			}
			msg.Content = updated
			msg.EditDate = req.EditDate
			msg.Version++
			list[i] = msg
			s.messages[req.Peer] = list
			return msg, nil
		}
	}
	return domain.WelcomeMessage{}, fmt.Errorf("welcome message %d not found", req.ID)
}

func (s *WelcomeMessageStore) ListWelcomeMessages(ctx context.Context, peer domain.Peer, hash int64) (domain.WelcomeMessageList, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := s.messages[peer]
	return domain.WelcomeMessageList{
		Messages: append([]domain.WelcomeMessage(nil), list...),
	}, nil
}

func (s *WelcomeMessageStore) DeleteWelcomeMessage(ctx context.Context, peer domain.Peer, id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.messages[peer]
	for i, msg := range list {
		if msg.ID == id {
			s.messages[peer] = append(list[:i], list[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (s *WelcomeMessageStore) DeleteAllWelcomeMessages(ctx context.Context, peer domain.Peer) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.messages[peer]; ok {
		delete(s.messages, peer)
		return true, nil
	}
	return false, nil
}

func (s *WelcomeMessageStore) HasWelcomeMessages(ctx context.Context, peer domain.Peer) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := s.messages[peer]
	return len(list) > 0, nil
}
