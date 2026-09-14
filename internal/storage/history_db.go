package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"

	"tdlibgo/internal/logger"
	"tdlibgo/internal/models"
)

var (
	bucketChats          = []byte("chats")
	bucketMessages       = []byte("messages")
	bucketMeta           = []byte("meta")
	bucketTranscriptions = []byte("transcriptions")
)

// HistoryDB manages local persistent caching for chats and messages using BoltDB.
type HistoryDB struct {
	db *bbolt.DB
	mu sync.RWMutex
}

// OpenHistoryDB initializes or opens the persistent local history database.
func OpenHistoryDB(dbPath string) (*HistoryDB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create history db dir: %w", err)
	}

	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("open bbolt db: %w", err)
	}

	// Ensure all required buckets exist
	err = db.Update(func(tx *bbolt.Tx) error {
		for _, bName := range [][]byte{bucketChats, bucketMessages, bucketMeta, bucketTranscriptions} {
			if _, err := tx.CreateBucketIfNotExists(bName); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create buckets: %w", err)
	}

	logger.DB("HistoryDB initialized at %s", dbPath)
	return &HistoryDB{db: db}, nil
}

// Close safely shuts down the database.
func (h *HistoryDB) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.db != nil {
		return h.db.Close()
	}
	return nil
}

func msgKey(chatID int64, msgID int) []byte {
	return []byte(fmt.Sprintf("%020d_%010d", chatID, msgID))
}

func chatPrefix(chatID int64) []byte {
	return []byte(fmt.Sprintf("%020d_", chatID))
}

// SaveChat stores or updates a chat model.
func (h *HistoryDB) SaveChat(c *models.Chat) error {
	if c == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	return h.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketChats)
		return b.Put([]byte(fmt.Sprintf("%d", c.ID)), data)
	})
}

// SaveChats batch-stores multiple chats.
func (h *HistoryDB) SaveChats(chats []*models.Chat) error {
	if len(chats) == 0 {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketChats)
		for _, c := range chats {
			if c == nil {
				continue
			}
			data, err := json.Marshal(c)
			if err != nil {
				continue
			}
			if err := b.Put([]byte(fmt.Sprintf("%d", c.ID)), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetChat loads a chat by ID.
func (h *HistoryDB) GetChat(id int64) (*models.Chat, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var chat *models.Chat
	err := h.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketChats)
		data := b.Get([]byte(fmt.Sprintf("%d", id)))
		if data == nil {
			return nil
		}
		chat = &models.Chat{}
		return json.Unmarshal(data, chat)
	})
	return chat, err
}

// GetAllChats loads all persisted chats sorted by LastMessageDate desc.
func (h *HistoryDB) GetAllChats() ([]*models.Chat, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var chats []*models.Chat
	err := h.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketChats)
		return b.ForEach(func(k, v []byte) error {
			var c models.Chat
			if err := json.Unmarshal(v, &c); err == nil {
				chats = append(chats, &c)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(chats, func(i, j int) bool {
		if chats[i].Pinned != chats[j].Pinned {
			return chats[i].Pinned
		}
		return chats[i].LastMessageDate.After(chats[j].LastMessageDate)
	})
	return chats, nil
}

// SaveMessage stores a single message.
func (h *HistoryDB) SaveMessage(msg *models.Message) error {
	if msg == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return h.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketMessages)
		return b.Put(msgKey(msg.ChatID, msg.ID), data)
	})
}

// SaveMessagesBatch inserts or updates multiple messages in a single transaction.
func (h *HistoryDB) SaveMessagesBatch(chatID int64, msgs []*models.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketMessages)
		for _, m := range msgs {
			if m == nil {
				continue
			}
			data, err := json.Marshal(m)
			if err != nil {
				continue
			}
			if err := b.Put(msgKey(chatID, m.ID), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetMessages retrieves messages for a chat with pagination (offsetID and limit).
// Returns messages ordered by ID ascending.
func (h *HistoryDB) GetMessages(chatID int64, limit int, offsetID int) ([]*models.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	prefix := chatPrefix(chatID)
	var messages []*models.Message

	err := h.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketMessages)
		c := b.Cursor()

		if offsetID > 0 {
			// Seek to the offset message and iterate backwards for older messages
			startKey := msgKey(chatID, offsetID)
			k, _ := c.Seek(startKey)
			if k == nil || !bytes.HasPrefix(k, prefix) || bytes.Compare(k, startKey) > 0 {
				k, _ = c.Prev()
			} else {
				// We seeked to the exact offsetID or after; step back one to get older
				k, _ = c.Prev()
			}

			count := 0
			for ; k != nil && bytes.HasPrefix(k, prefix) && count < limit; k, _ = c.Prev() {
				v := b.Get(k)
				var m models.Message
				if err := json.Unmarshal(v, &m); err == nil {
					messages = append(messages, &m)
					count++
				}
			}
		} else {
			// Fetch the newest `limit` messages by seeking to the end of the chat prefix and scanning backwards
			nextPrefix := []byte(fmt.Sprintf("%020d_\xff", chatID))
			k, _ := c.Seek(nextPrefix)
			if k == nil || !bytes.HasPrefix(k, prefix) {
				k, _ = c.Prev()
			}

			count := 0
			for ; k != nil && bytes.HasPrefix(k, prefix) && count < limit; k, _ = c.Prev() {
				v := b.Get(k)
				var m models.Message
				if err := json.Unmarshal(v, &m); err == nil {
					messages = append(messages, &m)
					count++
				}
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Reverse to deliver sorted ascending (oldest to newest)
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetLatestMessageID returns the maximum message ID stored for a chat, or 0 if none.
func (h *HistoryDB) GetLatestMessageID(chatID int64) (int, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	prefix := chatPrefix(chatID)
	maxID := 0

	err := h.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketMessages)
		c := b.Cursor()
		nextPrefix := []byte(fmt.Sprintf("%020d_\xff", chatID))
		k, v := c.Seek(nextPrefix)
		if k == nil || !bytes.HasPrefix(k, prefix) {
			k, v = c.Prev()
		}
		if k != nil && bytes.HasPrefix(k, prefix) {
			var m models.Message
			if err := json.Unmarshal(v, &m); err == nil {
				maxID = m.ID
			}
		}
		return nil
	})

	return maxID, err
}

// CountMessages returns the number of messages cached locally for a chat.
func (h *HistoryDB) CountMessages(chatID int64) (int, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	prefix := chatPrefix(chatID)
	count := 0

	err := h.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketMessages)
		c := b.Cursor()
		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			count++
		}
		return nil
	})

	return count, err
}

