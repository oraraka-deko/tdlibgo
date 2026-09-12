package telegram

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	pebbledb "github.com/cockroachdb/pebble"
	"github.com/go-faster/errors"
	boltstor "github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/contrib/pebble"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/log/logzap"
	gotdtg "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	qmessages "github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/telegram/updates"
	updhook "github.com/gotd/td/telegram/updates/hook"
	"github.com/gotd/td/tg"
	"go.etcd.io/bbolt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/time/rate"
	lj "gopkg.in/natefinch/lumberjack.v2"

	"tdlibgo/internal/models"
	"tdlibgo/internal/state"
)

// ClientController manages the gotd MTProto client lifecycle and auth channels.
type ClientController struct {
	appID   int
	appHash string
	phone   string
	state   *state.StateManager

	client          *gotdtg.Client
	api             *tg.Client
	peerDB          *pebble.PeerStorage
	updatesRecovery *updates.Manager

	codeChan     chan string
	passwordChan chan string

	mu        sync.RWMutex
	cancelRun context.CancelFunc
	running   bool
}

// NewClientController creates a new MTProto client manager.
func NewClientController(appID int, appHash, phone string, s *state.StateManager) *ClientController {
	return &ClientController{
		appID:        appID,
		appHash:      appHash,
		phone:        phone,
		state:        s,
		codeChan:     make(chan string, 10),
		passwordChan: make(chan string, 10),
	}
}

// sessionFolder formats the folder name based on digits of the phone number.
func sessionFolder(phone string) string {
	var out []rune
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			out = append(out, r)
		}
	}
	return "phone-" + string(out)
}

// Start launches the MTProto client and updates recovery in a background goroutine.
func (c *ClientController) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	c.cancelRun = cancel
	c.running = true
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			c.state.SetConnectionState(models.ConnDisconnected)
		}()

		for {
			select {
			case <-runCtx.Done():
				return
			default:
			}

			c.state.SetConnectionState(models.ConnConnecting)
			err := c.runClient(runCtx)
			if err != nil {
				if errors.Is(err, context.Canceled) || runCtx.Err() != nil {
					return
				}
				fmt.Printf("[MTProto] Client loop error: %v. Reconnecting in 3s...\n", err)
				c.state.SetConnectionState(models.ConnDisconnected)
				time.Sleep(3 * time.Second)
			}
		}
	}()

	return nil
}

