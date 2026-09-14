package services

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/dcpool"
	"github.com/iyear/tdl/core/tmedia"
	"github.com/iyear/tdl/core/util/tutil"

	"tdlibgo/internal/logger"
	"tdlibgo/internal/models"
	"tdlibgo/internal/state"
)

type DownloadTaskStatus string

const (
	StatusQueued      DownloadTaskStatus = "queued"
	StatusDownloading DownloadTaskStatus = "downloading"
	StatusPaused      DownloadTaskStatus = "paused"
	StatusCompleted   DownloadTaskStatus = "completed"
	StatusFailed      DownloadTaskStatus = "failed"
	StatusCancelled   DownloadTaskStatus = "cancelled"
)

// DownloadItem represents a single media file to be downloaded within a task.
type DownloadItem struct {
	ID              string `json:"id"`
	MsgID           int    `json:"msg_id"`
	ChatID          int64  `json:"chat_id"`
	FileName        string `json:"file_name"`
	FileSize        int64  `json:"file_size"`
	DownloadedBytes int64  `json:"downloaded_bytes"`
	DC              int    `json:"dc"`
	Status          string `json:"status"` // queued, downloading, completed, failed
	Error           string `json:"error,omitempty"`
	SavePath        string `json:"save_path,omitempty"`

	// Internal location
	Location tg.InputFileLocationClass `json:"-"`
}

