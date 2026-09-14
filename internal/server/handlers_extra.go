package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gotd/td/tg"

	"tdlibgo/internal/services"
)

// --- Batch Downloader Handlers ---

func (s *Server) handleDownloaderTasks(w http.ResponseWriter, r *http.Request) {
	dl := s.client.GetBatchDownloader()
	if dl == nil {
		s.writeError(w, http.StatusServiceUnavailable, "downloader not ready")
		return
	}
	tasks := dl.GetTasks()
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": tasks,
	})
}

func (s *Server) handleDownloaderStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	dl := s.client.GetBatchDownloader()
	if dl == nil {
		s.writeError(w, http.StatusServiceUnavailable, "downloader not ready")
		return
	}

	var req services.ChatDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if len(req.URLs) > 0 {
		task, err := dl.CreateTaskFromURLs(ctx, req.URLs, req.OutputDir, req.Threads, s.client.ResolveInputPeer)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, task)
		return
	}

	if req.ChatID == 0 {
		s.writeError(w, http.StatusBadRequest, "chat_id or urls required")
		return
	}

	chatTitle := fmt.Sprintf("Chat %d", req.ChatID)
	if chat, ok := s.state.GetChat(req.ChatID); ok {
		chatTitle = chat.Title
	}

	peer, err := s.client.ResolveInputPeer(ctx, req.ChatID)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to resolve peer: %v", err))
		return
	}

	task, err := dl.CreateTaskFromChat(ctx, req, chatTitle, peer)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleDownloaderPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dl := s.client.GetBatchDownloader()
	if dl == nil {
		s.writeError(w, http.StatusServiceUnavailable, "downloader not ready")
		return
	}
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" {
		s.writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}
	if err := dl.PauseTask(req.TaskID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (s *Server) handleDownloaderResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dl := s.client.GetBatchDownloader()
	if dl == nil {
		s.writeError(w, http.StatusServiceUnavailable, "downloader not ready")
		return
	}
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" {
		s.writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	task := dl.GetTask(req.TaskID)
	if task == nil {
		s.writeError(w, http.StatusNotFound, "task not found")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var peer tg.InputPeerClass
	if task.ChatID != 0 {
		p, err := s.client.ResolveInputPeer(ctx, task.ChatID)
		if err == nil {
			peer = p
		}
	}

	if err := dl.ResumeTask(req.TaskID, peer); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (s *Server) handleDownloaderCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dl := s.client.GetBatchDownloader()
	if dl == nil {
		s.writeError(w, http.StatusServiceUnavailable, "downloader not ready")
		return
	}
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" {
		s.writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}
	if err := dl.CancelTask(req.TaskID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleDownloaderDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	dl := s.client.GetBatchDownloader()
	if dl == nil {
		s.writeError(w, http.StatusServiceUnavailable, "downloader not ready")
		return
	}
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" {
		s.writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}
	if err := dl.DeleteTask(req.TaskID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Multi Uploader Handlers (Up to 4GB) ---

func (s *Server) handleUploaderTasks(w http.ResponseWriter, r *http.Request) {
	up := s.client.GetMultiUploader()
	if up == nil {
		s.writeError(w, http.StatusServiceUnavailable, "uploader not ready")
		return
	}
	tasks := up.GetTasks()
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": tasks,
	})
}

// handleUploaderStart queues a local file from disk directly (supports up to 4GB files with zero browser upload lag).
func (s *Server) handleUploaderStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	up := s.client.GetMultiUploader()
	if up == nil {
		s.writeError(w, http.StatusServiceUnavailable, "uploader not ready")
		return
	}

	var req struct {
		FilePath     string `json:"file_path"`
		TargetChatID int64  `json:"target_chat_id"`
		Caption      string `json:"caption"`
		MediaType    string `json:"media_type"`
		ReplyToMsgID int    `json:"reply_to_msg_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	if req.FilePath == "" || req.TargetChatID == 0 {
		s.writeError(w, http.StatusBadRequest, "file_path and target_chat_id are required")
		return
	}

	chatTitle := fmt.Sprintf("Chat %d", req.TargetChatID)
	if chat, ok := s.state.GetChat(req.TargetChatID); ok {
		chatTitle = chat.Title
	}

	task, err := up.EnqueueUpload(
		req.FilePath,
		filepath.Base(req.FilePath),
		req.TargetChatID,
		chatTitle,
		req.Caption,
		req.MediaType,
		req.ReplyToMsgID,
		false,
		s.client.ResolveInputPeer,
	)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, task)
}

// handleUploaderUploadFile streams a multipart browser file directly to disk and queues it for MTProto chunking.
func (s *Server) handleUploaderUploadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	up := s.client.GetMultiUploader()
	if up == nil {
		s.writeError(w, http.StatusServiceUnavailable, "uploader not ready")
		return
	}

	// Parse multipart stream (up to 4GB buffer limit via disk)
	err := r.ParseMultipartForm(32 << 20) // 32MB RAM max, rest streams to disk
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("parse multipart: %v", err))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("read form file: %v", err))
		return
	}
	defer file.Close()

	chatIDStr := r.FormValue("target_chat_id")
	caption := r.FormValue("caption")
	mediaType := r.FormValue("media_type")
	replyToStr := r.FormValue("reply_to_msg_id")

	var targetChatID int64
	fmt.Sscanf(chatIDStr, "%d", &targetChatID)
	if targetChatID == 0 {
		s.writeError(w, http.StatusBadRequest, "target_chat_id is required")
		return
	}

	var replyToMsgID int
	fmt.Sscanf(replyToStr, "%d", &replyToMsgID)

	tempDir := up.GetTempDir()
	tempFilePath := filepath.Join(tempDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), header.Filename))

	destFile, err := os.OpenFile(tempFilePath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("create temp file: %v", err))
		return
	}

	// Stream directly to disk
	_, err = io.Copy(destFile, file)
	_ = destFile.Close()
	if err != nil {
		_ = os.Remove(tempFilePath)
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("save upload: %v", err))
		return
	}

	chatTitle := fmt.Sprintf("Chat %d", targetChatID)
	if chat, ok := s.state.GetChat(targetChatID); ok {
		chatTitle = chat.Title
	}

	task, err := up.EnqueueUpload(
		tempFilePath,
		header.Filename,
		targetChatID,
		chatTitle,
		caption,
		mediaType,
		replyToMsgID,
		true, // delete temp file when done
		s.client.ResolveInputPeer,
	)
	if err != nil {
		_ = os.Remove(tempFilePath)
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleUploaderCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	up := s.client.GetMultiUploader()
	if up == nil {
		s.writeError(w, http.StatusServiceUnavailable, "uploader not ready")
		return
	}
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TaskID == "" {
		s.writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}
	if err := up.CancelUpload(req.TaskID); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// --- Indexer & Cache Handlers ---

func (s *Server) handleIndexerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	idx := s.client.GetIndexerService()
	if idx == nil {
		s.writeError(w, http.StatusServiceUnavailable, "indexer not ready")
		return
	}

	var req struct {
		ChatID int64 `json:"chat_id"`
		Limit  int   `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChatID == 0 {
		s.writeError(w, http.StatusBadRequest, "chat_id is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	peer, err := s.client.ResolveInputPeer(ctx, req.ChatID)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to resolve peer: %v", err))
		return
	}

	chatTitle := fmt.Sprintf("Chat %d", req.ChatID)
	if chat, ok := s.state.GetChat(req.ChatID); ok {
		chatTitle = chat.Title
	}

	if err := idx.StartChatIndexing(req.ChatID, chatTitle, req.Limit, peer, s.client.ResolveInputPeer); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "indexing_started"})
}