// runClient sets up storage and executes the client connection.
func (c *ClientController) runClient(ctx context.Context) error {
	folder := sessionFolder(c.phone)
	sessionDir := filepath.Join("session", folder)
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		return errors.Wrap(err, "mkdir session dir")
	}

	logFilePath := filepath.Join(sessionDir, "log.jsonl")
	logWriter := zapcore.AddSync(&lj.Logger{
		Filename:   logFilePath,
		MaxBackups: 3,
		MaxSize:    5, // megabytes
		MaxAge:     7, // days
	})
	logCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		logWriter,
		zap.WarnLevel,
	)
	lg := zap.New(logCore)
	defer func() { _ = lg.Sync() }()

	sessionStorage := &gotdtg.FileSessionStorage{
		Path: filepath.Join(sessionDir, "session.json"),
	}

	pdb, err := pebbledb.Open(filepath.Join(sessionDir, "peers.pebble.db"), &pebbledb.Options{})
	if err != nil {
		return errors.Wrap(err, "open pebble db")
	}
	defer pdb.Close()

	peerDB := pebble.NewPeerStorage(pdb)
	c.peerDB = peerDB

	dispatcher := tg.NewUpdateDispatcher()

	// Register update dispatcher handlers
	c.registerUpdateHandlers(&dispatcher)

	updateHandler := storage.UpdateHook(dispatcher, peerDB)

	boltdb, err := bbolt.Open(filepath.Join(sessionDir, "updates.bolt.db"), 0666, nil)
	if err != nil {
		return errors.Wrap(err, "open bolt db")
	}
	defer boltdb.Close()

	updatesRecovery := updates.New(updates.Config{
		Handler: updateHandler,
		Logger:  logzap.New(lg.Named("updates.recovery")),
		Storage: boltstor.NewStateStorage(boltdb),
	})
	c.updatesRecovery = updatesRecovery

	waiter := floodwait.NewWaiter().WithCallback(func(ctx context.Context, wait floodwait.FloodWait) {
		fmt.Printf("[MTProto] FLOOD_WAIT: retry after %v\n", wait.Duration)
	})

	options := gotdtg.Options{
		Logger:         logzap.New(lg),
		SessionStorage: sessionStorage,
		UpdateHandler:  updatesRecovery,
		Middlewares: []gotdtg.Middleware{
			waiter,
			updhook.AffectedHook(updatesRecovery),
			ratelimit.New(rate.Every(time.Millisecond*100), 5),
		},
	}

	client := gotdtg.NewClient(c.appID, c.appHash, options)
	c.client = client
	c.api = client.API()

	authFlow := auth.NewFlow(c, auth.SendCodeOptions{})

	return waiter.Run(ctx, func(ctx context.Context) error {
		return client.Run(ctx, func(ctx context.Context) error {
			c.state.SetConnectionState(models.ConnConnecting)

			// Authenticate if needed
			if err := client.Auth().IfNecessary(ctx, authFlow); err != nil {
				c.state.SetAuthState(models.AuthStateError, err.Error(), "", 0)
				return errors.Wrap(err, "auth flow")
			}

			// User is authorized
			self, err := client.Self(ctx)
			if err != nil {
				return errors.Wrap(err, "fetch self")
			}

			var selfPhotoURL string
			if p, ok := self.Photo.(*tg.UserProfilePhoto); ok {
				selfPhotoURL = ExtractStrippedThumbURL(p.StrippedThumb)
			}

			userProfile := &models.UserProfile{
				ID:        self.ID,
				FirstName: self.FirstName,
				LastName:  self.LastName,
				Username:  self.Username,
				Phone:     self.Phone,
				PhotoURL:  selfPhotoURL,
				IsBot:     self.Bot,
			}
			c.state.SetUser(userProfile)
			c.state.SetAuthState(models.AuthStateReady, "", "", 0)
			c.state.SetConnectionState(models.ConnReady)

			name := self.FirstName
			if self.Username != "" {
				name = fmt.Sprintf("%s (@%s)", name, self.Username)
			}
			fmt.Printf("[AUTH] Successfully logged in as: %s (ID: %d)\n", name, self.ID)

			// Populate dialogs and entity cache
			go func() {
				fetchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := c.FetchDialogs(fetchCtx); err != nil {
					fmt.Printf("[MTProto] Initial dialogs fetch error: %v\n", err)
				}
			}()

			// Start update recovery loop
			return updatesRecovery.Run(ctx, c.api, self.ID, updates.AuthOptions{
				IsBot: self.Bot,
				OnStart: func(ctx context.Context) {
					c.state.SetConnectionState(models.ConnReady)
					fmt.Println("[MTProto] Updates listener active and running.")
				},
			})
		})
	})
}

// UserAuthenticator implementation for interactive auth

func (c *ClientController) Phone(ctx context.Context) (string, error) {
	c.state.SetAuthState(models.AuthStateSendingCode, "", "", 0)
	return c.phone, nil
}

func (c *ClientController) Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
	codeType := "App"
	if sentCode.Type != nil {
		codeType = fmt.Sprintf("%T", sentCode.Type)
		codeType = strings.TrimPrefix(codeType, "*tg.AuthSentCodeType")
	}

	timeout := sentCode.Timeout
	c.state.SetAuthState(models.AuthStateWaitingCode, "", codeType, timeout)

	fmt.Println("==================================================================")
	fmt.Printf("[AUTH] Telegram verification code sent via %s to %s!\n", codeType, c.phone)
	fmt.Println("[AUTH] Enter code in the Web App at http://localhost:8080 or type here:")
	fmt.Println("==================================================================")

	select {
	case code := <-c.codeChan:
		code = strings.TrimSpace(code)
		c.state.SetAuthState(models.AuthStateSendingCode, "", "", 0)
		return code, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *ClientController) Password(ctx context.Context) (string, error) {
	c.state.SetAuthState(models.AuthStateWaitingPassword, "", "", 0)

	fmt.Println("==================================================================")
	fmt.Printf("[AUTH] 2FA Cloud Password required for %s!\n", c.phone)
	fmt.Println("[AUTH] Enter password in Web App or type here:")
	fmt.Println("==================================================================")

	select {
	case pwd := <-c.passwordChan:
		pwd = strings.TrimSpace(pwd)
		c.state.SetAuthState(models.AuthStateSendingCode, "", "", 0)
		return pwd, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *ClientController) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	return nil
}

func (c *ClientController) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign up not supported on this account")
}

// SubmitCode receives verification code from REST, WebSocket, or console.
func (c *ClientController) SubmitCode(code string) {
	select {
	case c.codeChan <- code:
	default:
	}
}

// SubmitPassword receives 2FA password from REST, WebSocket, or console.
func (c *ClientController) SubmitPassword(password string) {
	select {
	case c.passwordChan <- password:
	default:
	}
}

// SetPhone updates phone in client controller.
func (c *ClientController) SetPhone(phone string) {
	c.mu.Lock()
	c.phone = phone
	c.mu.Unlock()
}

