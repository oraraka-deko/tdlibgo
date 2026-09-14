package state

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"tdlibgo/internal/logger"
	"tdlibgo/internal/models"
	"tdlibgo/internal/storage"
)

// StateManager is the thread-safe centralized in-memory state store.
type StateManager struct {
	mu sync.RWMutex

	auth models.AuthState
	conn models.ConnectionStateType
	user *models.UserProfile

	chats     map[int64]*models.Chat
	messages  map[int64][]*models.Message // chatID -> messages sorted by ID ascending
	entities  map[int64]*models.EntityInfo
	historyDB *storage.HistoryDB

	// WebSocket broadcast subscribers
	subMu sync.Mutex
	subs  map[chan models.WSMessage]struct{}
}

// NewStateManager creates a new initialized StateManager.
func NewStateManager(defaultPhone string) *StateManager {
	return &StateManager{
		auth: models.AuthState{
			State: models.AuthStateIdle,
			Phone: defaultPhone,
		},
		conn:     models.ConnDisconnected,
		chats:    make(map[int64]*models.Chat),
		messages: make(map[int64][]*models.Message),
		entities: make(map[int64]*models.EntityInfo),
		subs:     make(map[chan models.WSMessage]struct{}),
	}
}

// Subscribe registers a new subscriber channel for real-time events.
func (s *StateManager) Subscribe() chan models.WSMessage {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	ch := make(chan models.WSMessage, 64)
	s.subs[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber channel.
func (s *StateManager) Unsubscribe(ch chan models.WSMessage) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	delete(s.subs, ch)
	close(ch)
}

// Broadcast sends a WSMessage to all active subscribers.
func (s *StateManager) Broadcast(msg models.WSMessage) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- msg:
		default:
			// Slow subscriber, skip to avoid stalling others
		}
	}
}

// SetAuthState updates authentication state and broadcasts the change.
func (s *StateManager) SetAuthState(st models.AuthStateType, errStr string, codeType string, timeout int) {
	s.mu.Lock()
	s.auth.State = st
	s.auth.Error = errStr
	if codeType != "" {
		s.auth.CodeType = codeType
	}
	if timeout > 0 {
		s.auth.Timeout = timeout
	}
	s.auth.IsLoggedIn = (st == models.AuthStateReady)
	current := s.auth
	s.mu.Unlock()

	s.Broadcast(models.WSMessage{
		Type:    "auth_state",
		Payload: current,
	})
}

// SetPhone updates the phone number in auth state.
func (s *StateManager) SetPhone(phone string) {
	s.mu.Lock()
	s.auth.Phone = phone
	current := s.auth
	s.mu.Unlock()

	s.Broadcast(models.WSMessage{
		Type:    "auth_state",
		Payload: current,
	})
}

// GetAuthState returns a copy of the current AuthState.
func (s *StateManager) GetAuthState() models.AuthState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.auth
}

// SetConnectionState updates the connection state and broadcasts.
func (s *StateManager) SetConnectionState(cs models.ConnectionStateType) {
	s.mu.Lock()
	s.conn = cs
	s.mu.Unlock()

	s.Broadcast(models.WSMessage{
		Type:    "connection_state",
		Payload: cs,
	})
}

// SetUser updates the current authenticated user profile.
func (s *StateManager) SetUser(u *models.UserProfile) {
	s.mu.Lock()
	s.user = u
	s.mu.Unlock()

	s.Broadcast(models.WSMessage{
		Type:    "user_profile",
		Payload: u,
	})
}

// GetUser returns the current authenticated user profile.
func (s *StateManager) GetUser() *models.UserProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.user
}

// GetFullState returns FullState snapshot for /api/state.
func (s *StateManager) GetFullState() models.FullState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return models.FullState{
		Auth:       s.auth,
		Connection: s.conn,
		User:       s.user,
		ChatsCount: len(s.chats),
	}
}

