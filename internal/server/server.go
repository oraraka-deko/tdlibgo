package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"tdlibgo/internal/models"
	"tdlibgo/internal/state"
	"tdlibgo/internal/telegram"
)

// Server hosts HTTP REST endpoints, WebSocket connections, and serves the Web UI.
type Server struct {
	port   int
	state  *state.StateManager
	client *telegram.ClientController
	srv    *http.Server
	webFS  embed.FS
}

// NewServer initializes a new Server.
func NewServer(port int, s *state.StateManager, client *telegram.ClientController, webFS embed.FS) *Server {
	return &Server{
		port:   port,
		state:  s,
		client: client,
		webFS:  webFS,
	}
}

// Start boots the HTTP/WebSocket listener.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// REST API Endpoints
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/auth/send-code", s.handleSendCode)
	mux.HandleFunc("/api/auth/submit-code", s.handleSubmitCode)
	mux.HandleFunc("/api/auth/submit-password", s.handleSubmitPassword)
	mux.HandleFunc("/api/auth/logout", s.handleLogout)
	mux.HandleFunc("/api/chats", s.handleChats)
	mux.HandleFunc("/api/messages", s.handleMessages)
	mux.HandleFunc("/api/messages/send", s.handleSendMessage)
	mux.HandleFunc("/api/chats/read", s.handleReadChat)

	// Media Streaming & Download Endpoint
	mux.HandleFunc("/api/media", s.handleMedia)

	// Telegram Premium Global Post Search Endpoint
	mux.HandleFunc("/api/search/posts", s.handleSearchGlobalPosts)

	// WebSocket Endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	// Embedded Static Frontend
	webSub, err := fs.Sub(s.webFS, "web")
	if err != nil {
		return fmt.Errorf("failed to open embedded web directory: %w", err)
	}
	fileServer := http.FileServer(http.FS(webSub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		fileServer.ServeHTTP(w, r)
	})

	s.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: s.corsMiddleware(mux),
	}

	fmt.Printf("[HTTP] Telegram Web client listening at: http://localhost:%d\n", s.port)
	return s.srv.ListenAndServe()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	if s.srv != nil {
		return s.srv.Shutdown(ctx)
	}
	return nil
}

// corsMiddleware adds basic CORS headers.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

// handleState returns full state.
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.writeJSON(w, http.StatusOK, s.state.GetFullState())
}

// handleSendCode initiates login.
func (s *Server) handleSendCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Phone string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Phone != "" {
		s.state.SetPhone(req.Phone)
		s.client.SetPhone(req.Phone)
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "code_sent_pending",
		"auth":   s.state.GetAuthState(),
	})
}

// handleSubmitCode receives verification code.
func (s *Server) handleSubmitCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		s.writeError(w, http.StatusBadRequest, "invalid code payload")
		return
	}

	s.client.SubmitCode(req.Code)
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "submitted"})
}

// handleSubmitPassword receives 2FA cloud password.
func (s *Server) handleSubmitPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		s.writeError(w, http.StatusBadRequest, "invalid password payload")
		return
	}

	s.client.SubmitPassword(req.Password)
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "submitted"})
}

// handleLogout clears state.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.state.SetUser(nil)
	s.state.SetAuthState(models.AuthStateIdle, "", "", 0)
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// handleChats returns filtered chat list.
func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	folderID := -1
	if fStr := r.URL.Query().Get("folder_id"); fStr != "" {
		if id, err := strconv.Atoi(fStr); err == nil {
			folderID = id
		}
	}
	filterType := r.URL.Query().Get("type")

	chats := s.state.GetChats(folderID, filterType)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"chats": chats,
		"count": len(chats),
	})
}

// handleMessages returns messages for a chat with lazy loading.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	chatIDStr := r.URL.Query().Get("chat_id")
	if chatIDStr == "" {
		s.writeError(w, http.StatusBadRequest, "missing chat_id")
		return
	}
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid chat_id")
		return
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	offsetID := 0
	if oStr := r.URL.Query().Get("offset_id"); oStr != "" {
		if o, err := strconv.Atoi(oStr); err == nil {
			offsetID = o
		}
	}

	normChatID, _ := telegram.NormalizeChatID(chatID)
	messages := s.state.GetMessages(normChatID, limit, offsetID)
	if len(messages) == 0 && normChatID != chatID {
		messages = s.state.GetMessages(chatID, limit, offsetID)
	}

	// Fetch from Telegram if chat only has preview message (<= 1) or older messages requested
	if (len(messages) <= 1 || (offsetID > 0 && len(messages) < limit)) && s.state.GetAuthState().IsLoggedIn {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := s.client.FetchHistory(ctx, normChatID, limit, offsetID); err != nil {
			fmt.Printf("[SERVER] FetchHistory error for chat %d: %v\n", normChatID, err)
		}
		messages = s.state.GetMessages(normChatID, limit, offsetID)
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"chat_id":  normChatID,
		"messages": messages,
		"count":    len(messages),
	})
}