// DownloadTask represents a batch download job initiated by the user.
type DownloadTask struct {
	ID              string             `json:"id"`
	Title           string             `json:"title"`
	Source          string             `json:"source"` // "url" or "chat"
	ChatID          int64              `json:"chat_id,omitempty"`
	ChatTitle       string             `json:"chat_title,omitempty"`
	URLs            []string           `json:"urls,omitempty"`
	StartMsgID      int                `json:"start_msg_id,omitempty"`
	EndMsgID        int                `json:"end_msg_id,omitempty"`
	Filter          string             `json:"filter"` // "all", "photo", "video", "document", "audio"
	OutputDir       string             `json:"output_dir"`
	Threads         int                `json:"threads"`
	TotalItems      int                `json:"total_items"`
	CompletedItems  int                `json:"completed_items"`
	TotalBytes      int64              `json:"total_bytes"`
	DownloadedBytes int64              `json:"downloaded_bytes"`
	Speed           int64              `json:"speed"` // bytes per sec
	Status          DownloadTaskStatus `json:"status"`
	Error           string             `json:"error,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	FinishedAt      *time.Time         `json:"finished_at,omitempty"`
	Items           []*DownloadItem    `json:"items"`

	cancel context.CancelFunc `json:"-"`
	mu     sync.Mutex         `json:"-"`
}

// BatchDownloaderService orchestrates multi-threaded batch downloads from chats and URLs.
type BatchDownloaderService struct {
	pool        dcpool.Pool
	mainAPI     *tg.Client
	state       *state.StateManager
	tasksMu     sync.RWMutex
	tasks       map[string]*DownloadTask
	activeSlots chan struct{}
}

// NewBatchDownloaderService creates a new batch download controller.
func NewBatchDownloaderService(pool dcpool.Pool, mainAPI *tg.Client, s *state.StateManager) *BatchDownloaderService {
	return &BatchDownloaderService{
		pool:        pool,
		mainAPI:     mainAPI,
		state:       s,
		tasks:       make(map[string]*DownloadTask),
		activeSlots: make(chan struct{}, 4), // up to 4 concurrent file downloads across all tasks
	}
}

// ChatDownloadRequest parameters for creating a chat batch download.
type ChatDownloadRequest struct {
	ChatID     int64    `json:"chat_id"`
	StartMsgID int      `json:"start_msg_id"`
	EndMsgID   int      `json:"end_msg_id"`
	MsgIDs     []int    `json:"msg_ids"`
	Filter     string   `json:"filter"` // "all", "photo", "video", "document", "audio"
	OutputDir  string   `json:"output_dir"`
	Threads    int      `json:"threads"`
	Limit      int      `json:"limit"`
	URLs       []string `json:"urls"`
}

// CreateTaskFromChat initializes a download job for messages in a chat.
func (s *BatchDownloaderService) CreateTaskFromChat(
	ctx context.Context,
	req ChatDownloadRequest,
	chatTitle string,
	peer tg.InputPeerClass,
) (*DownloadTask, error) {
	if s.mainAPI == nil {
		return nil, errors.New("telegram API not initialized")
	}

	outDir := req.OutputDir
	if outDir == "" {
		outDir = filepath.Join("downloads", fmt.Sprintf("chat_%d", req.ChatID))
	}
	_ = os.MkdirAll(outDir, 0755)

	threads := req.Threads
	if threads < 1 {
		threads = 4
	}

	taskID := fmt.Sprintf("dl_%d_%d", time.Now().UnixNano(), rand.Intn(10000))
	task := &DownloadTask{
		ID:         taskID,
		Title:      fmt.Sprintf("Download from %s", chatTitle),
		Source:     "chat",
		ChatID:     req.ChatID,
		ChatTitle:  chatTitle,
		StartMsgID: req.StartMsgID,
		EndMsgID:   req.EndMsgID,
		Filter:     req.Filter,
		OutputDir:  outDir,
		Threads:    threads,
		Status:     StatusQueued,
		CreatedAt:  time.Now(),
		Items:      make([]*DownloadItem, 0),
	}

	s.tasksMu.Lock()
	s.tasks[taskID] = task
	s.tasksMu.Unlock()

	// Crawl chat messages in background and populate items
	go s.runChatCrawlerAndDownload(task, peer, req)

	return task, nil
}

// CreateTaskFromURLs initializes a download job for Telegram URLs.
func (s *BatchDownloaderService) CreateTaskFromURLs(
	ctx context.Context,
	urls []string,
	outputDir string,
	threads int,
	resolvePeerFunc func(ctx context.Context, chatID int64) (tg.InputPeerClass, error),
) (*DownloadTask, error) {
	if s.mainAPI == nil {
		return nil, errors.New("telegram API not initialized")
	}

	if len(urls) == 0 {
		return nil, errors.New("no URLs provided")
	}

	outDir := outputDir
	if outDir == "" {
		outDir = filepath.Join("downloads", "urls")
	}
	_ = os.MkdirAll(outDir, 0755)

	if threads < 1 {
		threads = 4
	}

	taskID := fmt.Sprintf("dl_url_%d_%d", time.Now().UnixNano(), rand.Intn(10000))
	task := &DownloadTask{
		ID:        taskID,
		Title:     fmt.Sprintf("Download %d Telegram URL(s)", len(urls)),
		Source:    "url",
		URLs:      urls,
		Filter:    "all",
		OutputDir: outDir,
		Threads:   threads,
		Status:    StatusQueued,
		CreatedAt: time.Now(),
		Items:     make([]*DownloadItem, 0),
	}

	s.tasksMu.Lock()
	s.tasks[taskID] = task
	s.tasksMu.Unlock()

	// Resolve and run in background
	go s.runURLCrawlerAndDownload(task, urls, resolvePeerFunc)

	return task, nil
}

// runChatCrawlerAndDownload fetches message media from the chat and begins downloading.
func (s *BatchDownloaderService) runChatCrawlerAndDownload(
	task *DownloadTask,
	peer tg.InputPeerClass,
	req ChatDownloadRequest,
) {
	ctx, cancel := context.WithCancel(context.Background())
	task.mu.Lock()
	task.cancel = cancel
	task.Status = StatusDownloading
	task.mu.Unlock()

	s.broadcastProgress(task)

	defer func() {
		task.mu.Lock()
		now := time.Now()
		task.FinishedAt = &now
		task.Speed = 0
		var sum int64
		for _, it := range task.Items {
			sum += it.DownloadedBytes
		}
		task.DownloadedBytes = sum
		if task.CompletedItems == task.TotalItems && task.TotalItems > 0 {
			task.Status = StatusCompleted
			task.DownloadedBytes = task.TotalBytes
		} else if task.Status == StatusDownloading {
			task.Status = StatusFailed
		}
		task.mu.Unlock()
		s.broadcastProgress(task)
	}()

	// 1. Collect messages
	var targetIDs []int
	if len(req.MsgIDs) > 0 {
		targetIDs = req.MsgIDs
	} else if req.StartMsgID > 0 && req.EndMsgID >= req.StartMsgID {
		for i := req.StartMsgID; i <= req.EndMsgID; i++ {
			targetIDs = append(targetIDs, i)
		}
	} else {
		// Fetch last N messages from chat
		limit := req.Limit
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		historyReq := &tg.MessagesGetHistoryRequest{
			Peer:  peer,
			Limit: limit,
		}
		res, err := s.mainAPI.MessagesGetHistory(ctx, historyReq)
		if err != nil {
			task.mu.Lock()
			task.Status = StatusFailed
			task.Error = fmt.Sprintf("Failed to fetch chat history: %v", err)
			task.mu.Unlock()
			return
		}

		var msgs []tg.MessageClass
		switch h := res.(type) {
		case *tg.MessagesMessages:
			msgs = h.Messages
		case *tg.MessagesMessagesSlice:
			msgs = h.Messages
		case *tg.MessagesChannelMessages:
			msgs = h.Messages
		}

		for _, m := range msgs {
			if msg, ok := m.(*tg.Message); ok {
				targetIDs = append(targetIDs, msg.ID)
			}
		}
	}

	// 2. Fetch full message objects and extract media
	items := make([]*DownloadItem, 0)
	var totalBytes int64

	chunkSize := 50
	for i := 0; i < len(targetIDs); i += chunkSize {
		if ctx.Err() != nil {
			return
		}
		end := i + chunkSize
		if end > len(targetIDs) {
			end = len(targetIDs)
		}
		batch := targetIDs[i:end]

		inputMsgs := make([]tg.InputMessageClass, len(batch))
		for idx, id := range batch {
			inputMsgs[idx] = &tg.InputMessageID{ID: id}
		}

		// Fetch messages
		var fetchedMsgs []tg.MessageClass
		if channel, ok := peer.(*tg.InputPeerChannel); ok {
			chRes, err := s.mainAPI.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
				Channel: &tg.InputChannel{
					ChannelID:  channel.ChannelID,
					AccessHash: channel.AccessHash,
				},
				ID: inputMsgs,
			})
			if err == nil {
				switch r := chRes.(type) {
				case *tg.MessagesChannelMessages:
					fetchedMsgs = r.Messages
				case *tg.MessagesMessages:
					fetchedMsgs = r.Messages
				case *tg.MessagesMessagesSlice:
					fetchedMsgs = r.Messages
				}
			}
		} else {
			mRes, err := s.mainAPI.MessagesGetMessages(ctx, inputMsgs)
			if err == nil {
				switch r := mRes.(type) {
				case *tg.MessagesMessages:
					fetchedMsgs = r.Messages
				case *tg.MessagesMessagesSlice:
					fetchedMsgs = r.Messages
				case *tg.MessagesChannelMessages:
					fetchedMsgs = r.Messages
				}
			}
		}

		for _, m := range fetchedMsgs {
			msg, ok := m.(*tg.Message)
			if !ok {
				continue
			}
			media, hasMedia := tmedia.GetMedia(msg)
			if !hasMedia || media == nil {
				continue
			}

			// Apply filter
			if !s.matchesFilter(media, req.Filter) {
				continue
			}

			item := &DownloadItem{
				ID:              fmt.Sprintf("%s_item_%d", task.ID, msg.ID),
				MsgID:           msg.ID,
				ChatID:          task.ChatID,
				FileName:        media.Name,
				FileSize:        media.Size,
				DownloadedBytes: 0,
				DC:              media.DC,
				Status:          "queued",
				Location:        media.InputFileLoc,
				SavePath:        filepath.Join(task.OutputDir, media.Name),
			}
			items = append(items, item)
			totalBytes += media.Size
		}
	}

	task.mu.Lock()
	task.Items = items
	task.TotalItems = len(items)
	task.TotalBytes = totalBytes
	task.mu.Unlock()

	s.broadcastProgress(task)

	if len(items) == 0 {
		task.mu.Lock()
		task.Status = StatusCompleted
		task.mu.Unlock()
		return
	}

	// 3. Execute downloads
	s.downloadItems(ctx, task)
}

// runURLCrawlerAndDownload parses Telegram URLs, resolves media, and downloads.
func (s *BatchDownloaderService) runURLCrawlerAndDownload(
	task *DownloadTask,
	urls []string,
	resolvePeerFunc func(ctx context.Context, chatID int64) (tg.InputPeerClass, error),
) {
	ctx, cancel := context.WithCancel(context.Background())
	task.mu.Lock()
	task.cancel = cancel
	task.Status = StatusDownloading
	task.mu.Unlock()

	s.broadcastProgress(task)

	defer func() {
		task.mu.Lock()
		now := time.Now()
		task.FinishedAt = &now
		task.Speed = 0
		var sum int64
		for _, it := range task.Items {
			sum += it.DownloadedBytes
		}
		task.DownloadedBytes = sum
		if task.Status == StatusDownloading {
			if task.CompletedItems == task.TotalItems && task.TotalItems > 0 {
				task.Status = StatusCompleted
				task.DownloadedBytes = task.TotalBytes
			} else {
				task.Status = StatusFailed
			}
		}
		task.mu.Unlock()
		s.broadcastProgress(task)
	}()

	items := make([]*DownloadItem, 0)
	var totalBytes int64

	for _, u := range urls {
		if ctx.Err() != nil {
			return
		}
		// Parse URL: t.me/channel/msgID or t.me/c/chatID/msgID
		parts := strings.Split(strings.TrimPrefix(u, "https://"), "/")
		if len(parts) < 3 && !strings.Contains(u, "t.me/") {
			continue
		}

		// Use simple link extraction logic
		var chatRef string
		var msgID int
		cleanURL := strings.Split(u, "?")[0]
		urlSegments := strings.Split(strings.Trim(strings.TrimPrefix(cleanURL, "https://t.me/"), "/"), "/")

		if len(urlSegments) == 2 {
			chatRef = urlSegments[0]
			fmt.Sscanf(urlSegments[1], "%d", &msgID)
		} else if len(urlSegments) >= 3 && urlSegments[0] == "c" {
			chatRef = urlSegments[1]
			fmt.Sscanf(urlSegments[2], "%d", &msgID)
		}

		if msgID <= 0 {
			continue
		}

		// Resolve chat and fetch single message
		var chatID int64
		fmt.Sscanf(chatRef, "%d", &chatID)

		peer, err := resolvePeerFunc(ctx, chatID)
		if err != nil || peer == nil {
			continue
		}

		// Get message
		inputMsgs := []tg.InputMessageClass{&tg.InputMessageID{ID: msgID}}
		var fetchedMsgs []tg.MessageClass
		if channel, ok := peer.(*tg.InputPeerChannel); ok {
			chRes, err := s.mainAPI.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
				Channel: &tg.InputChannel{
					ChannelID:  channel.ChannelID,
					AccessHash: channel.AccessHash,
				},
				ID: inputMsgs,
			})
			if err == nil {
				switch r := chRes.(type) {
				case *tg.MessagesChannelMessages:
					fetchedMsgs = r.Messages
				case *tg.MessagesMessages:
					fetchedMsgs = r.Messages
				}
			}
		} else {
			mRes, err := s.mainAPI.MessagesGetMessages(ctx, inputMsgs)
			if err == nil {
				switch r := mRes.(type) {
				case *tg.MessagesMessages:
					fetchedMsgs = r.Messages
				}
			}
		}

		for _, m := range fetchedMsgs {
			msg, ok := m.(*tg.Message)
			if !ok {
				continue
			}
			media, hasMedia := tmedia.GetMedia(msg)
			if !hasMedia || media == nil {
				continue
			}

			item := &DownloadItem{
				ID:              fmt.Sprintf("%s_item_%d", task.ID, msg.ID),
				MsgID:           msg.ID,
				ChatID:          chatID,
				FileName:        media.Name,
				FileSize:        media.Size,
				DownloadedBytes: 0,
				DC:              media.DC,
				Status:          "queued",
				Location:        media.InputFileLoc,
				SavePath:        filepath.Join(task.OutputDir, media.Name),
			}
			items = append(items, item)
			totalBytes += media.Size
		}
	}

	task.mu.Lock()
	task.Items = items
	task.TotalItems = len(items)
	task.TotalBytes = totalBytes
	task.mu.Unlock()

	s.broadcastProgress(task)

	if len(items) == 0 {
		task.mu.Lock()
		task.Status = StatusCompleted
		task.mu.Unlock()
		return
	}

	s.downloadItems(ctx, task)
}

// downloadItems processes items sequentially or with bounded concurrency.
func (s *BatchDownloaderService) downloadItems(ctx context.Context, task *DownloadTask) {
	for _, item := range task.Items {
		if ctx.Err() != nil {
			return
		}

		// Check if paused
		task.mu.Lock()
		if task.Status == StatusPaused {
			task.mu.Unlock()
			return
		}
		item.Status = "downloading"
		task.mu.Unlock()
		s.broadcastProgress(task)

		// Acquire global slot
		select {
		case s.activeSlots <- struct{}{}:
		case <-ctx.Done():
			return
		}

		err := s.downloadSingleItem(ctx, task, item)
		<-s.activeSlots

		task.mu.Lock()
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			logger.Error("BATCH_DL", "Failed downloading %s: %v", item.FileName, err)
		} else {
			item.Status = "completed"
			item.DownloadedBytes = item.FileSize
			task.CompletedItems++
		}
		var sum int64
		for _, it := range task.Items {
			sum += it.DownloadedBytes
		}
		task.DownloadedBytes = sum
		task.mu.Unlock()

		s.broadcastProgress(task)
	}

	task.mu.Lock()
	task.Speed = 0
	var sum int64
	for _, it := range task.Items {
		sum += it.DownloadedBytes
	}
	task.DownloadedBytes = sum
	if task.CompletedItems == task.TotalItems && task.TotalItems > 0 {
		task.Status = StatusCompleted
		task.DownloadedBytes = task.TotalBytes
	}
	task.mu.Unlock()
	s.broadcastProgress(task)
}

// downloadSingleItem downloads one file using multi-DC pool and tracks speed.
func (s *BatchDownloaderService) downloadSingleItem(
	ctx context.Context,
	task *DownloadTask,
	item *DownloadItem,
) error {
	if item.Location == nil {
		return errors.New("nil location")
	}

	// Determine destination client based on file DC
	var client *tg.Client
	if item.DC > 0 && s.pool != nil {
		client = s.pool.Client(ctx, item.DC)
	} else if s.pool != nil {
		client = s.pool.Default(ctx)
	} else {
		client = s.mainAPI
	}

	if client == nil {
		return errors.New("client unavailable")
	}

	tempPath := item.SavePath + ".tdl.tmp"
	_ = os.MkdirAll(filepath.Dir(tempPath), 0755)

	threads := task.Threads
	if threads < 1 {
		threads = tutil.BestThreads(item.FileSize, 4)
	}
	if threads < 1 {
		threads = 2
	}

	dl := downloader.NewDownloader().WithPartSize(1024 * 1024)

	var lastBytes int64
	var lastTime = time.Now()

	writer := &progressWriter{
		path: tempPath,
		onWrite: func(n int64) {
			now := time.Now()
			elapsed := now.Sub(lastTime)
			if elapsed >= 500*time.Millisecond {
				diff := n - lastBytes
				if diff > 0 && elapsed.Seconds() > 0 {
					speed := int64(float64(diff) / elapsed.Seconds())
					task.mu.Lock()
					task.Speed = speed
					task.mu.Unlock()
				}
				lastBytes = n
				lastTime = now

				task.mu.Lock()
				item.DownloadedBytes = n
				// Update task total downloaded
				var sum int64
				for _, it := range task.Items {
					sum += it.DownloadedBytes
				}
				task.DownloadedBytes = sum
				task.mu.Unlock()

				s.broadcastProgress(task)
			}
		},
	}

	f, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.Wrap(err, "open temp file")
	}
	defer f.Close()

	writer.file = f

	_, err = dl.Download(client, item.Location).
		WithThreads(threads).
		Parallel(ctx, writer)

	if err != nil {
		_ = os.Remove(tempPath)
		return errors.Wrap(err, "download chunk parallel")
	}

	_ = f.Close()

	// Atomic rename to final path
	_ = os.Remove(item.SavePath)
	if err := os.Rename(tempPath, item.SavePath); err != nil {
		return errors.Wrap(err, "rename to final file")
	}

	return nil
}

type progressWriter struct {
	path    string
	file    *os.File
	written atomic.Int64
	onWrite func(n int64)
}

func (w *progressWriter) WriteAt(p []byte, off int64) (int, error) {
	n, err := w.file.WriteAt(p, off)
	if err == nil {
		current := w.written.Add(int64(n))
		if w.onWrite != nil {
			w.onWrite(current)
		}
	}
	return n, err
}

func (s *BatchDownloaderService) matchesFilter(media *tmedia.Media, filter string) bool {
	if filter == "" || filter == "all" {
		return true
	}
	name := strings.ToLower(media.Name)
	switch filter {
	case "photo":
		return strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") || strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".webp")
	case "video":
		return strings.HasSuffix(name, ".mp4") || strings.HasSuffix(name, ".mkv") || strings.HasSuffix(name, ".mov") || strings.HasSuffix(name, ".avi")
	case "audio":
		return strings.HasSuffix(name, ".mp3") || strings.HasSuffix(name, ".flac") || strings.HasSuffix(name, ".m4a") || strings.HasSuffix(name, ".ogg")
	case "document":
		return !strings.HasSuffix(name, ".jpg") && !strings.HasSuffix(name, ".jpeg") && !strings.HasSuffix(name, ".png") && !strings.HasSuffix(name, ".mp4")
	}
	return true
}

func (s *BatchDownloaderService) broadcastProgress(task *DownloadTask) {
	if s.state != nil {
		s.state.Broadcast(models.WSMessage{
			Type:    "download_progress",
			Payload: task,
		})
	}
}

// PauseTask pauses an active download task.
func (s *BatchDownloaderService) PauseTask(taskID string) error {
	s.tasksMu.RLock()
	task, ok := s.tasks[taskID]
	s.tasksMu.RUnlock()

	if !ok {
		return errors.New("task not found")
	}

	task.mu.Lock()
	if task.cancel != nil {
		task.cancel()
	}
	task.Status = StatusPaused
	task.Speed = 0
	task.mu.Unlock()

	s.broadcastProgress(task)
	return nil
}

// ResumeTask resumes a paused download task.
func (s *BatchDownloaderService) ResumeTask(taskID string, peer tg.InputPeerClass) error {
	s.tasksMu.RLock()
	task, ok := s.tasks[taskID]
	s.tasksMu.RUnlock()

	if !ok {
		return errors.New("task not found")
	}

	task.mu.Lock()
	if task.Status != StatusPaused && task.Status != StatusFailed {
		task.mu.Unlock()
		return errors.New("task cannot be resumed")
	}
	task.Status = StatusDownloading
	ctx, cancel := context.WithCancel(context.Background())
	task.cancel = cancel
	task.mu.Unlock()

	s.broadcastProgress(task)

	go func() {
		defer func() {
			task.mu.Lock()
			now := time.Now()
			task.FinishedAt = &now
			task.Speed = 0
			var sum int64
			for _, it := range task.Items {
				sum += it.DownloadedBytes
			}
			task.DownloadedBytes = sum
			if task.CompletedItems == task.TotalItems && task.TotalItems > 0 {
				task.Status = StatusCompleted
				task.DownloadedBytes = task.TotalBytes
			}
			task.mu.Unlock()
			s.broadcastProgress(task)
		}()
		s.downloadItems(ctx, task)
	}()

	return nil
}

// CancelTask cancels and terminates an active task.
func (s *BatchDownloaderService) CancelTask(taskID string) error {
	s.tasksMu.RLock()
	task, ok := s.tasks[taskID]
	s.tasksMu.RUnlock()

	if !ok {
		return errors.New("task not found")
	}

	task.mu.Lock()
	if task.cancel != nil {
		task.cancel()
	}
	task.Status = StatusCancelled
	task.Speed = 0
	task.mu.Unlock()

	s.broadcastProgress(task)
	return nil
}

// DeleteTask removes task from history.
func (s *BatchDownloaderService) DeleteTask(taskID string) error {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return errors.New("task not found")
	}

	task.mu.Lock()
	if task.cancel != nil {
		task.cancel()
	}
	task.mu.Unlock()

	delete(s.tasks, taskID)
	return nil
}

// GetTasks returns a list of all download tasks ordered by creation time.
func (s *BatchDownloaderService) GetTasks() []*DownloadTask {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()

	list := make([]*DownloadTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		list = append(list, t)
	}
	return list
}

// GetTask returns a specific task by ID.
func (s *BatchDownloaderService) GetTask(taskID string) *DownloadTask {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()
	return s.tasks[taskID]
}