// registerUpdateHandlers sets up callbacks for real-time MTProto updates.
func (c *ClientController) registerUpdateHandlers(dispatcher *tg.UpdateDispatcher) {
	dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		c.cacheEntities(e)
		switch m := u.Message.(type) {
		case *tg.Message:
			msgModel := c.tlMessageToModel(m, e)
			c.state.AppendMessage(msgModel)
		case *tg.MessageService:
			srvModel := c.tlServiceMessageToModel(m, e)
			c.state.AppendMessage(srvModel)
		}
		return nil
	})

	dispatcher.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		c.cacheEntities(e)
		switch m := u.Message.(type) {
		case *tg.Message:
			msgModel := c.tlMessageToModel(m, e)
			c.state.AppendMessage(msgModel)
		case *tg.MessageService:
			srvModel := c.tlServiceMessageToModel(m, e)
			c.state.AppendMessage(srvModel)
		}
		return nil
	})

	dispatcher.OnEditMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditMessage) error {
		msg, ok := u.Message.(*tg.Message)
		if !ok {
			return nil
		}

		c.cacheEntities(e)
		m := c.tlMessageToModel(msg, e)
		c.state.UpdateMessage(m)
		return nil
	})

	dispatcher.OnEditChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditChannelMessage) error {
		msg, ok := u.Message.(*tg.Message)
		if !ok {
			return nil
		}

		c.cacheEntities(e)
		m := c.tlMessageToModel(msg, e)
		c.state.UpdateMessage(m)
		return nil
	})

	dispatcher.OnDeleteMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteMessages) error {
		c.state.DeleteMessages(0, u.Messages)
		return nil
	})

	dispatcher.OnDeleteChannelMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		c.state.DeleteMessages(u.ChannelID, u.Messages)
		return nil
	})

	dispatcher.OnUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateUserTyping) error {
		name := fmt.Sprintf("User %d", u.UserID)
		if user, ok := e.Users[u.UserID]; ok {
			name = user.FirstName
		} else if ent, ok := c.state.GetEntity(u.UserID); ok {
			name = ent.Title
		}
		c.state.SetTyping(u.UserID, name)
		return nil
	})

	dispatcher.OnUserStatus(func(ctx context.Context, e tg.Entities, u *tg.UpdateUserStatus) error {
		_, isOnline := u.Status.(*tg.UserStatusOnline)
		c.state.SetUserStatus(u.UserID, isOnline)
		return nil
	})
}

// cacheEntities stores peers from Updates into StateManager.
func (c *ClientController) cacheEntities(e tg.Entities) {
	for id, u := range e.Users {
		name := strings.TrimSpace(u.FirstName + " " + u.LastName)
		if name == "" {
			name = u.Username
		}
		if name == "" {
			name = fmt.Sprintf("User %d", id)
		}

		var photoURL string
		if p, ok := u.Photo.(*tg.UserProfilePhoto); ok {
			photoURL = ExtractStrippedThumbURL(p.StrippedThumb)
		}

		c.state.UpsertEntity(&models.EntityInfo{
			ID:         id,
			AccessHash: u.AccessHash,
			Type:       models.ChatTypeUser,
			Title:      name,
			Username:   u.Username,
			Phone:      u.Phone,
			PhotoURL:   photoURL,
		})
	}

	for id, ch := range e.Chats {
		var photoURL string
		if p, ok := ch.Photo.(*tg.ChatPhoto); ok {
			photoURL = ExtractStrippedThumbURL(p.StrippedThumb)
		}
		c.state.UpsertEntity(&models.EntityInfo{
			ID:       id,
			Type:     models.ChatTypeGroup,
			Title:    ch.Title,
			PhotoURL: photoURL,
		})
	}

	for id, ch := range e.Channels {
		chatType := models.ChatTypeChannel
		if ch.Megagroup {
			chatType = models.ChatTypeGroup
		}
		var photoURL string
		if p, ok := ch.Photo.(*tg.ChatPhoto); ok {
			photoURL = ExtractStrippedThumbURL(p.StrippedThumb)
		}
		c.state.UpsertEntity(&models.EntityInfo{
			ID:         id,
			AccessHash: ch.AccessHash,
			Type:       chatType,
			Title:      ch.Title,
			Username:   ch.Username,
			PhotoURL:   photoURL,
		})
	}
}

