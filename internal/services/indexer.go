package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/tmedia"

	"tdlibgo/internal/logger"
	"tdlibgo/internal/models"
	"tdlibgo/internal/state"
	"tdlibgo/internal/storage"
)

// IndexingStatus represents the state of the active indexing crawl.
type IndexingStatus struct {
	IsIndexing       bool      `json:"is_indexing"`
	ChatID           int64     `json:"chat_id"`
	ChatTitle        string    `json:"chat_title"`
	IndexedCount     int       `json:"indexed_count"`
	TotalCount       int       `json:"total_count"`
	CurrentMessageID int       `json:"current_message_id"`
	StartedAt        time.Time `json:"started_at"`
	Status           string    `json:"status"` // "idle", "indexing", "completed", "cancelled", "error"
	Error            string    `json:"error,omitempty"`
}

// CacheStats represents on-disk cache metrics and message index counts.
type CacheStats struct {
	MediaCacheBytes     int64  `json:"media_cache_bytes"`
	MediaCacheFormatted string `json:"media_cache_formatted"`
	MediaCacheFiles     int    `json:"media_cache_files"`
	AvatarCacheFiles    int    `json:"avatar_cache_files"`
	HistoryDBBytes      int64  `json:"history_db_bytes"`
	HistoryDBFormatted  string `json:"history_db_formatted"`
	CachedMessagesCount int    `json:"cached_messages_count"`
	CachedChatsCount    int    `json:"cached_chats_count"`
	CacheDir            string `json:"cache_dir"`
}

// IndexerService indexes messages/media into local storage, performs search, exports JSON, and manages disk cache.
type IndexerService struct {
	mainAPI    *tg.Client
	historyDB  *storage.HistoryDB
	state      *state.StateManager
	mediaDir   string
	avatarDir  string
	dbPath     string
	mu         sync.RWMutex
	status     IndexingStatus
	cancelFunc context.CancelFunc
}

// NewIndexerService creates a new indexing and cache management service.
func NewIndexerService(
	mainAPI *tg.Client,
	hdb *storage.HistoryDB,
	s *state.StateManager,
	mediaDir string,
	avatarDir string,
	dbPath string,
) *IndexerService {
	if mediaDir == "" {
		mediaDir = filepath.Join("session", "media_cache")
	}
	if avatarDir == "" {
		avatarDir = filepath.Join("session", "avatars")
	}
	if dbPath == "" {
		dbPath = filepath.Join("session", "history.db")
	}

	_ = os.MkdirAll(mediaDir, 0755)
	_ = os.MkdirAll(avatarDir, 0755)

	return &IndexerService{
		mainAPI:   mainAPI,
		historyDB: hdb,
		state:     s,
		mediaDir:  mediaDir,
		avatarDir: avatarDir,
		dbPath:    dbPath,
		status: IndexingStatus{
			Status: "idle",
		},
	}
}

