package state

import (
	"testing"
	"time"

	"tdlibgo/internal/models"
)

func TestAppendHistoricalMessages(t *testing.T) {
	sm := NewStateManager("+12294660989")

	msgs := []*models.Message{
		{
			ID:       1,
			ChatID:   100,
			SenderID: 100,
			Text:     "First message",
			Date:     time.Now(),
		},
		{
			ID:       2,
			ChatID:   100,
			SenderID: 100,
			Text:     "Second message",
			Date:     time.Now().Add(time.Second),
		},
	}

	// This must not panic with double unlock
	sm.AppendHistoricalMessages(100, msgs)

	result := sm.GetMessages(100, 10, 0)
	if len(result) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(result))
	}
	if result[0].ID != 1 || result[1].ID != 2 {
		t.Fatalf("Unexpected message order: %+v", result)
	}
}
