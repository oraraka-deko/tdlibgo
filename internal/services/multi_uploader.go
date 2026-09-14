package services

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/dcpool"
	"github.com/iyear/tdl/core/util/mediautil"

	"tdlibgo/internal/logger"
	"tdlibgo/internal/models"
	"tdlibgo/internal/state"
)

type UploadStatus string

const (
	UploadStatusQueued     UploadStatus = "queued"
	UploadStatusUploading  UploadStatus = "uploading"
	UploadStatusCompleted  UploadStatus = "completed"
	UploadStatusFailed     UploadStatus = "failed"
	UploadStatusCancelled  UploadStatus = "cancelled"
)

// UploadTask represents a single file upload in the multi-uploader queue.
type UploadTask struct {
	ID           string       `json:"id"`
	FileName     string       `json:"file_name"`
	FilePath     string       `json:"file_path"`
	FileSize     int64        `json:"file_size"`
	SentBytes    int64        `json:"sent_bytes"`
	Speed        int64        `json:"speed"` // bytes/sec
	Status       UploadStatus `json:"status"`
	TargetChatID int64        `json:"target_chat_id"`
	ChatTitle    string       `json:"chat_title,omitempty"`
	Caption      string       `json:"caption"`
	MediaType    string       `json:"media_type"` // "auto", "document", "photo", "video", "audio"
	ReplyToMsgID int          `json:"reply_to_msg_id,omitempty"`
	Error        string       `json:"error,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	FinishedAt   *time.Time   `json:"finished_at,omitempty"`
	IsTempFile   bool         `json:"-"` // whether to delete file upon completion

	cancel context.CancelFunc `json:"-"`
	mu     sync.Mutex         `json:"-"`
}

// MultiUploaderEngine manages concurrent multi-file uploading with full 4GB file support.
type MultiUploaderEngine struct {
	pool        dcpool.Pool
	mainAPI     *tg.Client
	state       *state.StateManager
	tempDir     string
	tasksMu     sync.RWMutex
	tasks       map[string]*UploadTask
	activeSlots chan struct{}
}

// NewMultiUploaderEngine initializes the uploader engine.
func NewMultiUploaderEngine(
	pool dcpool.Pool,
	mainAPI *tg.Client,
	s *state.StateManager,
	tempDir string,
) *MultiUploaderEngine {
	if tempDir == "" {
		tempDir = filepath.Join("session", "upload_temp")
	}
	_ = os.MkdirAll(tempDir, 0755)

	return &MultiUploaderEngine{
		pool:        pool,
		mainAPI:     mainAPI,
		state:       s,
		tempDir:     tempDir,
		tasks:       make(map[string]*UploadTask),
		activeSlots: make(chan struct{}, 2), // max 2 concurrent uploads, each with multiple chunk threads
	}
}

// EnqueueUpload adds a new file upload task to the engine.
func (e *MultiUploaderEngine) EnqueueUpload(
	filePath string,
	fileName string,
	targetChatID int64,
	chatTitle string,
	caption string,
	mediaType string,
	replyToMsgID int,
	isTemp bool,
	peerFunc func(ctx context.Context, chatID int64) (tg.InputPeerClass, error),
) (*UploadTask, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat file %s: %w", filePath, err)
	}

	if fileName == "" {
		fileName = filepath.Base(filePath)
	}

	taskID := fmt.Sprintf("up_%d_%d", time.Now().UnixNano(), rand.Intn(10000))
	task := &UploadTask{
		ID:           taskID,
		FileName:     fileName,
		FilePath:     filePath,
		FileSize:     fileInfo.Size(),
		SentBytes:    0,
		Speed:        0,
		Status:       UploadStatusQueued,
		TargetChatID: targetChatID,
		ChatTitle:    chatTitle,
		Caption:      caption,
		MediaType:    mediaType,
		ReplyToMsgID: replyToMsgID,
		CreatedAt:    time.Now(),
		IsTempFile:   isTemp,
	}

	e.tasksMu.Lock()
	e.tasks[taskID] = task
	e.tasksMu.Unlock()

	e.broadcast(task)

	// Launch worker in background
	go e.processUpload(task, peerFunc)

	return task, nil
}

// processUpload executes the upload using streaming chunked workers.
func (e *MultiUploaderEngine) processUpload(
	task *UploadTask,
	peerFunc func(ctx context.Context, chatID int64) (tg.InputPeerClass, error),
) {
	// Acquire concurrency slot
	select {
	case e.activeSlots <- struct{}{}:
		defer func() { <-e.activeSlots }()
	}

	task.mu.Lock()
	if task.Status == UploadStatusCancelled {
		task.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	task.cancel = cancel
	task.Status = UploadStatusUploading
	task.mu.Unlock()

	e.broadcast(task)

	defer func() {
		task.mu.Lock()
		now := time.Now()
		task.FinishedAt = &now
		if task.Status == UploadStatusUploading {
			task.Status = UploadStatusCompleted
			task.SentBytes = task.FileSize
		}
		task.Speed = 0
		task.mu.Unlock()

		if task.IsTempFile {
			_ = os.Remove(task.FilePath)
		}

		e.broadcast(task)
	}()

	// 1. Resolve peer
	peer, err := peerFunc(ctx, task.TargetChatID)
	if err != nil {
		task.mu.Lock()
		task.Status = UploadStatusFailed
		task.Error = fmt.Sprintf("Resolve target peer: %v", err)
		task.mu.Unlock()
		return
	}

	// 2. Open file as io.ReadSeeker (Zero memory explosion for up to 4GB files!)
	file, err := os.Open(task.FilePath)
	if err != nil {
		task.mu.Lock()
		task.Status = UploadStatusFailed
		task.Error = fmt.Sprintf("Open file: %v", err)
		task.mu.Unlock()
		return
	}
	defer file.Close()

	// 3. Setup gotd uploader with 512KB parts and 4 parallel worker threads
	var uploaderClient *tg.Client
	if e.pool != nil {
		uploaderClient = e.pool.Default(ctx)
	} else {
		uploaderClient = e.mainAPI
	}

	up := uploader.NewUploader(uploaderClient).
		WithPartSize(512 * 1024).
		WithThreads(4)

	var lastBytes int64
	var lastTime = time.Now()

	// Wrap progress
	up.WithProgress(&uploaderProgressHook{
		onChunk: func(sentBytes, totalBytes int64) {
			now := time.Now()
			elapsed := now.Sub(lastTime)
			if elapsed >= 500*time.Millisecond {
				diff := sentBytes - lastBytes
				if diff > 0 && elapsed.Seconds() > 0 {
					speed := int64(float64(diff) / elapsed.Seconds())
					task.mu.Lock()
					task.Speed = speed
					task.mu.Unlock()
				}
				lastBytes = sentBytes
				lastTime = now

				task.mu.Lock()
				task.SentBytes = sentBytes
				task.mu.Unlock()

				e.broadcast(task)
			}
		},
	})

	// 4. Execute streaming upload
	uploadObj := uploader.NewUpload(task.FileName, file, task.FileSize)
	inputFile, err := up.Upload(ctx, uploadObj)
	if err != nil {
		task.mu.Lock()
		task.Status = UploadStatusFailed
		task.Error = fmt.Sprintf("Upload error: %v", err)
		task.mu.Unlock()
		logger.Error("MULTI_UP", "Upload failed for %s: %v", task.FileName, err)
		return
	}

	// 5. Detect MIME and construct media
	_, _ = file.Seek(0, io.SeekStart)
	mime, err := mimetype.DetectReader(file)
	mimeStr := "application/octet-stream"
	if err == nil && mime != nil {
		mimeStr = mime.String()
	}

	selectedType := task.MediaType
	if selectedType == "" || selectedType == "auto" {
		switch {
		case mediautil.IsImage(mimeStr) && mimeStr != "image/webp":
			selectedType = "photo"
		case mediautil.IsVideo(mimeStr):
			selectedType = "video"
		case mediautil.IsAudio(mimeStr):
			selectedType = "audio"
		default:
			selectedType = "document"
		}
	}

	var media message.MediaOption
	captionStyled := styling.Plain(task.Caption)

	doc := message.UploadedDocument(inputFile, captionStyled).
		MIME(mimeStr).
		Filename(task.FileName)

	switch selectedType {
	case "photo":
		media = message.UploadedPhoto(inputFile, captionStyled)
	case "video":
		_, _ = file.Seek(0, io.SeekStart)
		if dur, w, h, err := mediautil.GetMP4Info(file); err == nil {
			media = doc.Video().
				Duration(time.Duration(dur) * time.Second).
				Resolution(w, h).
				SupportsStreaming()
		} else {
			media = doc.Video().SupportsStreaming()
		}
	case "audio":
		baseName := strings.TrimSuffix(task.FileName, filepath.Ext(task.FileName))
		media = doc.Audio().Title(baseName)
	default:
		media = doc
	}

	// 6. Send message to chat
	sender := message.NewSender(e.mainAPI).WithUploader(up)
	reqBuilder := sender.To(peer)
	if task.ReplyToMsgID > 0 {
		_, err = reqBuilder.Reply(task.ReplyToMsgID).Media(ctx, media)
	} else {
		_, err = reqBuilder.Media(ctx, media)
	}
	if err != nil {
		task.mu.Lock()
		task.Status = UploadStatusFailed
		task.Error = fmt.Sprintf("Send message: %v", err)
		task.mu.Unlock()
		logger.Error("MULTI_UP", "Failed sending media message for %s: %v", task.FileName, err)
		return
	}

	task.mu.Lock()
	task.Status = UploadStatusCompleted
	task.SentBytes = task.FileSize
	task.mu.Unlock()

	logger.Upload("Uploaded %s (%d bytes) successfully to chat %d", task.FileName, task.FileSize, task.TargetChatID)
}

type uploaderProgressHook struct {
	onChunk func(sent, total int64)
}

func (h *uploaderProgressHook) Chunk(ctx context.Context, state uploader.ProgressState) error {
	if h.onChunk != nil {
		h.onChunk(state.Uploaded, state.Total)
	}
	return nil
}

func (e *MultiUploaderEngine) broadcast(task *UploadTask) {
	if e.state != nil {
		e.state.Broadcast(models.WSMessage{
			Type:    "upload_progress",
			Payload: task,
		})
	}
}

// CancelUpload cancels an ongoing upload.
func (e *MultiUploaderEngine) CancelUpload(taskID string) error {
	e.tasksMu.RLock()
	task, ok := e.tasks[taskID]
	e.tasksMu.RUnlock()

	if !ok {
		return errors.New("task not found")
	}

	task.mu.Lock()
	if task.cancel != nil {
		task.cancel()
	}
	task.Status = UploadStatusCancelled
	task.Speed = 0
	task.mu.Unlock()

	e.broadcast(task)
	return nil
}

// GetTasks returns all upload tasks.
func (e *MultiUploaderEngine) GetTasks() []*UploadTask {
	e.tasksMu.RLock()
	defer e.tasksMu.RUnlock()

	list := make([]*UploadTask, 0, len(e.tasks))
	for _, t := range e.tasks {
		list = append(list, t)
	}
	return list
}

// GetTask returns a single task by ID.
func (e *MultiUploaderEngine) GetTask(taskID string) *UploadTask {
	e.tasksMu.RLock()
	defer e.tasksMu.RUnlock()
	return e.tasks[taskID]
}

// GetTempDir returns the temporary directory for uploads.
func (e *MultiUploaderEngine) GetTempDir() string {
	return e.tempDir
}