// SetHistoryDB attaches persistent storage and preloads cached chats into memory.
func (s *StateManager) SetHistoryDB(db *storage.HistoryDB) {
	s.mu.Lock()
	s.historyDB = db
	s.mu.Unlock()

	if db == nil {
		return
	}

	// Preload all cached chats from DB
	if chats, err := db.GetAllChats(); err == nil && len(chats) > 0 {
		s.mu.Lock()
		for _, c := range chats {
			if _, exists := s.chats[c.ID]; !exists {
				s.chats[c.ID] = c
			}
		}
		s.mu.Unlock()
		logger.DB("Preloaded %d chats from persistent HistoryDB", len(chats))
	}
}

// GetHistoryDB returns the active persistent database instance.
func (s *StateManager) GetHistoryDB() *storage.HistoryDB {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.historyDB
}

// UpsertEntity stores/updates peer entity information.
func (s *StateManager) UpsertEntity(info *models.EntityInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entities[info.ID] = info
}

// GetEntity retrieves cached entity information.
func (s *StateManager) GetEntity(id int64) (*models.EntityInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ent, ok := s.entities[id]
	return ent, ok
}

// UpsertChat adds or updates a chat.
func (s *StateManager) UpsertChat(c *models.Chat) {
	s.mu.Lock()
	existing, exists := s.chats[c.ID]
	if exists {
		if c.Title != "" {
			existing.Title = c.Title
		}
		if c.Username != "" {
			existing.Username = c.Username
		}
		if c.TopMessageText != "" {
			existing.TopMessageText = c.TopMessageText
			existing.TopMessageID = c.TopMessageID
			existing.TopMessageSender = c.TopMessageSender
			existing.LastMessageDate = c.LastMessageDate
		}
		if c.UnreadCount >= 0 {
			existing.UnreadCount = c.UnreadCount
		}
		if c.AccessHash != 0 {
			existing.AccessHash = c.AccessHash
		}
		if c.PhotoURL != "" {
			existing.PhotoURL = c.PhotoURL
		}
		if c.PhotoID != 0 {
			existing.PhotoID = c.PhotoID
		}
		if c.StrippedThumb != "" {
			existing.StrippedThumb = c.StrippedThumb
		}
		if c.MembersCount > 0 {
			existing.MembersCount = c.MembersCount
		}
		existing.NoForwards = c.NoForwards
		existing.IsMuted = c.IsMuted
		existing.Pinned = c.Pinned
		existing.FolderID = c.FolderID
		c = existing
	} else {
		s.chats[c.ID] = c
	}
	db := s.historyDB
	s.mu.Unlock()

	if db != nil {
		_ = db.SaveChat(c)
	}

	s.Broadcast(models.WSMessage{
		Type:    "chat_updated",
		Payload: c,
	})
}

// GetChat retrieves a specific chat by ID.
func (s *StateManager) GetChat(id int64) (*models.Chat, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.chats[id]
	if !ok && id < 0 {
		sID := fmt.Sprintf("%d", id)
		if strings.HasPrefix(sID, "-100") {
			var norm int64
			if _, err := fmt.Sscanf(sID, "-100%d", &norm); err == nil {
				c, ok = s.chats[norm]
			}
		}
	}
	return c, ok
}

// GetChats returns a sorted slice of chats (pinned first, then by last message date desc).
func (s *StateManager) GetChats(folderID int, filterType string) []*models.Chat {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]*models.Chat, 0, len(s.chats))
	for _, c := range s.chats {
		if folderID != -1 && c.FolderID != folderID {
			continue
		}
		if filterType != "" && filterType != "all" {
			switch filterType {
			case "users", "private":
				if c.Type != models.ChatTypeUser {
					continue
				}
			case "groups":
				if c.Type != models.ChatTypeGroup {
					continue
				}
			case "channels":
				if c.Type != models.ChatTypeChannel {
					continue
				}
			case "bots":
				if c.Type != models.ChatTypeBot {
					continue
				}
			}
		}
		res = append(res, c)
	}

	sort.Slice(res, func(i, j int) bool {
		if res[i].Pinned != res[j].Pinned {
			return res[i].Pinned
		}
		return res[i].LastMessageDate.After(res[j].LastMessageDate)
	})

	return res
}