// tlMessageToModel maps a tg.Message to our Message struct.
func (c *ClientController) tlMessageToModel(msg *tg.Message, e tg.Entities) *models.Message {
	chatID := c.extractPeerID(msg.PeerID)
	senderID := c.extractPeerID(msg.FromID)
	if senderID == 0 {
		senderID = chatID
	}

	senderName := ""
	if user, ok := e.Users[senderID]; ok {
		senderName = strings.TrimSpace(user.FirstName + " " + user.LastName)
	} else if ent, ok := c.state.GetEntity(senderID); ok {
		senderName = ent.Title
	}

	text := msg.Message
	if text == "" && msg.Media != nil {
		text = InspectMediaText(msg.Media)
	}

	media := ExtractMessageMedia(msg, chatID)

	// Reply extraction
	var replyToMsgID int
	var replyToSender string
	var replyToText string
	if replyToClass, ok := msg.GetReplyTo(); ok {
		if r, ok := replyToClass.(*tg.MessageReplyHeader); ok {
			replyToMsgID = r.ReplyToMsgID
			if history := c.state.GetMessages(chatID, 50, 0); len(history) > 0 {
				for _, m := range history {
					if m.ID == replyToMsgID {
						replyToSender = m.SenderName
						replyToText = m.Text
						break
					}
				}
			}
		}
	}

	// Forward extraction
	var forwardFrom string
	var forwardDate time.Time
	var forwardPostID int
	if f, ok := msg.GetFwdFrom(); ok {
		if f.FromName != "" {
			forwardFrom = f.FromName
		} else if f.FromID != nil {
			fwdID := c.extractPeerID(f.FromID)
			if ent, ok := c.state.GetEntity(fwdID); ok {
				forwardFrom = ent.Title
			} else if user, ok := e.Users[fwdID]; ok {
				forwardFrom = strings.TrimSpace(user.FirstName + " " + user.LastName)
			} else if ch, ok := e.Channels[fwdID]; ok {
				forwardFrom = ch.Title
			}
		}
		forwardDate = time.Unix(int64(f.Date), 0)
		forwardPostID = f.ChannelPost
	}

	status := "sent"
	if msg.Out {
		status = "read"
	}

	return &models.Message{
		ID:            msg.ID,
		ChatID:        chatID,
		SenderID:      senderID,
		SenderName:    senderName,
		Text:          text,
		Date:          time.Unix(int64(msg.Date), 0),
		Out:           msg.Out,
		ReplyToMsgID:  replyToMsgID,
		ReplyToSender: replyToSender,
		ReplyToText:   replyToText,
		ForwardFrom:   forwardFrom,
		ForwardDate:   forwardDate,
		ForwardPostID: forwardPostID,
		Media:         media,
		EditDate:      time.Unix(int64(msg.EditDate), 0),
		Status:        status,
	}
}

// tlServiceMessageToModel maps a tg.MessageService to a user-friendly Message struct.
func (c *ClientController) tlServiceMessageToModel(msg *tg.MessageService, e tg.Entities) *models.Message {
	chatID := c.extractPeerID(msg.PeerID)
	senderID := c.extractPeerID(msg.FromID)
	if senderID == 0 {
		senderID = chatID
	}

	text := "Service notification"
	if msg.Action != nil {
		switch a := msg.Action.(type) {
		case *tg.MessageActionChatEditPhoto:
			text = "Channel photo updated"
		case *tg.MessageActionChatEditTitle:
			text = fmt.Sprintf("Channel title changed to %q", a.Title)
		case *tg.MessageActionChannelCreate:
			text = fmt.Sprintf("Channel %q created", a.Title)
		case *tg.MessageActionChatCreate:
			text = fmt.Sprintf("Group %q created", a.Title)
		case *tg.MessageActionPinMessage:
			text = "Pinned a message"
		case *tg.MessageActionChatAddUser:
			text = "User joined the chat"
		case *tg.MessageActionChatDeleteUser:
			text = "User left the chat"
		case *tg.MessageActionCustomAction:
			text = a.Message
		}
	}

	return &models.Message{
		ID:        msg.ID,
		ChatID:    chatID,
		SenderID:  senderID,
		Text:      text,
		Date:      time.Unix(int64(msg.Date), 0),
		Out:       msg.Out,
		Status:    "sent",
		IsService: true,
	}
}

// extractPeerID retrieves the int64 ID from a PeerClass.
func (c *ClientController) extractPeerID(peer tg.PeerClass) int64 {
	if peer == nil {
		return 0
	}
	switch p := peer.(type) {
	case *tg.PeerUser:
		return p.UserID
	case *tg.PeerChat:
		return p.ChatID
	case *tg.PeerChannel:
		return p.ChannelID
	}
	return 0
}