// SearchMessages performs full text and media search across indexed messages.
func (h *HistoryDB) SearchMessages(query string, chatID int64, mediaOnly bool, limit int) ([]*models.Message, error) {
	if limit <= 0 {
		limit = 100
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	queryLower := strings.ToLower(strings.TrimSpace(query))
	var results []*models.Message

	err := h.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketMessages)
		c := b.Cursor()

		var prefix []byte
		if chatID != 0 {
			prefix = chatPrefix(chatID)
		}

		for k, v := c.First(); k != nil; k, v = c.Next() {
			if prefix != nil && !bytes.HasPrefix(k, prefix) {
				continue
			}

			var m models.Message
			if err := json.Unmarshal(v, &m); err != nil {
				continue
			}

			if mediaOnly && m.Media == nil {
				continue
			}

			if queryLower != "" {
				textMatch := strings.Contains(strings.ToLower(m.Text), queryLower)
				senderMatch := strings.Contains(strings.ToLower(m.SenderName), queryLower)
				mediaMatch := false
				if m.Media != nil {
					mediaMatch = strings.Contains(strings.ToLower(m.Media.FileName), queryLower) ||
						strings.Contains(strings.ToLower(m.Media.MimeType), queryLower)
				}

				if !textMatch && !senderMatch && !mediaMatch {
					continue
				}
			}

			results = append(results, &m)
			if len(results) >= limit {
				break
			}
		}
		return nil
	})

	return results, err
}

// GetAllMessagesForChat retrieves all messages stored for a specific chat, ordered by ID ascending.
func (h *HistoryDB) GetAllMessagesForChat(chatID int64) ([]*models.Message, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	prefix := chatPrefix(chatID)
	var messages []*models.Message

	err := h.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketMessages)
		c := b.Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var m models.Message
			if err := json.Unmarshal(v, &m); err == nil {
				messages = append(messages, &m)
			}
		}
		return nil
	})

	return messages, err
}

// GetStats returns database statistics: chat count, total messages count, and file size on disk.
func (h *HistoryDB) GetStats() (chatCount int, messageCount int, dbSize int64, err error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	err = h.db.View(func(tx *bbolt.Tx) error {
		bChats := tx.Bucket(bucketChats)
		if bChats != nil {
			chatCount = bChats.Stats().KeyN
		}
		bMsgs := tx.Bucket(bucketMessages)
		if bMsgs != nil {
			messageCount = bMsgs.Stats().KeyN
		}
		dbSize = tx.Size()
		return nil
	})

	return chatCount, messageCount, dbSize, err
}

// ClearChatMessages deletes all stored messages for a specific chat.
func (h *HistoryDB) ClearChatMessages(chatID int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	prefix := chatPrefix(chatID)
	return h.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketMessages)
		c := b.Cursor()
		var keysToDelete [][]byte
		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			keyCopy := make([]byte, len(k))
			copy(keyCopy, k)
			keysToDelete = append(keysToDelete, keyCopy)
		}
		for _, k := range keysToDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// ClearAllMessages purges all stored messages.
func (h *HistoryDB) ClearAllMessages() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.db.Update(func(tx *bbolt.Tx) error {
		if err := tx.DeleteBucket(bucketMessages); err != nil {
			return err
		}
		_, err := tx.CreateBucket(bucketMessages)
		return err
	})
}

// GetTranscription retrieves a saved voice/audio transcription for a message.
func (h *HistoryDB) GetTranscription(chatID int64, messageID int) (string, bool) {
	if h == nil || h.db == nil {
		return "", false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	key := []byte(fmt.Sprintf("%d:%d", chatID, messageID))
	var text string
	var found bool
	_ = h.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketTranscriptions)
		if b == nil {
			return nil
		}
		val := b.Get(key)
		if val != nil {
			text = string(val)
			found = true
		}
		return nil
	})
	return text, found
}

// SaveTranscription caches a voice/audio transcription for a message.
func (h *HistoryDB) SaveTranscription(chatID int64, messageID int, text string) error {
	if h == nil || h.db == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	key := []byte(fmt.Sprintf("%d:%d", chatID, messageID))
	return h.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketTranscriptions)
		if b == nil {
			var err error
			b, err = tx.CreateBucketIfNotExists(bucketTranscriptions)
			if err != nil {
				return err
			}
		}
		return b.Put(key, []byte(text))
	})
}

