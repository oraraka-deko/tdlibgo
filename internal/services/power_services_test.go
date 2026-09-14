package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/tmedia"

	"tdlibgo/internal/state"
	"tdlibgo/internal/storage"
)

func TestBatchDownloaderService_TaskLifecycle(t *testing.T) {
	st := state.NewStateManager("+1234567890")
	dl := NewBatchDownloaderService(nil, nil, st)

	// Test matchesFilter
	photoMedia := &tmedia.Media{Name: "photo.jpg"}
	videoMedia := &tmedia.Media{Name: "movie.mp4"}
	audioMedia := &tmedia.Media{Name: "song.mp3"}
	docMedia := &tmedia.Media{Name: "archive.zip"}

	if !dl.matchesFilter(photoMedia, "photo") {
		t.Fatalf("Expected photo to match photo filter")
	}
	if dl.matchesFilter(videoMedia, "photo") {
		t.Fatalf("Expected video not to match photo filter")
	}
	if !dl.matchesFilter(videoMedia, "video") {
		t.Fatalf("Expected video to match video filter")
	}
	if !dl.matchesFilter(audioMedia, "audio") {
		t.Fatalf("Expected audio to match audio filter")
	}
	if !dl.matchesFilter(docMedia, "document") {
		t.Fatalf("Expected doc to match document filter")
	}
	if !dl.matchesFilter(docMedia, "all") {
		t.Fatalf("Expected all to match any media")
	}

	// Manual task insertion
	task := &DownloadTask{
		ID:        "test_dl_1",
		Title:     "Test Download",
		Status:    StatusDownloading,
		OutputDir: "downloads",
		Items:     []*DownloadItem{},
	}
	dl.tasks[task.ID] = task

	// Pause
	if err := dl.PauseTask("test_dl_1"); err != nil {
		t.Fatalf("PauseTask failed: %v", err)
	}
	if task.Status != StatusPaused {
		t.Fatalf("Expected status paused, got %s", task.Status)
	}

	// Cancel
	if err := dl.CancelTask("test_dl_1"); err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}
	if task.Status != StatusCancelled {
		t.Fatalf("Expected status cancelled, got %s", task.Status)
	}

	// Delete
	if err := dl.DeleteTask("test_dl_1"); err != nil {
		t.Fatalf("DeleteTask failed: %v", err)
	}
	if dl.GetTask("test_dl_1") != nil {
		t.Fatalf("Expected task to be deleted")
	}
}

func TestMultiUploaderEngine_EnqueueAndCancel(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "uploader_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFilePath := filepath.Join(tempDir, "sample_4gb_test.bin")
	_ = os.WriteFile(testFilePath, []byte("test content payload"), 0644)

	st := state.NewStateManager("+1234567890")
	engine := NewMultiUploaderEngine(nil, nil, st, tempDir)

	mockPeerFunc := func(ctx context.Context, chatID int64) (tg.InputPeerClass, error) {
		return &tg.InputPeerUser{UserID: 12345, AccessHash: 67890}, nil
	}

	task, err := engine.EnqueueUpload(
		testFilePath,
		"sample_4gb_test.bin",
		12345,
		"Test Chat",
		"Test Caption",
		"document",
		0,
		false,
		mockPeerFunc,
	)
	if err != nil {
		t.Fatalf("EnqueueUpload failed: %v", err)
	}

	if task.FileName != "sample_4gb_test.bin" || task.FileSize != 20 {
		t.Fatalf("Unexpected task info: %+v", task)
	}

	tasks := engine.GetTasks()
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(tasks))
	}

	if err := engine.CancelUpload(task.ID); err != nil {
		t.Fatalf("CancelUpload failed: %v", err)
	}
}

func TestIndexerService_CacheStats(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "indexer_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mediaDir := filepath.Join(tempDir, "media")
	avatarDir := filepath.Join(tempDir, "avatars")
	dbPath := filepath.Join(tempDir, "history.db")

	_ = os.MkdirAll(mediaDir, 0755)
	_ = os.MkdirAll(avatarDir, 0755)

	// Write mock media file
	_ = os.WriteFile(filepath.Join(mediaDir, "123_456.bin"), make([]byte, 1024*50), 0644)
	_ = os.WriteFile(filepath.Join(avatarDir, "user_1.jpg"), make([]byte, 1024*10), 0644)

	hdb, err := storage.OpenHistoryDB(dbPath)
	if err != nil {
		t.Fatalf("OpenHistoryDB failed: %v", err)
	}
	defer hdb.Close()

	st := state.NewStateManager("+1234567890")
	idx := NewIndexerService(nil, hdb, st, mediaDir, avatarDir, dbPath)

	stats := idx.GetCacheStats()
	if stats.MediaCacheFiles != 1 || stats.MediaCacheBytes != 1024*50 {
		t.Fatalf("Unexpected media stats: files=%d, bytes=%d", stats.MediaCacheFiles, stats.MediaCacheBytes)
	}
	if stats.AvatarCacheFiles != 1 {
		t.Fatalf("Unexpected avatar stats: files=%d", stats.AvatarCacheFiles)
	}

	// Test cache clear
	if err := idx.ClearCache(true, false); err != nil {
		t.Fatalf("ClearCache failed: %v", err)
	}
	statsAfter := idx.GetCacheStats()
	if statsAfter.MediaCacheFiles != 0 || statsAfter.MediaCacheBytes != 0 {
		t.Fatalf("Expected media cache to be empty, got files=%d, bytes=%d", statsAfter.MediaCacheFiles, statsAfter.MediaCacheBytes)
	}
}