// FetchDialogs loads conversations from Telegram.
func (c *ClientController) FetchDialogs(ctx context.Context) error {
	if c.api == nil {
		return errors.New("client api not initialized")
	}

	res, err := c.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		Limit:      100,
		OffsetPeer: &tg.InputPeerEmpty{},
	})
	if err != nil {
		return errors.Wrap(err, "get dialogs")
	}

	mod, ok := res.AsModified()
	if !ok {
		return nil
	}

	// Cache Users
	for _, uClass := range mod.GetUsers() {
		u, ok := uClass.(*tg.User)
		if !ok {
			continue
		}
		name := strings.TrimSpace(u.FirstName + " " + u.LastName)
		if name == "" {
			name = u.Username
		}
		if name == "" {
			name = fmt.Sprintf("User %d", u.ID)
		}
		chatType := models.ChatTypeUser
		if u.Bot {
			chatType = models.ChatTypeBot
		}

		var photoURL string
		if p, ok := u.Photo.(*tg.UserProfilePhoto); ok {
			photoURL = ExtractStrippedThumbURL(p.StrippedThumb)
		}

		c.state.UpsertEntity(&models.EntityInfo{
			ID:         u.ID,
			AccessHash: u.AccessHash,
			Type:       chatType,
			Title:      name,
			Username:   u.Username,
			Phone:      u.Phone,
			PhotoURL:   photoURL,
		})
	}

	// Cache Chats & Channels
	for _, cClass := range mod.GetChats() {
		switch ch := cClass.(type) {
		case *tg.Chat:
			var photoURL string
			if p, ok := ch.Photo.(*tg.ChatPhoto); ok {
				photoURL = ExtractStrippedThumbURL(p.StrippedThumb)
			}
			c.state.UpsertEntity(&models.EntityInfo{
				ID:       ch.ID,
				Type:     models.ChatTypeGroup,
				Title:    ch.Title,
				PhotoURL: photoURL,
			})
		case *tg.Channel:
			chatType := models.ChatTypeChannel
			if ch.Megagroup {
				chatType = models.ChatTypeGroup
			}
			var photoURL string
			if p, ok := ch.Photo.(*tg.ChatPhoto); ok {
				photoURL = ExtractStrippedThumbURL(p.StrippedThumb)
			}
			c.state.UpsertEntity(&models.EntityInfo{
				ID:         ch.ID,
				AccessHash: ch.AccessHash,
				Type:       chatType,
				Title:      ch.Title,
				Username:   ch.Username,
				PhotoURL:   photoURL,
			})
		}
	}

	// Index Messages by ID
	messagesMap := make(map[int]*tg.Message)
	for _, mClass := range mod.GetMessages() {
		if m, ok := mClass.(*tg.Message); ok {
			messagesMap[m.ID] = m
		}
	}

	// Process Dialogs
	for _, dClass := range mod.GetDialogs() {
		d, ok := dClass.(*tg.Dialog)
		if !ok {
			continue
		}

		chatID := c.extractPeerID(d.Peer)
		if chatID == 0 {
			continue
		}

		entity, _ := c.state.GetEntity(chatID)
		title := fmt.Sprintf("Chat %d", chatID)
		username := ""
		chatType := models.ChatTypeUser
		var accessHash int64
		var photoURL string

		if entity != nil {
			title = entity.Title
			username = entity.Username
			chatType = entity.Type
			accessHash = entity.AccessHash
			photoURL = entity.PhotoURL
		}

		if accessHash == 0 && c.peerDB != nil {
			if p, err := storage.FindPeer(ctx, c.peerDB, d.Peer); err == nil {
				if p.User != nil {
					accessHash = p.User.AccessHash
					if title == fmt.Sprintf("Chat %d", chatID) {
						title = strings.TrimSpace(p.User.FirstName + " " + p.User.LastName)
						if title == "" {
							title = p.User.Username
						}
					}
					if username == "" {
						username = p.User.Username
					}
				} else if p.Channel != nil {
					accessHash = p.Channel.AccessHash
					if title == fmt.Sprintf("Chat %d", chatID) {
						title = p.Channel.Title
					}
					if username == "" {
						username = p.Channel.Username
					}
				}
			}
		}

		topMsgText := ""
		topMsgSender := ""
		lastDate := time.Now()

		if topMsg, ok := messagesMap[d.TopMessage]; ok {
			mModel := c.tlMessageToModel(topMsg, tg.Entities{})
			topMsgText = mModel.Text
			lastDate = mModel.Date
			senderID := c.extractPeerID(topMsg.FromID)
			if senderEnt, ok := c.state.GetEntity(senderID); ok {
				topMsgSender = senderEnt.Title
				mModel.SenderName = topMsgSender
			}
			c.state.AppendMessage(mModel)
		}

		c.state.UpsertChat(&models.Chat{
			ID:               chatID,
			Type:             chatType,
			Title:            title,
			Username:         username,
			UnreadCount:      d.UnreadCount,
			Pinned:           d.Pinned,
			FolderID:         d.FolderID,
			TopMessageID:     d.TopMessage,
			TopMessageText:   topMsgText,
			TopMessageSender: topMsgSender,
			LastMessageDate:  lastDate,
			PhotoURL:         photoURL,
			AccessHash:       accessHash,
		})
	}

	return nil
}

