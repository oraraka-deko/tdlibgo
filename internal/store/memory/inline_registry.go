package memory

import (
	"context"
	"sync"
	"time"

	"tdlibgo/internal/domain"
	"tdlibgo/internal/store"
)

// InlineRegistryStore provides an in-memory implementation of store.InlineRegistryStore.
type InlineRegistryStore struct {
	mu                  sync.RWMutex
	pending             map[int64]store.InlinePending
	results             map[int64]domain.BotInlineResults
	cache               map[store.InlineCacheKey]domain.BotInlineResults
	cacheExpire         map[store.InlineCacheKey]time.Time
	webdocs             map[store.InlineWebDocumentKey]store.InlineWebDocumentEntry
	prepMsgs            map[string]store.PreparedInlineMessage
	webviews            map[int64]store.WebViewSession
	webviewsByBotQuery  map[string]store.WebViewSession
	botQuerySubscribers []func(context.Context, store.BotInlineQueryPush)
}

var _ store.InlineRegistryStore = (*InlineRegistryStore)(nil)
var _ store.BotInlineQueryPushBroker = (*InlineRegistryStore)(nil)

// NewInlineRegistryStore creates an in-memory InlineRegistryStore.
func NewInlineRegistryStore() *InlineRegistryStore {
	return &InlineRegistryStore{
		pending:            make(map[int64]store.InlinePending),
		results:            make(map[int64]domain.BotInlineResults),
		cache:              make(map[store.InlineCacheKey]domain.BotInlineResults),
		cacheExpire:        make(map[store.InlineCacheKey]time.Time),
		webdocs:            make(map[store.InlineWebDocumentKey]store.InlineWebDocumentEntry),
		prepMsgs:           make(map[string]store.PreparedInlineMessage),
		webviews:           make(map[int64]store.WebViewSession),
		webviewsByBotQuery: make(map[string]store.WebViewSession),
	}
}

func (s *InlineRegistryStore) PutInlinePending(ctx context.Context, pending store.InlinePending, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pending[pending.QueryID] = pending
	if ttl > 0 {
		time.AfterFunc(ttl, func() {
			s.mu.Lock()
			delete(s.pending, pending.QueryID)
			s.mu.Unlock()
		})
	}
	return nil
}

func (s *InlineRegistryStore) GetInlinePending(ctx context.Context, queryID int64) (store.InlinePending, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.pending[queryID]
	return p, ok, nil
}

func (s *InlineRegistryStore) DeleteInlinePending(ctx context.Context, queryID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.pending, queryID)
	return nil
}

func (s *InlineRegistryStore) PutInlineResult(ctx context.Context, results domain.BotInlineResults, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.results[results.QueryID] = results
	if ttl > 0 {
		time.AfterFunc(ttl, func() {
			s.mu.Lock()
			delete(s.results, results.QueryID)
			s.mu.Unlock()
		})
	}
	return nil
}

func (s *InlineRegistryStore) GetInlineResult(ctx context.Context, queryID int64) (domain.BotInlineResults, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.results[queryID]
	return r, ok, nil
}

func (s *InlineRegistryStore) DeleteInlineResult(ctx context.Context, queryID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.results, queryID)
	return nil
}

func (s *InlineRegistryStore) PutInlineCache(ctx context.Context, key store.InlineCacheKey, results domain.BotInlineResults, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache[key] = results
	if ttl > 0 {
		exp := time.Now().Add(ttl)
		s.cacheExpire[key] = exp
		time.AfterFunc(ttl, func() {
			s.mu.Lock()
			delete(s.cache, key)
			delete(s.cacheExpire, key)
			s.mu.Unlock()
		})
	}
	return nil
}

func (s *InlineRegistryStore) GetInlineCache(ctx context.Context, key store.InlineCacheKey) (domain.BotInlineResults, bool, time.Duration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res, ok := s.cache[key]
	if !ok {
		return domain.BotInlineResults{}, false, 0, nil
	}
	exp, hasExp := s.cacheExpire[key]
	var ttl time.Duration
	if hasExp {
		ttl = time.Until(exp)
		if ttl <= 0 {
			return domain.BotInlineResults{}, false, 0, nil
		}
	}
	return res, true, ttl, nil
}

func (s *InlineRegistryStore) PutInlineWebDocument(ctx context.Context, document domain.BotInlineWebDocument, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := store.InlineWebDocumentKey{URL: document.URL, AccessHash: document.AccessHash}
	entry := s.webdocs[k]
	entry.Document = document
	s.webdocs[k] = entry
	return nil
}

func (s *InlineRegistryStore) GetInlineWebDocument(ctx context.Context, key store.InlineWebDocumentKey) (store.InlineWebDocumentEntry, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	doc, ok := s.webdocs[key]
	return doc, ok, nil
}

func (s *InlineRegistryStore) PutInlineWebDocumentBytes(ctx context.Context, key store.InlineWebDocumentKey, data []byte, mimeType string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.webdocs[key]
	entry.Bytes = data
	entry.MimeType = mimeType
	s.webdocs[key] = entry
	return nil
}

func (s *InlineRegistryStore) PutPreparedInlineMessage(ctx context.Context, msg store.PreparedInlineMessage, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.prepMsgs[msg.ID] = msg
	if ttl > 0 {
		time.AfterFunc(ttl, func() {
			s.mu.Lock()
			delete(s.prepMsgs, msg.ID)
			s.mu.Unlock()
		})
	}
	return nil
}

func (s *InlineRegistryStore) GetPreparedInlineMessage(ctx context.Context, id string) (store.PreparedInlineMessage, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msg, ok := s.prepMsgs[id]
	return msg, ok, nil
}

func (s *InlineRegistryStore) PutWebViewSession(ctx context.Context, session store.WebViewSession, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.webviews[session.QueryID] = session
	if session.BotQueryID != "" {
		s.webviewsByBotQuery[session.BotQueryID] = session
	}
	if ttl > 0 {
		time.AfterFunc(ttl, func() {
			s.mu.Lock()
			delete(s.webviews, session.QueryID)
			if session.BotQueryID != "" {
				delete(s.webviewsByBotQuery, session.BotQueryID)
			}
			s.mu.Unlock()
		})
	}
	return nil
}

func (s *InlineRegistryStore) GetWebViewSession(ctx context.Context, queryID int64) (store.WebViewSession, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.webviews[queryID]
	return sess, ok, nil
}

func (s *InlineRegistryStore) GetWebViewSessionByBotQuery(ctx context.Context, botQueryID string) (store.WebViewSession, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.webviewsByBotQuery[botQueryID]
	return sess, ok, nil
}

func (s *InlineRegistryStore) DeleteWebViewSession(ctx context.Context, queryID int64, botQueryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.webviews, queryID)
	if botQueryID != "" {
		delete(s.webviewsByBotQuery, botQueryID)
	}
	return nil
}

func (s *InlineRegistryStore) PublishBotInlineQuery(ctx context.Context, push store.BotInlineQueryPush) error {
	s.mu.RLock()
	subs := make([]func(context.Context, store.BotInlineQueryPush), len(s.botQuerySubscribers))
	copy(subs, s.botQuerySubscribers)
	s.mu.RUnlock()

	for _, sub := range subs {
		sub(ctx, push)
	}
	return nil
}

func (s *InlineRegistryStore) SubscribeBotInlineQueries(ctx context.Context, handle func(context.Context, store.BotInlineQueryPush)) error {
	s.mu.Lock()
	s.botQuerySubscribers = append(s.botQuerySubscribers, handle)
	s.mu.Unlock()
	return nil
}