func (s *Server) handleIndexerStatus(w http.ResponseWriter, r *http.Request) {
	idx := s.client.GetIndexerService()
	if idx == nil {
		s.writeError(w, http.StatusServiceUnavailable, "indexer not ready")
		return
	}
	s.writeJSON(w, http.StatusOK, idx.GetStatus())
}

func (s *Server) handleIndexerCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	idx := s.client.GetIndexerService()
	if idx == nil {
		s.writeError(w, http.StatusServiceUnavailable, "indexer not ready")
		return
	}
	idx.CancelIndexing()
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleIndexerSearch(w http.ResponseWriter, r *http.Request) {
	idx := s.client.GetIndexerService()
	if idx == nil {
		s.writeError(w, http.StatusServiceUnavailable, "indexer not ready")
		return
	}

	q := r.URL.Query().Get("q")
	chatIDStr := r.URL.Query().Get("chat_id")
	mediaOnlyStr := r.URL.Query().Get("media_only")
	limitStr := r.URL.Query().Get("limit")

	var chatID int64
	fmt.Sscanf(chatIDStr, "%d", &chatID)

	mediaOnly := (mediaOnlyStr == "true" || mediaOnlyStr == "1")

	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	results, err := idx.Search(q, chatID, mediaOnly, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":   q,
		"chat_id": chatID,
		"count":   len(results),
		"results": results,
	})
}

func (s *Server) handleIndexerExport(w http.ResponseWriter, r *http.Request) {
	idx := s.client.GetIndexerService()
	if idx == nil {
		s.writeError(w, http.StatusServiceUnavailable, "indexer not ready")
		return
	}

	chatIDStr := r.URL.Query().Get("chat_id")
	var chatID int64
	fmt.Sscanf(chatIDStr, "%d", &chatID)
	if chatID == 0 {
		s.writeError(w, http.StatusBadRequest, "chat_id is required")
		return
	}

	data, err := idx.ExportChat(chatID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	fileName := fmt.Sprintf("chat_export_%d_%d.json", chatID, time.Now().Unix())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	_, _ = w.Write(data)
}

func (s *Server) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	idx := s.client.GetIndexerService()
	if idx == nil {
		s.writeError(w, http.StatusServiceUnavailable, "indexer not ready")
		return
	}
	s.writeJSON(w, http.StatusOK, idx.GetCacheStats())
}

func (s *Server) handleCacheClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	idx := s.client.GetIndexerService()
	if idx == nil {
		s.writeError(w, http.StatusServiceUnavailable, "indexer not ready")
		return
	}

	var req struct {
		ClearMedia   bool `json:"clear_media"`
		ClearHistory bool `json:"clear_history"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := idx.ClearCache(req.ClearMedia, req.ClearHistory); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// --- Media Hub & Technical Inspector Handler ---

func (s *Server) handleMediaInspect(w http.ResponseWriter, r *http.Request) {
	hub := s.client.GetMediaHubService()
	if hub == nil {
		s.writeError(w, http.StatusServiceUnavailable, "media hub not ready")
		return
	}

	chatIDStr := r.URL.Query().Get("chat_id")
	msgIDStr := r.URL.Query().Get("message_id")

	var chatID int64
	var msgID int
	fmt.Sscanf(chatIDStr, "%d", &chatID)
	fmt.Sscanf(msgIDStr, "%d", &msgID)

	if chatID == 0 || msgID == 0 {
		s.writeError(w, http.StatusBadRequest, "chat_id and message_id are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	peer, err := s.client.ResolveInputPeer(ctx, chatID)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to resolve peer: %v", err))
		return
	}

	result, err := hub.InspectMessageMedia(ctx, chatID, msgID, peer)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}