// FetchHistory loads messages for a specific chat.
func (c *ClientController) FetchHistory(ctx context.Context, chatID int64, limit, offsetID int) error {
	if c.api == nil {
		return errors.New("client api not initialized")
	}

	normChatID, _ := NormalizeChatID(chatID)
	inputPeer, err := c.resolveInputPeer(ctx, normChatID)
	if err != nil {
		return err
	}

	if limit <= 0 {
		limit = 50
	}

	res, err := c.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:     inputPeer,
		OffsetID: offsetID,
		Limit:    limit,
	})
	if err != nil {
		return errors.Wrap(err, "get history")
	}

	mod, ok := res.AsModified()
	if !ok {
		return nil
	}

	entities := tg.Entities{
		Users:    make(map[int64]*tg.User),
		Chats:    make(map[int64]*tg.Chat),
		Channels: make(map[int64]*tg.Channel),
	}

	for _, uClass := range mod.GetUsers() {
		if u, ok := uClass.(*tg.User); ok {
			entities.Users[u.ID] = u
			name := strings.TrimSpace(u.FirstName + " " + u.LastName)
			if name == "" {
				name = u.Username
			}
			var photoURL string
			if p, ok := u.Photo.(*tg.UserProfilePhoto); ok {
				photoURL = ExtractStrippedThumbURL(p.StrippedThumb)
			}
			c.state.UpsertEntity(&models.EntityInfo{
				ID:         u.ID,
				AccessHash: u.AccessHash,
				Type:       models.ChatTypeUser,
				Title:      name,
				Username:   u.Username,
				PhotoURL:   photoURL,
			})
		}
	}

	for _, cClass := range mod.GetChats() {
		switch ch := cClass.(type) {
		case *tg.Chat:
			entities.Chats[ch.ID] = ch
			var photoURL string
			if p, ok := ch.Photo.(*tg.ChatPhoto); ok {
				photoURL = ExtractStrippedThumbURL(p.StrippedThumb)
			}
			c.state.UpsertEntity(&models.EntityInfo{
				ID:       ch.ID,
				Type:     models.ChatTypeGroup,
				Title:    ch.Title,
				PhotoURL: photoURL,
			})
		case *tg.Channel:
			entities.Channels[ch.ID] = ch
			var photoURL string
			if p, ok := ch.Photo.(*tg.ChatPhoto); ok {
				photoURL = ExtractStrippedThumbURL(p.StrippedThumb)
			}
			c.state.UpsertEntity(&models.EntityInfo{
				ID:         ch.ID,
				AccessHash: ch.AccessHash,
				Type:       models.ChatTypeChannel,
				Title:      ch.Title,
				Username:   ch.Username,
				PhotoURL:   photoURL,
			})
		}
	}

	messages := mod.GetMessages()
	for _, mClass := range messages {
		switch m := mClass.(type) {
		case *tg.Message:
			msgModel := c.tlMessageToModel(m, entities)
			c.state.AppendMessage(msgModel)
		case *tg.MessageService:
			srvModel := c.tlServiceMessageToModel(m, entities)
			c.state.AppendMessage(srvModel)
		}
	}

	return nil
}

