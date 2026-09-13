package services

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/dcpool"
	"github.com/iyear/tdl/core/util/tutil"

	"tdlibgo/internal/logger"
)

// DownloaderService manages disk caching and parallel MTProto chunked downloads via multi-DC pool.
type DownloaderService struct {
	pool     dcpool.Pool
	cacheDir string
	mu       sync.Mutex
	active   map[string]*sync.WaitGroup
	sem      chan struct{}
}

// NewDownloaderService initializes the downloader service with a multi-DC connection pool.
func NewDownloaderService(pool dcpool.Pool, cacheDir string) *DownloaderService {
	if cacheDir == "" {
		cacheDir = filepath.Join("session", "media_cache")
	}
	_ = os.MkdirAll(cacheDir, 0755)

	return &DownloaderService{
		pool:     pool,
		cacheDir: cacheDir,
		active:   make(map[string]*sync.WaitGroup),
		sem:      make(chan struct{}, 4), // Max 4 concurrent downloads across pool
	}
}

// CachePath returns the full local filesystem path for a cached media file.
func (s *DownloaderService) CachePath(chatID int64, messageID int) string {
	return filepath.Join(s.cacheDir, fmt.Sprintf("%d_%d.bin", chatID, messageID))
}

// IsCached returns true if the media file is already downloaded and present on disk.
func (s *DownloaderService) IsCached(chatID int64, messageID int) (string, bool) {
	p := s.CachePath(chatID, messageID)
	info, err := os.Stat(p)
	if err == nil && info.Size() > 0 {
		return p, true
	}
	return p, false
}

// FetchOrDownload ensures the media file is cached locally on disk.
func (s *DownloaderService) FetchOrDownload(
	ctx context.Context,
	chatID int64,
	messageID int,
	location tg.InputFileLocationClass,
) (string, error) {
	return s.FetchOrDownloadWithDC(ctx, chatID, messageID, location, 0, 0)
}

// FetchOrDownloadWithDC downloads media chunks using the dedicated connection pool for the target DC.
func (s *DownloaderService) FetchOrDownloadWithDC(
	ctx context.Context,
	chatID int64,
	messageID int,
	location tg.InputFileLocationClass,
	dc int,
	size int64,
) (string, error) {
	cachePath, exists := s.IsCached(chatID, messageID)
	if exists {
		return cachePath, nil
	}

	key := fmt.Sprintf("%d:%d", chatID, messageID)

	s.mu.Lock()
	wg, inFlight := s.active[key]
	if inFlight {
		s.mu.Unlock()
		wg.Wait()
		if _, ok := s.IsCached(chatID, messageID); ok {
			return cachePath, nil
		}
	} else {
		wg = &sync.WaitGroup{}
		wg.Add(1)
		s.active[key] = wg
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			delete(s.active, key)
			s.mu.Unlock()
			wg.Done()
		}()
	}

	// Double check cache
	if _, ok := s.IsCached(chatID, messageID); ok {
		return cachePath, nil
	}

	// Acquire concurrency slot (max 4 concurrent downloads)
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	tempPath := cachePath + ".tmp"
	start := time.Now()
	logger.Download("Downloading media [%d:%d] (DC %d, %d bytes) via tdl/core multi-DC pool...", chatID, messageID, dc, size)

	// Execute chunked download using detached context (180s)
	dlCtx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	var client *tg.Client
	if dc > 0 && s.pool != nil {
		client = s.pool.Client(dlCtx, dc)
	} else if s.pool != nil {
		client = s.pool.Default(dlCtx)
	}

	if client == nil {
		return "", errors.New("dcpool client unavailable")
	}

	threads := tutil.BestThreads(size, 4)
	if threads < 1 {
		threads = 2
	}

	// Telegram max part size is 1MB
	dl := downloader.NewDownloader().WithPartSize(1024 * 1024)
	_, err := dl.Download(client, location).
		WithThreads(threads).
		ToPath(dlCtx, tempPath)

	if err != nil {
		_ = os.Remove(tempPath)
		logger.Error("DOWNLOAD", "Download failed for [%d:%d] on DC %d: %v", chatID, messageID, dc, err)
		return "", errors.Wrap(err, "download file")
	}

	// Rename temp file to permanent cache file atomically
	if err := os.Rename(tempPath, cachePath); err != nil {
		return "", errors.Wrap(err, "rename to cache path")
	}

	logger.Download("Cached media [%d:%d] from DC %d in %v -> %s", chatID, messageID, dc, time.Since(start), cachePath)
	return cachePath, nil
}

// ServeMediaFile streams a local cached file with HTTP Range support (206 Partial Content).
func (s *DownloaderService) ServeMediaFile(
	w http.ResponseWriter,
	r *http.Request,
	filePath string,
	fileName string,
	mimeType string,
	forceDownload bool,
) {
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, "Unable to read file stat", http.StatusInternalServerError)
		return
	}

	if mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	if forceDownload {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, fileName))
	}

	// Enable byte-range seeking for audio/video players
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

	http.ServeContent(w, r, fileName, info.ModTime(), file)
}
