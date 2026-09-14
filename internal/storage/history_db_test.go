package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"tdlibgo/internal/models"
)

func TestHistoryDB_ChatsAndMessages(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "historydb_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := OpenHistoryDB(dbPath)
	if err != nil {
		t.Fatalf("OpenHistoryDB failed: %v", err)
	}
	defer db.Close()

	// 1. Save and Get Chat
	chat := &models.Chat{
		ID:              123456789,
		Type:            models.ChatTypeUser,
		Title:           "Alice",
		TopMessageID:    10,
		TopMessageText:  "Hello world",
		LastMessageDate: time.Now(),
		UnreadCount:     2,
	}

	if err := db.SaveChat(chat); err != nil {
		t.Fatalf("SaveChat failed: %v", err)
	}

	fetchedChat, err := db.GetChat(123456789)
	if err != nil {
		t.Fatalf("GetChat failed: %v", err)
	}
	if fetchedChat.Title != "Alice" || fetchedChat.TopMessageText != "Hello world" {
		t.Fatalf("Fetched chat mismatch: %+v", fetchedChat)
	}

	// 2. Batch Insert Messages
	msgs := make([]*models.Message, 0, 5)
	for i := 1; i <= 5; i++ {
		msgs = append(msgs, &models.Message{
			ID:         i,
			ChatID:     123456789,
			SenderID:   123456789,
			SenderName: "Alice",
			Text:       "Message " + string(rune('0'+i)),
			Date:       time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	if err := db.SaveMessagesBatch(123456789, msgs); err != nil {
		t.Fatalf("SaveMessagesBatch failed: %v", err)
	}

	// 3. Count Messages
	count, err := db.CountMessages(123456789)
	if err != nil {
		t.Fatalf("CountMessages failed: %v", err)
	}
	if count != 5 {
		t.Fatalf("Expected 5 messages, got %d", count)
	}

	// 4. Get Latest Message ID
	latestID, err := db.GetLatestMessageID(123456789)
	if err != nil {
		t.Fatalf("GetLatestMessageID failed: %v", err)
	}
	if latestID != 5 {
		t.Fatalf("Expected latest message ID 5, got %d", latestID)
	}

	// 5. Paginated Messages
	page, err := db.GetMessages(123456789, 3, 0)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(page) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(page))
	}
	// Verify ascending order
	if page[0].ID > page[len(page)-1].ID {
		t.Fatalf("Expected ascending order in page: %d vs %d", page[0].ID, page[len(page)-1].ID)
	}

	// 6. Search Messages
	results, err := db.SearchMessages("Message 3", 123456789, false, 10)
	if err != nil {
		t.Fatalf("SearchMessages failed: %v", err)
	}
	if len(results) != 1 || results[0].ID != 3 {
		t.Fatalf("Expected to find Message 3, got %+v", results)
	}

	// 7. Get All Messages For Chat
	allMsgs, err := db.GetAllMessagesForChat(123456789)
	if err != nil {
		t.Fatalf("GetAllMessagesForChat failed: %v", err)
	}
	if len(allMsgs) != 5 {
		t.Fatalf("Expected 5 messages, got %d", len(allMsgs))
	}

	// 8. Stats
	chatCount, msgCount, dbSize, err := db.GetStats()
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if chatCount != 1 || msgCount != 5 || dbSize <= 0 {
		t.Fatalf("Invalid stats: chats=%d, msgs=%d, size=%d", chatCount, msgCount, dbSize)
	}

	// 9. Clear Chat Messages
	if err := db.ClearChatMessages(123456789); err != nil {
		t.Fatalf("ClearChatMessages failed: %v", err)
	}
	afterClearCount, _ := db.CountMessages(123456789)
	if afterClearCount != 0 {
		t.Fatalf("Expected 0 messages after clear, got %d", afterClearCount)
	}
}