// SendMessage dispatches a text message to a chat with optional reply ID.
func (c *ClientController) SendMessage(ctx context.Context, chatID int64, text string, replyToMsgID int) (*models.Message, error) {
	if c.api == nil {
		return nil, errors.New("client api not initialized")
	}

	normChatID, _ := NormalizeChatID(chatID)
	inputPeer, err := c.resolveInputPeer(ctx, normChatID)
	if err != nil {
		return nil, err
	}

	randomID := rand.Int63()
	req := &tg.MessagesSendMessageRequest{
		Peer:     inputPeer,
		Message:  text,
		RandomID: randomID,
	}
	if replyToMsgID > 0 {
		req.SetReplyTo(&tg.InputReplyToMessage{
			ReplyToMsgID: replyToMsgID,
		})
	}

	updatesClass, err := c.api.MessagesSendMessage(ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "send message")
	}

	msgID := int(randomID & 0x7FFFFFFF)
	var date time.Time = time.Now()

	switch u := updatesClass.(type) {
	case *tg.Updates:
		for _, upd := range u.Updates {
			switch m := upd.(type) {
			case *tg.UpdateNewMessage:
				if msg, ok := m.Message.(*tg.Message); ok {
					msgID = msg.ID
					date = time.Unix(int64(msg.Date), 0)
				}
			case *tg.UpdateNewChannelMessage:
				if msg, ok := m.Message.(*tg.Message); ok {
					msgID = msg.ID
					date = time.Unix(int64(msg.Date), 0)
				}
			}
		}
	case *tg.UpdatesCombined:
		for _, upd := range u.Updates {
			switch m := upd.(type) {
			case *tg.UpdateNewMessage:
				if msg, ok := m.Message.(*tg.Message); ok {
					msgID = msg.ID
					date = time.Unix(int64(msg.Date), 0)
				}
			case *tg.UpdateNewChannelMessage:
				if msg, ok := m.Message.(*tg.Message); ok {
					msgID = msg.ID
					date = time.Unix(int64(msg.Date), 0)
				}
			}
		}
	case *tg.UpdateShortSentMessage:
		msgID = u.ID
		date = time.Unix(int64(u.Date), 0)
	case *tg.UpdateShortMessage:
		msgID = u.ID
		date = time.Unix(int64(u.Date), 0)
	case *tg.UpdateShortChatMessage:
		msgID = u.ID
		date = time.Unix(int64(u.Date), 0)
	}

	senderName := "You"
	var senderID int64
	if u := c.state.GetUser(); u != nil {
		senderID = u.ID
		senderName = u.FirstName
	}

	msgModel := &models.Message{
		ID:           msgID,
		ChatID:       normChatID,
		SenderID:     senderID,
		SenderName:   senderName,
		Text:         text,
		Date:         date,
		Out:          true,
		ReplyToMsgID: replyToMsgID,
		Status:       "sent",
	}

	c.state.AppendMessage(msgModel)
	return msgModel, nil
}

// DownloadMedia streams a message's media file directly to an io.Writer.
func (c *ClientController) DownloadMedia(ctx context.Context, chatID int64, messageID int, w io.Writer) (filename, mimeType string, err error) {
	if c.api == nil {
		return "", "", errors.New("client api not initialized")
	}

	normChatID, _ := NormalizeChatID(chatID)
	inputPeer, err := c.resolveInputPeer(ctx, normChatID)
	if err != nil {
		return "", "", err
	}

	var tgMsg *tg.Message
	switch p := inputPeer.(type) {
	case *tg.InputPeerChannel:
		res, err := c.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}},
		})
		if err != nil {
			return "", "", errors.Wrap(err, "get channel message")
		}
		if mod, ok := res.AsModified(); ok && len(mod.GetMessages()) > 0 {
			tgMsg, _ = mod.GetMessages()[0].(*tg.Message)
		}
	default:
		res, err := c.api.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}})
		if err != nil {
			return "", "", errors.Wrap(err, "get message")
		}
		if mod, ok := res.AsModified(); ok && len(mod.GetMessages()) > 0 {
			tgMsg, _ = mod.GetMessages()[0].(*tg.Message)
		}
	}

	if tgMsg == nil || tgMsg.Media == nil {
		return "", "", errors.New("message or media attachment not found")
	}

	file, ok := qmessages.Elem{Msg: tgMsg}.File()
	if !ok {
		return "", "", errors.New("file location could not be resolved from message media")
	}

	d := downloader.NewDownloader()
	_, err = d.Download(c.api, file.Location).Stream(ctx, w)
	if err != nil {
		return file.Name, file.MIMEType, errors.Wrap(err, "stream download")
	}

	return file.Name, file.MIMEType, nil
}

// MarkAsRead sends read history receipt.
func (c *ClientController) MarkAsRead(ctx context.Context, chatID int64, maxID int) error {
	if c.api == nil {
		return errors.New("client api not initialized")
	}

	normChatID, _ := NormalizeChatID(chatID)
	inputPeer, err := c.resolveInputPeer(ctx, normChatID)
	if err != nil {
		return err
	}

	switch p := inputPeer.(type) {
	case *tg.InputPeerChannel:
		_, err = c.api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{
			Channel: &tg.InputChannel{
				ChannelID:  p.ChannelID,
				AccessHash: p.AccessHash,
			},
			MaxID: maxID,
		})
	default:
		_, err = c.api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{
			Peer:  inputPeer,
			MaxID: maxID,
		})
	}

	if err == nil {
		if chat, ok := c.state.GetChat(chatID); ok {
			chat.UnreadCount = 0
			c.state.UpsertChat(chat)
		}
	}

	return err
}

// NormalizeChatID converts negative channel/supergroup/chat IDs (e.g. -1004406102499 or -12345)
// into standard positive MTProto IDs and returns whether it is a channel.
func NormalizeChatID(chatID int64) (normID int64, isChannel bool) {
	if chatID < 0 {
		s := strconv.FormatInt(chatID, 10)
		if strings.HasPrefix(s, "-100") {
			if id, err := strconv.ParseInt(s[4:], 10, 64); err == nil {
				return id, true
			}
		}
		return -chatID, false
	}
	return chatID, false
}

