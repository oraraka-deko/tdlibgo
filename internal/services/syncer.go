package services

import (
	"context"
	"sync"
	"time"

	"tdlibgo/internal/logger"
	"tdlibgo/internal/models"
	"tdlibgo/internal/storage"
)

// SyncerService handles background keep-alive pinging, periodic dialog refreshes, and asynchronous chat history catchup.
type SyncerService struct {
	client      HistoryFetcher
	state       StateStore
	db          *storage.HistoryDB
	activeChat  int64
	syncedChats map[int64]int
	mu          sync.RWMutex
	stopChan    chan struct{}
	running     bool
}

// HistoryFetcher interface decoupling from ClientController.
type HistoryFetcher interface {
	FetchHistory(ctx context.Context, chatID int64, limit, offsetID int) error
	FetchDialogs(ctx context.Context) error
	Ping(ctx context.Context) error
}

// StateStore interface for updating in-memory models.
type StateStore interface {
	GetAuthState() models.AuthState
	GetChats(folderID int, filterType string) []*models.Chat
	GetChat(id int64) (*models.Chat, bool)
	UpsertChat(c *models.Chat)
	AppendHistoricalMessages(chatID int64, msgs []*models.Message)
	Broadcast(msg models.WSMessage)
}

// NewSyncerService creates an instance of the background syncer and keep-alive engine.
func NewSyncerService(client HistoryFetcher, s StateStore, db *storage.HistoryDB) *SyncerService {
	return &SyncerService{
		client:      client,
		state:       s,
		db:          db,
		syncedChats: make(map[int64]int),
		stopChan:    make(chan struct{}),
	}
}

// SetActiveChat tracks the chat currently open in the user interface.
func (s *SyncerService) SetActiveChat(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeChat = chatID
}

// Start launches the background keep-alive and sync loops.
func (s *SyncerService) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopChan = make(chan struct{})
	s.mu.Unlock()

	logger.Sync("Background Keep-Alive and History Syncer engine started.")

	// Keep-Alive Loop (Ticks every 25 seconds)
	go s.keepAliveLoop(ctx)

	// Dialog & History Catchup Loop (Ticks every 15 seconds)
	go s.historySyncLoop(ctx)
}

// Stop terminates the background loops cleanly.
func (s *SyncerService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		s.running = false
		close(s.stopChan)
		logger.Sync("Background Keep-Alive and History Syncer engine stopped.")
	}
}

func (s *SyncerService) keepAliveLoop(ctx context.Context) {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			if !s.state.GetAuthState().IsLoggedIn {
				continue
			}

			// Issue lightweight keep-alive ping to maintain active TCP session
			pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := s.client.Ping(pingCtx)
			cancel()

			if err != nil {
				logger.Warn("SYNC", "Keep-alive ping warning: %v", err)
			} else {
				logger.MTProto("Keep-alive heartbeat OK (TCP session warm)")
			}
		}
	}
}

func (s *SyncerService) historySyncLoop(ctx context.Context) {
	// Wait a moment after boot before starting the sync cycle
	time.Sleep(5 * time.Second)
	ticker := time.NewTicker(45 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			if !s.state.GetAuthState().IsLoggedIn {
				continue
			}

			// 1. Sync dialogs top messages
			dlgCtx, cancelDlg := context.WithTimeout(context.Background(), 25*time.Second)
			if err := s.client.FetchDialogs(dlgCtx); err != nil {
				logger.Warn("SYNC", "Background dialog sync: %v", err)
			}
			cancelDlg()

			// 2. Identify priority chats to sync
			s.mu.RLock()
			active := s.activeChat
			s.mu.RUnlock()

			var targetChats []int64
			if active != 0 {
				targetChats = append(targetChats, active)
			}

			// Pick top 3 recent chats as well
			chats := s.state.GetChats(-1, "all")
			for i, c := range chats {
				if i >= 3 {
					break
				}
				if c.ID != active {
					targetChats = append(targetChats, c.ID)
				}
			}

			// 3. Catch up message gaps in priority chats
			for _, chatID := range targetChats {
				chat, ok := s.state.GetChat(chatID)
				if !ok || chat.TopMessageID <= 0 {
					continue
				}

				s.mu.Lock()
				lastSyncedTopID := s.syncedChats[chatID]
				s.mu.Unlock()

				// If already caught up with current top message, skip to avoid spamming Telegram
				if chat.TopMessageID <= lastSyncedTopID {
					continue
				}

				// If local DB already contains top message or recent messages, mark as caught up
				latestDBID, _ := s.db.GetLatestMessageID(chatID)
				if latestDBID >= chat.TopMessageID {
					s.mu.Lock()
					s.syncedChats[chatID] = chat.TopMessageID
					s.mu.Unlock()
					continue
				}

				syncCtx, cancelSync := context.WithTimeout(context.Background(), 25*time.Second)
				err := s.client.FetchHistory(syncCtx, chatID, 50, 0)
				cancelSync()

				if err == nil {
					s.mu.Lock()
					s.syncedChats[chatID] = chat.TopMessageID
					s.mu.Unlock()
					logger.Sync("Background history caught up for chat %d (%s): topMsg=%d", chatID, chat.Title, chat.TopMessageID)
				}
				// Polite throttle between chats to avoid FLOOD_WAIT
				time.Sleep(1 * time.Second)
			}
		}
	}
}