// handleSendMessage sends a message with optional reply_to_msg_id.
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		ChatID       int64  `json:"chat_id"`
		Text         string `json:"text"`
		ReplyToMsgID int    `json:"reply_to_msg_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChatID == 0 || strings.TrimSpace(req.Text) == "" {
		s.writeError(w, http.StatusBadRequest, "invalid chat_id or empty text")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	normChatID, _ := telegram.NormalizeChatID(req.ChatID)
	msg, err := s.client.SendMessage(ctx, normChatID, req.Text, req.ReplyToMsgID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, msg)
}

// handleMedia streams media files (photos, videos, audio, documents).
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	chatIDStr := r.URL.Query().Get("chat_id")
	msgIDStr := r.URL.Query().Get("message_id")
	if chatIDStr == "" || msgIDStr == "" {
		s.writeError(w, http.StatusBadRequest, "chat_id and message_id parameters are required")
		return
	}

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid chat_id")
		return
	}
	normChatID, _ := telegram.NormalizeChatID(chatID)
	msgID, err := strconv.Atoi(msgIDStr)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid message_id")
		return
	}

	isDownload := r.URL.Query().Get("download") == "1"

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Intercept to set response headers once filename and MIME type are known
	pipeReader, pipeWriter := io.Pipe()

	errCh := make(chan error, 1)
	var fileName, mimeType string

	go func() {
		defer pipeWriter.Close()
		var streamErr error
		fileName, mimeType, streamErr = s.client.DownloadMedia(ctx, normChatID, msgID, pipeWriter)
		if streamErr != nil {
			pipeWriter.CloseWithError(streamErr)
			errCh <- streamErr
			return
		}
		errCh <- nil
	}()

	// Read small header probe to ensure no immediate failure
	buf := make([]byte, 512)
	n, readErr := pipeReader.Read(buf)
	if readErr != nil && n == 0 {
		s.writeError(w, http.StatusNotFound, "media file could not be streamed")
		return
	}

	if mimeType == "" {
		mimeType = http.DetectContentType(buf[:n])
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "public, max-age=86400")

	disposition := "inline"
	if isDownload {
		disposition = "attachment"
	}
	if fileName != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, fileName))
	} else {
		w.Header().Set("Content-Disposition", disposition)
	}

	// Write the probed first chunk
	_, _ = w.Write(buf[:n])

	// Stream the remaining bytes
	_, _ = io.Copy(w, pipeReader)
}

// handleReadChat marks chat messages as read.
func (s *Server) handleReadChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		ChatID int64 `json:"chat_id"`
		MaxID  int   `json:"max_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChatID == 0 {
		s.writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	normChatID, _ := telegram.NormalizeChatID(req.ChatID)
	_ = s.client.MarkAsRead(ctx, normChatID, req.MaxID)
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "read"})
}

// handleSearchGlobalPosts searches public channel posts globally (Telegram Premium feature).
func (s *Server) handleSearchGlobalPosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		s.writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	limit := 20
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	offsetID := 0
	if oStr := r.URL.Query().Get("offset_id"); oStr != "" {
		if o, err := strconv.Atoi(oStr); err == nil {
			offsetID = o
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	posts, err := s.client.SearchGlobalPosts(ctx, query, offsetID, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"query": query,
		"count": len(posts),
		"posts": posts,
	})
}

// handleWebSocket manages real-time bi-directional messaging with web clients.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer c.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Send initial snapshot
	initialData := map[string]interface{}{
		"state": s.state.GetFullState(),
		"chats": s.state.GetChats(-1, ""),
	}
	initJSON, _ := json.Marshal(models.WSMessage{
		Type:    "initial_state",
		Payload: initialData,
	})
	_ = c.Write(ctx, websocket.MessageText, initJSON)

	subCh := s.state.Subscribe()
	defer s.state.Unsubscribe(subCh)

	// Forward state manager broadcasts to client
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-subCh:
				if !ok {
					return
				}
				data, err := json.Marshal(msg)
				if err != nil {
					continue
				}
				writeCtx, wCancel := context.WithTimeout(ctx, 3*time.Second)
				err = c.Write(writeCtx, websocket.MessageText, data)
				wCancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	// Read loop for client commands
	for {
		_, msgBytes, err := c.Read(ctx)
		if err != nil {
			break
		}

		var cmd struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(msgBytes, &cmd); err != nil {
			continue
		}

		switch cmd.Type {
		case "ping":
			_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"pong"}`))

		case "submit_code":
			var p struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(cmd.Payload, &p); err == nil && p.Code != "" {
				s.client.SubmitCode(p.Code)
			}

		case "submit_password":
			var p struct {
				Password string `json:"password"`
			}
			if err := json.Unmarshal(cmd.Payload, &p); err == nil && p.Password != "" {
				s.client.SubmitPassword(p.Password)
			}

		case "send_message":
			var p struct {
				ChatID       int64  `json:"chat_id"`
				Text         string `json:"text"`
				ReplyToMsgID int    `json:"reply_to_msg_id"`
			}
			if err := json.Unmarshal(cmd.Payload, &p); err == nil && p.ChatID != 0 && p.Text != "" {
				sendCtx, sCancel := context.WithTimeout(ctx, 10*time.Second)
				_, _ = s.client.SendMessage(sendCtx, p.ChatID, p.Text, p.ReplyToMsgID)
				sCancel()
			}

		case "read_chat":
			var p struct {
				ChatID int64 `json:"chat_id"`
				MaxID  int   `json:"max_id"`
			}
			if err := json.Unmarshal(cmd.Payload, &p); err == nil && p.ChatID != 0 {
				readCtx, rCancel := context.WithTimeout(ctx, 5*time.Second)
				_ = s.client.MarkAsRead(readCtx, p.ChatID, p.MaxID)
				rCancel()
			}
		}
	}
}
