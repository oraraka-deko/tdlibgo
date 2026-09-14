package memory

import (
	"context"
	"sync"
	"time"

	"tdlibgo/internal/domain"
	"tdlibgo/internal/store"
)

type callbackKey struct {
	botUserID int64
	queryID   int64
}

// BotCallbackRegistryStore is an in-memory implementation of store.BotCallbackRegistryStore.
type BotCallbackRegistryStore struct {
	mu          sync.RWMutex
	pending     map[callbackKey]store.BotCallbackPending
	answers     map[callbackKey]domain.BotCallbackAnswer
	subscribers []func(context.Context, store.BotCallbackAnswerPush)
}

var _ store.BotCallbackRegistryStore = (*BotCallbackRegistryStore)(nil)

// NewBotCallbackRegistryStore creates a new in-memory BotCallbackRegistryStore.
func NewBotCallbackRegistryStore() *BotCallbackRegistryStore {
	return &BotCallbackRegistryStore{
		pending: make(map[callbackKey]store.BotCallbackPending),
		answers: make(map[callbackKey]domain.BotCallbackAnswer),
	}
}

func (s *BotCallbackRegistryStore) PutBotCallbackPending(ctx context.Context, pending store.BotCallbackPending, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := callbackKey{botUserID: pending.BotUserID, queryID: pending.QueryID}
	if _, exists := s.pending[k]; exists {
		return false, nil
	}
	s.pending[k] = pending

	if ttl > 0 {
		time.AfterFunc(ttl, func() {
			s.mu.Lock()
			delete(s.pending, k)
			s.mu.Unlock()
		})
	}
	return true, nil
}

func (s *BotCallbackRegistryStore) ResolveBotCallback(ctx context.Context, botUserID, queryID int64, answer domain.BotCallbackAnswer) (bool, error) {
	s.mu.Lock()
	k := callbackKey{botUserID: botUserID, queryID: queryID}
	if _, answered := s.answers[k]; answered {
		s.mu.Unlock()
		return false, nil
	}
	s.answers[k] = answer
	delete(s.pending, k)

	subs := make([]func(context.Context, store.BotCallbackAnswerPush), len(s.subscribers))
	copy(subs, s.subscribers)
	s.mu.Unlock()

	push := store.BotCallbackAnswerPush{
		QueryID:   queryID,
		BotUserID: botUserID,
		Answer:    answer,
	}
	for _, sub := range subs {
		sub(ctx, push)
	}
	return true, nil
}

func (s *BotCallbackRegistryStore) GetBotCallbackAnswer(ctx context.Context, botUserID, queryID int64) (domain.BotCallbackAnswer, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	k := callbackKey{botUserID: botUserID, queryID: queryID}
	ans, ok := s.answers[k]
	return ans, ok, nil
}

func (s *BotCallbackRegistryStore) DeleteBotCallbackPending(ctx context.Context, botUserID, queryID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := callbackKey{botUserID: botUserID, queryID: queryID}
	delete(s.pending, k)
	delete(s.answers, k)
	return nil
}

func (s *BotCallbackRegistryStore) SubscribeBotCallbackAnswers(ctx context.Context, handle func(context.Context, store.BotCallbackAnswerPush)) error {
	s.mu.Lock()
	s.subscribers = append(s.subscribers, handle)
	s.mu.Unlock()
	return nil
}