// resolveInputPeer constructs a valid InputPeerClass for the given chat ID with AccessHash.
func (c *ClientController) resolveInputPeer(ctx context.Context, chatID int64) (tg.InputPeerClass, error) {
	normID, isChannel := NormalizeChatID(chatID)

	// 1. Try Pebble peer storage first (contains permanent AccessHashes for users and channels)
	if c.peerDB != nil {
		if isChannel {
			if p, err := storage.FindPeer(ctx, c.peerDB, &tg.PeerChannel{ChannelID: normID}); err == nil {
				return p.AsInputPeer(), nil
			}
		}

		if p, err := storage.FindPeer(ctx, c.peerDB, &tg.PeerUser{UserID: normID}); err == nil {
			return p.AsInputPeer(), nil
		}

		if p, err := storage.FindPeer(ctx, c.peerDB, &tg.PeerChannel{ChannelID: normID}); err == nil {
			return p.AsInputPeer(), nil
		}

		if p, err := storage.FindPeer(ctx, c.peerDB, &tg.PeerChat{ChatID: normID}); err == nil {
			return p.AsInputPeer(), nil
		}
	}

	// 2. Try In-Memory State Chats
	for _, id := range []int64{normID, chatID} {
		if chat, ok := c.state.GetChat(id); ok {
			switch chat.Type {
			case models.ChatTypeUser, models.ChatTypeBot:
				if chat.AccessHash != 0 {
					return &tg.InputPeerUser{
						UserID:     normID,
						AccessHash: chat.AccessHash,
					}, nil
				}
			case models.ChatTypeChannel:
				if chat.AccessHash != 0 {
					return &tg.InputPeerChannel{
						ChannelID:  normID,
						AccessHash: chat.AccessHash,
					}, nil
				}
			case models.ChatTypeGroup:
				if chat.AccessHash != 0 {
					return &tg.InputPeerChannel{
						ChannelID:  normID,
						AccessHash: chat.AccessHash,
					}, nil
				}
				return &tg.InputPeerChat{
					ChatID: normID,
				}, nil
			}
		}
	}

	// 3. Try In-Memory State Entities
	for _, id := range []int64{normID, chatID} {
		if ent, ok := c.state.GetEntity(id); ok {
			switch ent.Type {
			case models.ChatTypeUser, models.ChatTypeBot:
				if ent.AccessHash != 0 {
					return &tg.InputPeerUser{
						UserID:     normID,
						AccessHash: ent.AccessHash,
					}, nil
				}
			case models.ChatTypeChannel:
				if ent.AccessHash != 0 {
					return &tg.InputPeerChannel{
						ChannelID:  normID,
						AccessHash: ent.AccessHash,
					}, nil
				}
			case models.ChatTypeGroup:
				if ent.AccessHash != 0 {
					return &tg.InputPeerChannel{
						ChannelID:  normID,
						AccessHash: ent.AccessHash,
					}, nil
				}
				return &tg.InputPeerChat{
					ChatID: normID,
				}, nil
			}
		}
	}

	// 4. Try resolving via username if available in chat or entity
	var username string
	if chat, ok := c.state.GetChat(normID); ok && chat.Username != "" {
		username = chat.Username
	} else if ent, ok := c.state.GetEntity(normID); ok && ent.Username != "" {
		username = ent.Username
	}

	if username != "" && c.api != nil {
		cleanUser := strings.TrimPrefix(username, "@")
		res, err := c.api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
			Username: cleanUser,
		})
		if err == nil && res != nil {
			for _, uClass := range res.Users {
				if u, ok := uClass.(*tg.User); ok && u.ID == normID {
					c.state.UpsertEntity(&models.EntityInfo{
						ID:         u.ID,
						AccessHash: u.AccessHash,
						Type:       models.ChatTypeUser,
						Title:      strings.TrimSpace(u.FirstName + " " + u.LastName),
						Username:   u.Username,
						Phone:      u.Phone,
					})
					return &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}, nil
				}
			}
			for _, chClass := range res.Chats {
				if ch, ok := chClass.(*tg.Channel); ok && ch.ID == normID {
					c.state.UpsertEntity(&models.EntityInfo{
						ID:         ch.ID,
						AccessHash: ch.AccessHash,
						Type:       models.ChatTypeChannel,
						Title:      ch.Title,
						Username:   ch.Username,
					})
					return &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("unable to resolve input peer for chat id %d (normalized %d)", chatID, normID)
}
