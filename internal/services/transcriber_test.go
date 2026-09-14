package services_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"tdlibgo/internal/services"
	"tdlibgo/internal/storage"
)

func TestTranscriberService(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "transcriber_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "history.db")
	db, err := storage.OpenHistoryDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to open history db: %v", err)
	}
	defer db.Close()

	svc := services.NewTranscriberService(db)
	defer svc.Close()

	// 1. Test cache insertion and retrieval
	chatID := int64(987654)
	msgID := 123
	expectedText := "Testing local speech to text transcription."

	_ = db.SaveTranscription(chatID, msgID, expectedText)

	cached, found := svc.GetCachedTranscription(chatID, msgID)
	if !found || cached != expectedText {
		t.Fatalf("Expected %q (found=%v), got %q", expectedText, found, cached)
	}

	// 2. Test audio transcription if sample audio file exists
	sampleAudio := filepath.Join("..", "..", "transcribe", "audio.wav")
	if _, err := os.Stat(sampleAudio); err == nil {
		ctx := context.Background()
		text, err := svc.TranscribeAudioFile(ctx, 111, 222, sampleAudio)
		if err != nil {
			t.Fatalf("TranscribeAudioFile failed: %v", err)
		}
		if len(text) == 0 {
			t.Fatalf("Transcribed text is empty")
		}
		t.Logf("Transcribed audio text: %q", text)

		// Check that it got cached in DB
		dbText, ok := db.GetTranscription(111, 222)
		if !ok || dbText != text {
			t.Fatalf("Expected db cached transcription %q, got %q", text, dbText)
		}
	}
}