// AppendHistoricalMessages bulk-inserts historical messages without broadcasting live new_message events.
func (s *StateManager) AppendHistoricalMessages(chatID int64, msgs []*models.Message) {
	if len(msgs) == 0 {
		return
	}
	s.mu.Lock()

	history := s.messages[chatID]
	existingMap := make(map[int]int, len(history))
	for i, m := range history {
		existingMap[m.ID] = i
	}

	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if idx, found := existingMap[msg.ID]; found {
			history[idx] = msg
		} else {
			history = append(history, msg)
			existingMap[msg.ID] = len(history) - 1
		}
	}

	sort.Slice(history, func(i, j int) bool {
		return history[i].ID < history[j].ID
	})

	s.messages[chatID] = history
	db := s.historyDB
	s.mu.Unlock()

	if db != nil {
		_ = db.SaveMessagesBatch(chatID, msgs)
	}
}

// AppendMessage appends a message to the chat history and updates the chat preview.
func (s *StateManager) AppendMessage(msg *models.Message) {
	s.mu.Lock()
	history := s.messages[msg.ChatID]
	idx := -1
	for i, m := range history {
		if m.ID == msg.ID {
			idx = i
			break
		}
	}
	if idx >= 0 {
		history[idx] = msg
	} else {
		history = append(history, msg)
		sort.Slice(history, func(i, j int) bool {
			return history[i].ID < history[j].ID
		})
	}
	s.messages[msg.ChatID] = history

	chat, exists := s.chats[msg.ChatID]
	if exists {
		if msg.Date.After(chat.LastMessageDate) || msg.ID >= chat.TopMessageID {
			chat.TopMessageID = msg.ID
			chat.TopMessageText = msg.Text
			chat.TopMessageSender = msg.SenderName
			chat.LastMessageDate = msg.Date
		}
		if !msg.Out {
			chat.UnreadCount++
		}
	}
	db := s.historyDB
	s.mu.Unlock()

	if db != nil {
		_ = db.SaveMessage(msg)
		if exists {
			_ = db.SaveChat(chat)
		}
	}

	s.Broadcast(models.WSMessage{
		Type:    "new_message",
		Payload: msg,
	})

	if exists {
		s.Broadcast(models.WSMessage{
			Type:    "chat_updated",
			Payload: chat,
		})
	}
}

// UpdateMessage updates an existing message.
func (s *StateManager) UpdateMessage(msg *models.Message) {
	s.mu.Lock()
	history := s.messages[msg.ChatID]
	found := false
	for i, m := range history {
		if m.ID == msg.ID {
			history[i] = msg
			found = true
			break
		}
	}
	s.mu.Unlock()

	if found {
		s.Broadcast(models.WSMessage{
			Type:    "edit_message",
			Payload: msg,
		})
	}
}

// UpdateMessageReactions updates reactions on an existing message and broadcasts.
func (s *StateManager) UpdateMessageReactions(chatID int64, msgID int, reactions []models.ReactionCount) {
	s.mu.Lock()
	history := s.messages[chatID]
	for _, m := range history {
		if m.ID == msgID {
			m.Reactions = reactions
			break
		}
	}
	s.mu.Unlock()

	s.Broadcast(models.WSMessage{
		Type: "message_reactions",
		Payload: map[string]interface{}{
			"chat_id":    chatID,
			"message_id": msgID,
			"reactions":  reactions,
		},
	})
}

// UpdateMessageTranscription updates transcription on a message and broadcasts.
func (s *StateManager) UpdateMessageTranscription(chatID int64, msgID int, text string) {
	s.mu.Lock()
	history := s.messages[chatID]
	var target *models.Message
	for _, m := range history {
		if m.ID == msgID {
			if m.Media != nil {
				m.Media.Transcription = text
			}
			target = m
			break
		}
	}
	s.mu.Unlock()

	s.Broadcast(models.WSMessage{
		Type: "message_transcription",
		Payload: map[string]interface{}{
			"chat_id":       chatID,
			"message_id":    msgID,
			"transcription": text,
		},
	})

	if target != nil {
		s.Broadcast(models.WSMessage{
			Type:    "edit_message",
			Payload: target,
		})
	}
}