// StartChatIndexing triggers a background crawl of a chat to index all messages and media metadata.
func (s *IndexerService) StartChatIndexing(
	chatID int64,
	chatTitle string,
	limit int,
	peer tg.InputPeerClass,
	peerFunc func(ctx context.Context, chatID int64) (tg.InputPeerClass, error),
) error {
	if s.mainAPI == nil {
		return errors.New("telegram API not ready")
	}

	s.mu.Lock()
	if s.status.IsIndexing {
		s.mu.Unlock()
		return errors.New("an indexing operation is already in progress")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFunc = cancel
	s.status = IndexingStatus{
		IsIndexing:   true,
		ChatID:       chatID,
		ChatTitle:    chatTitle,
		IndexedCount: 0,
		TotalCount:   limit,
		StartedAt:    time.Now(),
		Status:       "indexing",
	}
	s.mu.Unlock()

	s.broadcastStatus()

	go s.crawlChat(ctx, chatID, chatTitle, limit, peer)
	return nil
}

// crawlChat performs the paging traversal and saves all messages to historyDB.
func (s *IndexerService) crawlChat(
	ctx context.Context,
	chatID int64,
	chatTitle string,
	limit int,
	peer tg.InputPeerClass,
) {
	defer func() {
		s.mu.Lock()
		s.status.IsIndexing = false
		if s.status.Status == "indexing" {
			s.status.Status = "completed"
		}
		s.mu.Unlock()
		s.broadcastStatus()
	}()

	if limit <= 0 {
		limit = 1000 // default crawl depth
	}

	offsetID := 0
	batchSize := 100
	totalIndexed := 0

	for totalIndexed < limit {
		if ctx.Err() != nil {
			s.mu.Lock()
			s.status.Status = "cancelled"
			s.mu.Unlock()
			return
		}

		currentBatchSize := batchSize
		if totalIndexed+currentBatchSize > limit {
			currentBatchSize = limit - totalIndexed
		}

		historyReq := &tg.MessagesGetHistoryRequest{
			Peer:     peer,
			OffsetID: offsetID,
			Limit:    currentBatchSize,
		}

		res, err := s.mainAPI.MessagesGetHistory(ctx, historyReq)
		if err != nil {
			s.mu.Lock()
			s.status.Status = "error"
			s.status.Error = fmt.Sprintf("GetHistory: %v", err)
			s.mu.Unlock()
			logger.Error("INDEXER", "Crawl failed for chat %d: %v", chatID, err)
			return
		}

		var messages []tg.MessageClass
		var users []tg.UserClass
		var chats []tg.ChatClass

		switch h := res.(type) {
		case *tg.MessagesMessages:
			messages = h.Messages
			users = h.Users
			chats = h.Chats
		case *tg.MessagesMessagesSlice:
			messages = h.Messages
			users = h.Users
			chats = h.Chats
			if s.status.TotalCount < h.Count && limit > h.Count {
				s.status.TotalCount = h.Count
			}
		case *tg.MessagesChannelMessages:
			messages = h.Messages
			users = h.Users
			chats = h.Chats
			if s.status.TotalCount < h.Count && limit > h.Count {
				s.status.TotalCount = h.Count
			}
		}

		if len(messages) == 0 {
			break // No more history
		}

		// User lookup map
		userMap := make(map[int64]string)
		for _, u := range users {
			if user, ok := u.(*tg.User); ok {
				name := strings.TrimSpace(user.FirstName + " " + user.LastName)
				if name == "" {
					name = user.Username
				}
				userMap[user.ID] = name
			}
		}
		for _, c := range chats {
			switch chat := c.(type) {
			case *tg.Chat:
				userMap[chat.ID] = chat.Title
			case *tg.Channel:
				userMap[chat.ID] = chat.Title
			}
		}

		var modelsBatch []*models.Message
		minID := offsetID

		for _, m := range messages {
			msg, ok := m.(*tg.Message)
			if !ok {
				continue
			}

			if minID == 0 || msg.ID < minID {
				minID = msg.ID
			}

			senderName := "Unknown"
			if msg.FromID != nil {
				switch p := msg.FromID.(type) {
				case *tg.PeerUser:
					if n, exists := userMap[p.UserID]; exists {
						senderName = n
					}
				case *tg.PeerChannel:
					if n, exists := userMap[p.ChannelID]; exists {
						senderName = n
					}
				}
			}

			modelMsg := &models.Message{
				ID:         msg.ID,
				ChatID:     chatID,
				SenderName: senderName,
				Text:       msg.Message,
				Date:       time.Unix(int64(msg.Date), 0),
				Out:        msg.Out,
			}

			// Extract media details if present
			if media, hasMedia := tmedia.GetMedia(msg); hasMedia && media != nil {
				mediaURL := fmt.Sprintf("/api/media?chat_id=%d&message_id=%d", chatID, msg.ID)
				mediaType := "document"
				nameLower := strings.ToLower(media.Name)
				if strings.HasSuffix(nameLower, ".jpg") || strings.HasSuffix(nameLower, ".jpeg") || strings.HasSuffix(nameLower, ".png") {
					mediaType = "photo"
				} else if strings.HasSuffix(nameLower, ".mp4") || strings.HasSuffix(nameLower, ".mkv") {
					mediaType = "video"
				} else if strings.HasSuffix(nameLower, ".mp3") || strings.HasSuffix(nameLower, ".ogg") {
					mediaType = "audio"
				}

				modelMsg.Media = &models.MessageMedia{
					Type:     mediaType,
					URL:      mediaURL,
					FileName: media.Name,
					FileSize: media.Size,
				}
			}

			modelsBatch = append(modelsBatch, modelMsg)
		}

		// Persist batch into HistoryDB
		if s.historyDB != nil && len(modelsBatch) > 0 {
			if err := s.historyDB.SaveMessagesBatch(chatID, modelsBatch); err != nil {
				logger.Error("INDEXER", "Failed saving batch to historyDB: %v", err)
			}
		}

		totalIndexed += len(messages)
		offsetID = minID

		s.mu.Lock()
		s.status.IndexedCount = totalIndexed
		s.status.CurrentMessageID = offsetID
		s.mu.Unlock()

		s.broadcastStatus()

		// Brief throttle to be gentle on MTProto RPC flood waits
		time.Sleep(200 * time.Millisecond)
	}
}

// CancelIndexing cancels the active crawling operation.
func (s *IndexerService) CancelIndexing() {
	s.mu.Lock()
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	s.status.Status = "cancelled"
	s.status.IsIndexing = false
	s.mu.Unlock()

	s.broadcastStatus()
}

// GetStatus returns the current indexing status.
func (s *IndexerService) GetStatus() IndexingStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *IndexerService) broadcastStatus() {
	if s.state != nil {
		s.state.Broadcast(models.WSMessage{
			Type:    "indexer_progress",
			Payload: s.GetStatus(),
		})
	}
}