// DeleteMessages removes message IDs from chat history.
func (s *StateManager) DeleteMessages(chatID int64, ids []int) {
	idMap := make(map[int]bool, len(ids))
	for _, id := range ids {
		idMap[id] = true
	}

	s.mu.Lock()
	history := s.messages[chatID]
	newHistory := make([]*models.Message, 0, len(history))
	for _, m := range history {
		if !idMap[m.ID] {
			newHistory = append(newHistory, m)
		}
	}
	s.messages[chatID] = newHistory
	s.mu.Unlock()

	s.Broadcast(models.WSMessage{
		Type: "delete_messages",
		Payload: map[string]interface{}{
			"chat_id":     chatID,
			"message_ids": ids,
		},
	})
}

// GetMessages returns a slice of messages with pagination support.
func (s *StateManager) GetMessages(chatID int64, limit int, offsetID int) []*models.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history := s.messages[chatID]
	if len(history) == 0 && chatID < 0 {
		sID := fmt.Sprintf("%d", chatID)
		if strings.HasPrefix(sID, "-100") {
			var norm int64
			if _, err := fmt.Sscanf(sID, "-100%d", &norm); err == nil {
				history = s.messages[norm]
			}
		}
	}
	if len(history) == 0 {
		// Fallback to local DB query if memory cache is cold
		if s.historyDB != nil {
			if dbMsgs, err := s.historyDB.GetMessages(chatID, limit, offsetID); err == nil && len(dbMsgs) > 0 {
				return dbMsgs
			}
		}
		return []*models.Message{}
	}

	endIdx := len(history)
	if offsetID > 0 {
		for i, m := range history {
			if m.ID == offsetID {
				endIdx = i
				break
			}
		}
	}

	startIdx := 0
	if limit > 0 && endIdx > limit {
		startIdx = endIdx - limit
	}

	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx > endIdx {
		startIdx = endIdx
	}

	res := make([]*models.Message, endIdx-startIdx)
	copy(res, history[startIdx:endIdx])

	// If fewer messages returned than requested and offset is needed, augment from DB
	if len(res) < limit && s.historyDB != nil {
		if dbMsgs, err := s.historyDB.GetMessages(chatID, limit, offsetID); err == nil && len(dbMsgs) > len(res) {
			return dbMsgs
		}
	}

	return res
}

// SetTyping marks user as typing in a chat and notifies clients.
func (s *StateManager) SetTyping(chatID int64, userName string) {
	s.mu.Lock()
	if c, ok := s.chats[chatID]; ok {
		c.TypingUser = userName
	}
	s.mu.Unlock()

	s.Broadcast(models.WSMessage{
		Type: "user_typing",
		Payload: map[string]interface{}{
			"chat_id":   chatID,
			"user_name": userName,
		},
	})

	go func() {
		time.Sleep(4 * time.Second)
		s.mu.Lock()
		if c, ok := s.chats[chatID]; ok && c.TypingUser == userName {
			c.TypingUser = ""
		}
		s.mu.Unlock()

		s.Broadcast(models.WSMessage{
			Type: "user_typing",
			Payload: map[string]interface{}{
				"chat_id":   chatID,
				"user_name": "",
			},
		})
	}()
}

// SetUserStatus updates online status for a user peer.
func (s *StateManager) SetUserStatus(userID int64, isOnline bool) {
	s.mu.Lock()
	if c, ok := s.chats[userID]; ok {
		c.IsOnline = isOnline
	}
	s.mu.Unlock()

	s.Broadcast(models.WSMessage{
		Type: "user_status",
		Payload: map[string]interface{}{
			"user_id":   userID,
			"is_online": isOnline,
		},
	})
}