// Search queries the locally indexed messages and media files.
func (s *IndexerService) Search(query string, chatID int64, mediaOnly bool, limit int) ([]*models.Message, error) {
	if s.historyDB == nil {
		return nil, errors.New("database not available")
	}
	return s.historyDB.SearchMessages(query, chatID, mediaOnly, limit)
}

// ExportChat produces a JSON export of all indexed messages and media for a chat (matching tdl chat export).
func (s *IndexerService) ExportChat(chatID int64) ([]byte, error) {
	if s.historyDB == nil {
		return nil, errors.New("database not available")
	}

	msgs, err := s.historyDB.GetAllMessagesForChat(chatID)
	if err != nil {
		return nil, err
	}

	exportData := struct {
		ChatID     int64             `json:"chat_id"`
		ExportedAt time.Time         `json:"exported_at"`
		TotalCount int               `json:"total_count"`
		Messages   []*models.Message `json:"messages"`
	}{
		ChatID:     chatID,
		ExportedAt: time.Now(),
		TotalCount: len(msgs),
		Messages:   msgs,
	}

	return json.MarshalIndent(exportData, "", "  ")
}

// GetCacheStats computes disk sizes and counts for all cached media, avatars, and db.
func (s *IndexerService) GetCacheStats() CacheStats {
	var mediaBytes int64
	var mediaFiles int
	var avatarFiles int

	// Walk media dir
	_ = filepath.Walk(s.mediaDir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			mediaBytes += info.Size()
			mediaFiles++
		}
		return nil
	})

	// Walk avatar dir
	_ = filepath.Walk(s.avatarDir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			avatarFiles++
		}
		return nil
	})

	var dbBytes int64
	var chatsCount, msgsCount int
	if s.historyDB != nil {
		chatsCount, msgsCount, dbBytes, _ = s.historyDB.GetStats()
	}

	return CacheStats{
		MediaCacheBytes:     mediaBytes,
		MediaCacheFormatted: formatBytes(mediaBytes),
		MediaCacheFiles:     mediaFiles,
		AvatarCacheFiles:    avatarFiles,
		HistoryDBBytes:      dbBytes,
		HistoryDBFormatted:  formatBytes(dbBytes),
		CachedMessagesCount: msgsCount,
		CachedChatsCount:    chatsCount,
		CacheDir:            s.mediaDir,
	}
}

// ClearCache removes media cache files and optionally resets indexed messages.
func (s *IndexerService) ClearCache(clearMedia bool, clearHistory bool) error {
	if clearMedia {
		// Remove all cached media files
		entries, err := os.ReadDir(s.mediaDir)
		if err == nil {
			for _, entry := range entries {
				_ = os.Remove(filepath.Join(s.mediaDir, entry.Name()))
			}
		}
	}

	if clearHistory && s.historyDB != nil {
		if err := s.historyDB.ClearAllMessages(); err != nil {
			return err
		}
	}

	logger.Info("CACHE", "Cache cleared (clearMedia=%v, clearHistory=%v)", clearMedia, clearHistory)
	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
