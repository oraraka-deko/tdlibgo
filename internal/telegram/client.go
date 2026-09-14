package telegram

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math/rand"
	"net/http"
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
	lj "gopkg.in/natefinch/lumberjack.v2"

	"github.com/iyear/tdl/core/dcpool"
	"github.com/iyear/tdl/core/tmedia"

	"tdlibgo/internal/logger"
	"tdlibgo/internal/models"
	"tdlibgo/internal/services"
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
	pool            dcpool.Pool
	peerDB          *pebble.PeerStorage
	updatesRecovery *updates.Manager

	uploader        *services.UploaderService
	downloader      *services.DownloaderService
	syncer          *services.SyncerService
	batchDownloader *services.BatchDownloaderService
	multiUploader   *services.MultiUploaderEngine
	indexerService  *services.IndexerService
	mediaHubService *services.MediaHubService
	transcriber     *services.TranscriberService

	codeChan     chan string
	passwordChan chan string

	mu        sync.RWMutex
	cancelRun context.CancelFunc
	running   bool
}

// NewClientController creates a new MTProto client manager.
func NewClientController(appID int, appHash, phone string, s *state.StateManager) *ClientController {
	c := &ClientController{
		appID:        appID,
		appHash:      appHash,
		phone:        phone,
		state:        s,
		codeChan:     make(chan string, 10),
		passwordChan: make(chan string, 10),
	}
	if s != nil {
		c.transcriber = services.NewTranscriberService(s.GetHistoryDB())
	} else {
		c.transcriber = services.NewTranscriberService(nil)
	}
	return c
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

			// Establish dedicated multi-DC connection pool from tdl/core
			pool := dcpool.NewPool(client, 8)
			c.pool = pool
			defer pool.Close()
			logger.MTProto("Dedicated tdl/core multi-DC connection pool active (up to 8 parallel TCP connections per DC).")

			// Initialize worker services with dedicated multi-DC data pool
			c.uploader = services.NewUploaderService(pool, c.api)
			c.downloader = services.NewDownloaderService(pool, filepath.Join(sessionDir, "media_cache"))
			c.batchDownloader = services.NewBatchDownloaderService(pool, c.api, c.state)
			c.multiUploader = services.NewMultiUploaderEngine(pool, c.api, c.state, filepath.Join(sessionDir, "upload_temp"))
			c.indexerService = services.NewIndexerService(c.api, c.state.GetHistoryDB(), c.state, filepath.Join(sessionDir, "media_cache"), filepath.Join(sessionDir, "avatars"), filepath.Join(sessionDir, "history.db"))
			c.mediaHubService = services.NewMediaHubService(c.api)
			if c.transcriber != nil && c.state.GetHistoryDB() != nil {
				c.transcriber.SetHistoryDB(c.state.GetHistoryDB())
			}

			// Populate initial dialogs and entity cache synchronously so chats & peers are immediately ready
			fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			if err := c.FetchDialogs(fetchCtx); err != nil {
				logger.Warn("MTProto", "Initial dialogs fetch error: %v", err)
			}
			cancel()

			// Start background keep-alive and history syncer if local DB is present
			if c.state.GetHistoryDB() != nil {
				c.syncer = services.NewSyncerService(c, c.state, c.state.GetHistoryDB())
				c.syncer.Start(ctx)
			}

			// Start update recovery loop
			return updatesRecovery.Run(ctx, c.api, self.ID, updates.AuthOptions{
				IsBot: self.Bot,
				OnStart: func(ctx context.Context) {
					c.state.SetConnectionState(models.ConnReady)
					logger.MTProto("Updates listener active and running.")
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

	dispatcher.OnMessageReactions(func(ctx context.Context, e tg.Entities, u *tg.UpdateMessageReactions) error {
		peerID := c.extractPeerID(u.Peer)
		if peerID == 0 {
			return nil
		}
		var reactions []models.ReactionCount
		for _, rc := range u.Reactions.Results {
			var emoticon string
			switch r := rc.Reaction.(type) {
			case *tg.ReactionEmoji:
				emoticon = r.Emoticon
			case *tg.ReactionCustomEmoji:
				emoticon = fmt.Sprintf("custom:%d", r.DocumentID)
			case *tg.ReactionPaid:
				emoticon = "⭐"
			}
			if emoticon != "" {
				chosen := rc.Flags.Has(0) || rc.ChosenOrder > 0
				reactions = append(reactions, models.ReactionCount{
					Reaction: emoticon,
					Count:    rc.Count,
					Chosen:   chosen,
				})
			}
		}
		c.state.UpdateMessageReactions(peerID, u.MsgID, reactions)
		return nil
	})
}

// parseUserStatus formats tg.UserStatusClass into a friendly status string and online flag.
func parseUserStatus(status tg.UserStatusClass) (string, bool) {
	if status == nil {
		return "", false
	}
	switch s := status.(type) {
	case *tg.UserStatusOnline:
		return "online", true
	case *tg.UserStatusOffline:
		t := time.Unix(int64(s.WasOnline), 0)
		now := time.Now()
		diff := now.Sub(t)
		if diff < time.Minute {
			return "last seen just now", false
		} else if diff < time.Hour {
			return fmt.Sprintf("last seen %d min ago", int(diff.Minutes())), false
		} else if diff < 24*time.Hour && t.Day() == now.Day() {
			return fmt.Sprintf("last seen today at %02d:%02d", t.Hour(), t.Minute()), false
		} else if diff < 48*time.Hour {
			return fmt.Sprintf("last seen yesterday at %02d:%02d", t.Hour(), t.Minute()), false
		}
		return fmt.Sprintf("last seen %s", t.Format("Jan 02")), false
	case *tg.UserStatusRecently:
		return "last seen recently", false
	case *tg.UserStatusLastWeek:
		return "last seen within a week", false
	case *tg.UserStatusLastMonth:
		return "last seen within a month", false
	default:
		return "", false
	}
}

// parseEmojiStatus extracts custom emoji status or badge identifier.
func parseEmojiStatus(es tg.EmojiStatusClass) string {
	if es == nil {
		return ""
	}
	switch s := es.(type) {
	case *tg.EmojiStatusCollectible:
		if s.Title != "" {
			return s.Title
		}
		return "collectible"
	case *tg.EmojiStatus:
		return "status"
	default:
		return ""
	}
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

		statusText, isOnline := parseUserStatus(u.Status)

		c.state.UpsertEntity(&models.EntityInfo{
			ID:         id,
			AccessHash: u.AccessHash,
			Type:       models.ChatTypeUser,
			Title:      name,
			Username:   u.Username,
			Phone:      u.Phone,
			PhotoURL:   photoURL,
			StatusText: statusText,
			IsOnline:   isOnline,
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
func (c *ClientController) tlMessageToModel(msg *tg.Message, e tg.Entities) (model *models.Message) {
	if msg == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			model = &models.Message{
				ID:     msg.ID,
				ChatID: c.extractPeerID(msg.PeerID),
				Text:   msg.Message,
				Date:   time.Unix(int64(msg.Date), 0),
				Out:    msg.Out,
				Status: "sent",
			}
		}
	}()

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

	var reactions []models.ReactionCount
	if msg.Reactions.Results != nil {
		for _, rc := range msg.Reactions.Results {
			var emoticon string
			switch r := rc.Reaction.(type) {
			case *tg.ReactionEmoji:
				emoticon = r.Emoticon
			case *tg.ReactionCustomEmoji:
				emoticon = fmt.Sprintf("custom:%d", r.DocumentID)
			case *tg.ReactionPaid:
				emoticon = "⭐"
			}
			if emoticon != "" {
				chosen := rc.Flags.Has(0) || rc.ChosenOrder > 0
				reactions = append(reactions, models.ReactionCount{
					Reaction: emoticon,
					Count:    rc.Count,
					Chosen:   chosen,
				})
			}
		}
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
		Reactions:     reactions,
		EditDate:      time.Unix(int64(msg.EditDate), 0),
		Views:         msg.Views,
		Forwards:      msg.Forwards,
		NoForwards:    msg.Noforwards,
		Status:        status,
	}
}

// tlServiceMessageToModel maps a tg.MessageService to a user-friendly Message struct.
func (c *ClientController) tlServiceMessageToModel(msg *tg.MessageService, e tg.Entities) (model *models.Message) {
	if msg == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			model = nil
		}
	}()

	chatID := c.extractPeerID(msg.PeerID)
	senderID := c.extractPeerID(msg.FromID)
	if senderID == 0 {
		senderID = chatID
	}

	text := "Service notification"
	var starGiftInfo *models.StarGiftInfo

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
		case *tg.MessageActionStarGift:
			starGiftInfo = c.extractStarGiftFromAction(a, e, msg.Out)
			if starGiftInfo != nil {
				fromStr := starGiftInfo.FromName
				if fromStr == "" {
					fromStr = "Anonymous"
				}
				if msg.Out {
					text = fmt.Sprintf("You sent a gift to %s", starGiftInfo.ToName)
				} else {
					text = fmt.Sprintf("%s transferred you a gift", fromStr)
				}
			} else {
				text = "Transferred you a gift"
			}
		case *tg.MessageActionStarGiftUnique:
			starGiftInfo = c.extractUniqueStarGiftFromAction(a, e, msg.Out)
			if starGiftInfo != nil {
				fromStr := starGiftInfo.FromName
				if fromStr == "" {
					fromStr = "Anonymous"
				}
				if a.Upgrade {
					text = fmt.Sprintf("Gift upgraded to %s #%d", starGiftInfo.Title, starGiftInfo.Num)
				} else if msg.Out {
					text = fmt.Sprintf("You transferred a collectible gift to %s", starGiftInfo.ToName)
				} else {
					text = fmt.Sprintf("%s transferred you a gift", fromStr)
				}
			} else {
				text = "Transferred you a collectible gift"
			}
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
		StarGift:  starGiftInfo,
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
		var strippedThumb string
		var photoID int64
		if p, ok := u.Photo.(*tg.UserProfilePhoto); ok {
			strippedThumb = ExtractStrippedThumbURL(p.StrippedThumb)
			photoID = p.PhotoID
			if photoID != 0 {
				photoURL = fmt.Sprintf("/api/avatar?peer_id=%d", u.ID)
			} else {
				photoURL = strippedThumb
			}
		}

		statusText, isOnline := parseUserStatus(u.Status)

		c.state.UpsertEntity(&models.EntityInfo{
			ID:            u.ID,
			AccessHash:    u.AccessHash,
			Type:          chatType,
			Title:         name,
			Username:      u.Username,
			Phone:         u.Phone,
			PhotoURL:      photoURL,
			PhotoID:       photoID,
			StrippedThumb: strippedThumb,
			StatusText:    statusText,
			IsOnline:      isOnline,
		})

		// Save non-min user to persistent pebble storage
		if c.peerDB != nil && !u.Min && u.AccessHash != 0 {
			var p storage.Peer
			if p.FromUser(u) {
				_ = c.peerDB.Add(ctx, p)
			}
		}
	}

	// Cache Chats & Channels
	for _, cClass := range mod.GetChats() {
		switch ch := cClass.(type) {
		case *tg.Chat:
			var photoURL string
			var strippedThumb string
			var photoID int64
			if p, ok := ch.Photo.(*tg.ChatPhoto); ok {
				strippedThumb = ExtractStrippedThumbURL(p.StrippedThumb)
				photoID = p.PhotoID
				if photoID != 0 {
					photoURL = fmt.Sprintf("/api/avatar?peer_id=%d", ch.ID)
				} else {
					photoURL = strippedThumb
				}
			}
			c.state.UpsertEntity(&models.EntityInfo{
				ID:            ch.ID,
				Type:          models.ChatTypeGroup,
				Title:         ch.Title,
				PhotoURL:      photoURL,
				PhotoID:       photoID,
				StrippedThumb: strippedThumb,
				MembersCount:  ch.ParticipantsCount,
				NoForwards:    ch.Noforwards,
			})
		case *tg.Channel:
			chatType := models.ChatTypeChannel
			if ch.Megagroup {
				chatType = models.ChatTypeGroup
			}
			var photoURL string
			var strippedThumb string
			var photoID int64
			if p, ok := ch.Photo.(*tg.ChatPhoto); ok {
				strippedThumb = ExtractStrippedThumbURL(p.StrippedThumb)
				photoID = p.PhotoID
				if photoID != 0 {
					photoURL = fmt.Sprintf("/api/avatar?peer_id=%d", ch.ID)
				} else {
					photoURL = strippedThumb
				}
			}
			c.state.UpsertEntity(&models.EntityInfo{
				ID:            ch.ID,
				AccessHash:    ch.AccessHash,
				Type:          chatType,
				Title:         ch.Title,
				Username:      ch.Username,
				PhotoURL:      photoURL,
				PhotoID:       photoID,
				StrippedThumb: strippedThumb,
				MembersCount:  ch.ParticipantsCount,
				IsVerified:    ch.Verified,
				NoForwards:    ch.Noforwards,
			})

			if c.peerDB != nil && !ch.Min && ch.AccessHash != 0 {
				var p storage.Peer
				if p.FromChat(ch) {
					_ = c.peerDB.Add(ctx, p)
				}
			}
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
		var strippedThumb string
		var photoID int64
		var noForwards bool
		var statusText string
		var isOnline bool
		var emojiStatus string
		membersCount := 0
		isVerified := false

		if entity != nil {
			title = entity.Title
			username = entity.Username
			chatType = entity.Type
			accessHash = entity.AccessHash
			photoURL = entity.PhotoURL
			photoID = entity.PhotoID
			strippedThumb = entity.StrippedThumb
			noForwards = entity.NoForwards
			statusText = entity.StatusText
			isOnline = entity.IsOnline
			membersCount = entity.MembersCount
			isVerified = entity.IsVerified
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
					statusText, isOnline = parseUserStatus(p.User.Status)
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

		isMuted := d.NotifySettings.Silent || (d.NotifySettings.MuteUntil > int(time.Now().Unix()))

		topMsgText := ""
		topMsgSender := ""
		topMsgMedia := ""
		topMsgOut := false
		topMsgRead := false
		lastDate := time.Now()

		if topMsg, ok := messagesMap[d.TopMessage]; ok {
			mModel := c.tlMessageToModel(topMsg, tg.Entities{})
			topMsgText = mModel.Text
			lastDate = mModel.Date
			topMsgOut = mModel.Out
			topMsgRead = (topMsg.Out && topMsg.ID <= d.ReadOutboxMaxID) || (!topMsg.Out && topMsg.ID <= d.ReadInboxMaxID)

			if mModel.Media != nil {
				topMsgMedia = mModel.Media.Type
				if topMsgText == "" {
					topMsgText = InspectMediaText(topMsg.Media)
				}
			}

			if topMsg.Out {
				topMsgSender = "You"
			} else if chatType == models.ChatTypeGroup {
				senderID := c.extractPeerID(topMsg.FromID)
				if senderEnt, ok := c.state.GetEntity(senderID); ok {
					topMsgSender = senderEnt.Title
				}
			}
			c.state.AppendMessage(mModel)
		} else if chatType == models.ChatTypeUser && statusText != "" {
			topMsgText = statusText
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
			TopMessageMedia:  topMsgMedia,
			TopMessageOut:    topMsgOut,
			TopMessageRead:   topMsgRead,
			LastMessageDate:  lastDate,
			PhotoURL:         photoURL,
			PhotoID:          photoID,
			StrippedThumb:    strippedThumb,
			IsOnline:         isOnline,
			StatusText:       statusText,
			IsMuted:          isMuted,
			EmojiStatus:      emojiStatus,
			MembersCount:     membersCount,
			IsVerified:       isVerified,
			NoForwards:       noForwards,
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

	var res tg.MessagesMessagesClass
	opName := fmt.Sprintf("FetchHistory chat=%d limit=%d", normChatID, limit)
	err = services.WithRetry(ctx, opName, services.DefaultRetryConfig(), func(attemptCtx context.Context) error {
		var fErr error
		res, fErr = c.api.MessagesGetHistory(attemptCtx, &tg.MessagesGetHistoryRequest{
			Peer:     inputPeer,
			OffsetID: offsetID,
			Limit:    limit,
		})
		return fErr
	})
	if err != nil {
		logger.Error("MTProto", "FetchHistory failed for %d: %v", normChatID, err)
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
	var historicalMsgs []*models.Message
	for _, mClass := range messages {
		switch m := mClass.(type) {
		case *tg.Message:
			msgModel := c.tlMessageToModel(m, entities)
			if msgModel != nil {
				historicalMsgs = append(historicalMsgs, msgModel)
			}
		case *tg.MessageService:
			srvModel := c.tlServiceMessageToModel(m, entities)
			if srvModel != nil {
				historicalMsgs = append(historicalMsgs, srvModel)
			}
		}
	}
	c.state.AppendHistoricalMessages(normChatID, historicalMsgs)
	if chatID != normChatID {
		c.state.AppendHistoricalMessages(chatID, historicalMsgs)
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

	var updatesClass tg.UpdatesClass
	opName := fmt.Sprintf("SendMessage chat=%d len=%d", normChatID, len(text))
	err = services.WithRetry(ctx, opName, services.DefaultRetryConfig(), func(attemptCtx context.Context) error {
		var sErr error
		updatesClass, sErr = c.api.MessagesSendMessage(attemptCtx, req)
		return sErr
	})
	if err != nil {
		logger.Error("MTProto", "SendMessage failed for %d: %v", normChatID, err)
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
	logger.MTProto("Message %d sent to chat %d successfully", msgID, normChatID)
	return msgModel, nil
}

// SendMedia uploads media chunks and dispatches MessagesSendMedia to Telegram.
func (c *ClientController) SendMedia(
	ctx context.Context,
	chatID int64,
	mediaType string,
	fileName string,
	data []byte,
	caption string,
	replyToMsgID int,
) (*models.Message, error) {
	if c.uploader == nil {
		return nil, errors.New("uploader service not initialized")
	}

	normChatID, _ := NormalizeChatID(chatID)
	peer, err := c.resolveInputPeer(ctx, normChatID)
	if err != nil {
		return nil, err
	}

	var msg *models.Message
	opName := fmt.Sprintf("SendMedia %s (%d bytes) to %d", fileName, len(data), normChatID)
	err = services.WithRetry(ctx, opName, services.DefaultRetryConfig(), func(attemptCtx context.Context) error {
		var uErr error
		msg, uErr = c.uploader.UploadAndSendMedia(attemptCtx, peer, normChatID, fileName, data, mediaType, caption, replyToMsgID)
		return uErr
	})
	if err != nil {
		return nil, err
	}

	c.state.AppendMessage(msg)
	return msg, nil
}

// GetDownloader returns the active downloader service.
func (c *ClientController) GetDownloader() *services.DownloaderService {
	return c.downloader
}

// GetBatchDownloader returns the batch downloader service.
func (c *ClientController) GetBatchDownloader() *services.BatchDownloaderService {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.batchDownloader
}

// GetMultiUploader returns the multi-uploader engine.
func (c *ClientController) GetMultiUploader() *services.MultiUploaderEngine {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.multiUploader
}

// GetIndexerService returns the indexing and cache manager service.
func (c *ClientController) GetIndexerService() *services.IndexerService {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.indexerService
}

// GetMediaHubService returns the media hub and inspection service.
func (c *ClientController) GetMediaHubService() *services.MediaHubService {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mediaHubService
}

// GetTranscriber returns the speech-to-text service.
func (c *ClientController) GetTranscriber() *services.TranscriberService {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.transcriber
}

// TranscribeMessage fetches or downloads audio for a message and transcribes it.
func (c *ClientController) TranscribeMessage(ctx context.Context, chatID int64, messageID int) (string, error) {
	if c.transcriber == nil {
		return "", errors.New("transcriber service not initialized")
	}

	normChatID, _ := NormalizeChatID(chatID)

	// Check if already transcribed
	if text, ok := c.transcriber.GetCachedTranscription(normChatID, messageID); ok && text != "" {
		return text, nil
	}

	// 1. Check if media is already cached on disk by downloader
	var audioPath string
	if c.downloader != nil {
		if path, ok := c.downloader.IsCached(normChatID, messageID); ok {
			audioPath = path
		} else {
			// Download via dedicated DC pool
			media, _, err := c.ResolveMedia(ctx, normChatID, messageID)
			if err == nil && media != nil && media.InputFileLoc != nil {
				path, err := c.downloader.FetchOrDownloadWithDC(ctx, normChatID, messageID, media.InputFileLoc, media.DC, media.Size)
				if err == nil {
					audioPath = path
				}
			}
		}
	}

	// 2. Fallback to direct download to temporary file if not cached
	if audioPath == "" {
		tmpFile, err := os.CreateTemp("", fmt.Sprintf("tg_voice_%d_%d_*.ogg", normChatID, messageID))
		if err != nil {
			return "", fmt.Errorf("create temp audio file: %w", err)
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		_, _, err = c.DownloadMedia(ctx, normChatID, messageID, tmpFile)
		_ = tmpFile.Close()
		if err != nil {
			return "", fmt.Errorf("failed to download audio for transcription: %w", err)
		}
		audioPath = tmpPath
	}

	return c.transcriber.TranscribeAudioFile(ctx, normChatID, messageID, audioPath)
}

// ResolveInputPeer exposes peer resolution for services.
func (c *ClientController) ResolveInputPeer(ctx context.Context, chatID int64) (tg.InputPeerClass, error) {
	return c.resolveInputPeer(ctx, chatID)
}

// SetActiveChat informs the background syncer about user focus.
func (c *ClientController) SetActiveChat(chatID int64) {
	if c.syncer != nil {
		c.syncer.SetActiveChat(chatID)
	}
}

// Ping issues a lightweight MTProto call to keep the TCP connection alive.
func (c *ClientController) Ping(ctx context.Context) error {
	if c.api == nil {
		return errors.New("api not initialized")
	}
	_, err := c.api.HelpGetNearestDC(ctx)
	return err
}

// DownloadMedia streams a message's media file directly to an io.Writer using tdl/core multi-DC pool.
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

	var (
		loc  tg.InputFileLocationClass
		name string
		mime string
		dc   int
		size int64
	)

	// Extract media metadata and target DC using tdl/core/tmedia
	if m, ok := tmedia.ExtractMedia(tgMsg.Media); ok {
		loc = m.InputFileLoc
		name = m.Name
		dc = m.DC
		size = m.Size
	}

	qElem := qmessages.Elem{Msg: tgMsg}
	if file, ok := qElem.File(); ok {
		if loc == nil {
			loc = file.Location
		}
		if name == "" {
			name = file.Name
		}
		mime = file.MIMEType
	}

	if loc == nil {
		return "", "", errors.New("file location could not be resolved from message media")
	}

	if c.downloader != nil {
		cachePath, err := c.downloader.FetchOrDownloadWithDC(ctx, normChatID, messageID, loc, dc, size)
		if err != nil {
			return name, mime, err
		}
		// Stream to writer
		f, err := os.Open(cachePath)
		if err != nil {
			return name, mime, err
		}
		defer f.Close()
		_, _ = io.Copy(w, f)
		return name, mime, nil
	}

	var dlClient *tg.Client = c.api
	if dc > 0 && c.pool != nil {
		dlClient = c.pool.Client(ctx, dc)
	}

	d := downloader.NewDownloader().WithPartSize(1024 * 1024)
	_, err = d.Download(dlClient, loc).Stream(ctx, w)
	if err != nil {
		return name, mime, errors.Wrap(err, "stream download")
	}

	return name, mime, nil
}

// ResolveMedia extracts tmedia.Media (DC, Location, Size, Name) and MIME type for a message.
func (c *ClientController) ResolveMedia(ctx context.Context, chatID int64, messageID int) (*tmedia.Media, string, error) {
	if c.api == nil {
		return nil, "", errors.New("client api not initialized")
	}

	normChatID, _ := NormalizeChatID(chatID)
	inputPeer, err := c.resolveInputPeer(ctx, normChatID)
	if err != nil {
		return nil, "", err
	}

	var tgMsg *tg.Message
	switch p := inputPeer.(type) {
	case *tg.InputPeerChannel:
		res, err := c.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}},
		})
		if err != nil {
			return nil, "", errors.Wrap(err, "get channel message")
		}
		if mod, ok := res.AsModified(); ok && len(mod.GetMessages()) > 0 {
			tgMsg, _ = mod.GetMessages()[0].(*tg.Message)
		}
	default:
		res, err := c.api.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}})
		if err != nil {
			return nil, "", errors.Wrap(err, "get message")
		}
		if mod, ok := res.AsModified(); ok && len(mod.GetMessages()) > 0 {
			tgMsg, _ = mod.GetMessages()[0].(*tg.Message)
		}
	}

	if tgMsg == nil || tgMsg.Media == nil {
		return nil, "", errors.New("message or media attachment not found")
	}

	mimeType := ""
	qElem := qmessages.Elem{Msg: tgMsg}
	if file, ok := qElem.File(); ok {
		mimeType = file.MIMEType
	}

	if media, ok := tmedia.ExtractMedia(tgMsg.Media); ok {
		return media, mimeType, nil
	}

	return nil, mimeType, errors.New("media metadata could not be extracted")
}

// ResolveMediaFileLocation finds the underlying file location and metadata for a message.
func (c *ClientController) ResolveMediaFileLocation(ctx context.Context, chatID int64, messageID int) (tg.InputFileLocationClass, string, string, error) {
	if c.api == nil {
		return nil, "", "", errors.New("client api not initialized")
	}

	normChatID, _ := NormalizeChatID(chatID)
	inputPeer, err := c.resolveInputPeer(ctx, normChatID)
	if err != nil {
		return nil, "", "", err
	}

	var tgMsg *tg.Message
	switch p := inputPeer.(type) {
	case *tg.InputPeerChannel:
		res, err := c.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
			ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}},
		})
		if err != nil {
			return nil, "", "", errors.Wrap(err, "get channel message")
		}
		if mod, ok := res.AsModified(); ok && len(mod.GetMessages()) > 0 {
			tgMsg, _ = mod.GetMessages()[0].(*tg.Message)
		}
	default:
		res, err := c.api.MessagesGetMessages(ctx, []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}})
		if err != nil {
			return nil, "", "", errors.Wrap(err, "get message")
		}
		if mod, ok := res.AsModified(); ok && len(mod.GetMessages()) > 0 {
			tgMsg, _ = mod.GetMessages()[0].(*tg.Message)
		}
	}

	if tgMsg == nil || tgMsg.Media == nil {
		return nil, "", "", errors.New("message or media attachment not found")
	}

	file, ok := qmessages.Elem{Msg: tgMsg}.File()
	if !ok {
		return nil, "", "", errors.New("file location could not be resolved from message media")
	}

	return file.Location, file.Name, file.MIMEType, nil
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

	// Check if this is self (Saved Messages)
	if selfProfile := c.state.GetUser(); selfProfile != nil && (normID == selfProfile.ID || chatID == selfProfile.ID) {
		return &tg.InputPeerSelf{}, nil
	}

	// 1. Try In-Memory State Chats (authoritative from live GetDialogs)
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

	// 2. Try In-Memory State Entities
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

	// 3. Try Pebble peer storage (only accept full non-min peers with valid access hashes)
	if c.peerDB != nil {
		if isChannel {
			if p, err := storage.FindPeer(ctx, c.peerDB, &tg.PeerChannel{ChannelID: normID}); err == nil {
				if p.Channel != nil && !p.Channel.Min && p.Channel.AccessHash != 0 {
					return &tg.InputPeerChannel{ChannelID: p.Channel.ID, AccessHash: p.Channel.AccessHash}, nil
				}
			}
		}

		if p, err := storage.FindPeer(ctx, c.peerDB, &tg.PeerUser{UserID: normID}); err == nil {
			if p.User != nil && !p.User.Min && p.User.AccessHash != 0 {
				return &tg.InputPeerUser{UserID: p.User.ID, AccessHash: p.User.AccessHash}, nil
			}
		}

		if p, err := storage.FindPeer(ctx, c.peerDB, &tg.PeerChannel{ChannelID: normID}); err == nil {
			if p.Channel != nil && !p.Channel.Min && p.Channel.AccessHash != 0 {
				return &tg.InputPeerChannel{ChannelID: p.Channel.ID, AccessHash: p.Channel.AccessHash}, nil
			}
		}

		if p, err := storage.FindPeer(ctx, c.peerDB, &tg.PeerChat{ChatID: normID}); err == nil {
			if p.Chat != nil {
				return &tg.InputPeerChat{ChatID: p.Chat.ID}, nil
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
				if u, ok := uClass.(*tg.User); ok && u.ID == normID && !u.Min && u.AccessHash != 0 {
					c.state.UpsertEntity(&models.EntityInfo{
						ID:         u.ID,
						AccessHash: u.AccessHash,
						Type:       models.ChatTypeUser,
						Title:      strings.TrimSpace(u.FirstName + " " + u.LastName),
						Username:   u.Username,
						Phone:      u.Phone,
					})
					if c.peerDB != nil {
						var p storage.Peer
						if p.FromUser(u) {
							_ = c.peerDB.Add(ctx, p)
						}
					}
					return &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}, nil
				}
			}
			for _, chClass := range res.Chats {
				if ch, ok := chClass.(*tg.Channel); ok && ch.ID == normID && !ch.Min && ch.AccessHash != 0 {
					c.state.UpsertEntity(&models.EntityInfo{
						ID:         ch.ID,
						AccessHash: ch.AccessHash,
						Type:       models.ChatTypeChannel,
						Title:      ch.Title,
						Username:   ch.Username,
					})
					if c.peerDB != nil {
						var p storage.Peer
						if p.FromChat(ch) {
							_ = c.peerDB.Add(ctx, p)
						}
					}
					return &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
				}
			}
		}
	}

	// 5. Fallback: if in-memory state was not populated, try a quick FetchDialogs and retry
	if c.api != nil {
		dCtx, dCancel := context.WithTimeout(ctx, 10*time.Second)
		_ = c.FetchDialogs(dCtx)
		dCancel()

		if chat, ok := c.state.GetChat(normID); ok && chat.AccessHash != 0 {
			switch chat.Type {
			case models.ChatTypeUser, models.ChatTypeBot:
				return &tg.InputPeerUser{UserID: normID, AccessHash: chat.AccessHash}, nil
			case models.ChatTypeChannel:
				return &tg.InputPeerChannel{ChannelID: normID, AccessHash: chat.AccessHash}, nil
			case models.ChatTypeGroup:
				return &tg.InputPeerChannel{ChannelID: normID, AccessHash: chat.AccessHash}, nil
			}
		}
		if ent, ok := c.state.GetEntity(normID); ok && ent.AccessHash != 0 {
			switch ent.Type {
			case models.ChatTypeUser, models.ChatTypeBot:
				return &tg.InputPeerUser{UserID: normID, AccessHash: ent.AccessHash}, nil
			case models.ChatTypeChannel:
				return &tg.InputPeerChannel{ChannelID: normID, AccessHash: ent.AccessHash}, nil
			case models.ChatTypeGroup:
				return &tg.InputPeerChannel{ChannelID: normID, AccessHash: ent.AccessHash}, nil
			}
		}
	}

	return nil, fmt.Errorf("unable to resolve input peer for chat id %d (normalized %d)", chatID, normID)
}

// SendReaction sends or removes a reaction on a message.
func (c *ClientController) SendReaction(ctx context.Context, chatID int64, msgID int, reaction string) error {
	if c.api == nil {
		return errors.New("client api not initialized")
	}

	normChatID, _ := NormalizeChatID(chatID)
	inputPeer, err := c.resolveInputPeer(ctx, normChatID)
	if err != nil {
		return err
	}

	req := &tg.MessagesSendReactionRequest{
		Peer:  inputPeer,
		MsgID: msgID,
	}

	if reaction != "" {
		req.SetReaction([]tg.ReactionClass{
			&tg.ReactionEmoji{Emoticon: reaction},
		})
		req.SetAddToRecent(true)
	}

	_, err = c.api.MessagesSendReaction(ctx, req)
	if err != nil {
		return errors.Wrap(err, "send reaction")
	}

	return nil
}

// GetAvailableReactions returns default and server reactions list.
func (c *ClientController) GetAvailableReactions(ctx context.Context) ([]string, error) {
	defaults := []string{"👍", "❤️", "🔥", "🎉", "👏", "😁", "🤔", "🤯", "😱", "🤬", "😢", "🤩", "🤮", "💩", "🙏", "👌", "🕊️", "🤡", "🥱", "🥴", "😍", "🐳", "❤️‍🔥", "🌚", "🌭", "💯", "🤣", "⚡", "🍌", "🏆", "💔", "🤨", "😐", "🍓", "🍾", "💋", "🖕", "😈", "😴", "😭", "🤓", "👻", "👀", "🎃", "🙈", "😇", "😨", "🤝", "✍️", "🤗", "🫡"}
	if c.api == nil {
		return defaults, nil
	}

	res, err := c.api.MessagesGetAvailableReactions(ctx, 0)
	if err != nil {
		return defaults, nil
	}

	if ar, ok := res.(*tg.MessagesAvailableReactions); ok {
		var list []string
		for _, r := range ar.Reactions {
			if !r.Inactive && r.Reaction != "" {
				list = append(list, r.Reaction)
			}
		}
		if len(list) > 0 {
			return list, nil
		}
	}

	return defaults, nil
}

// GetFullUser retrieves extended profile, birthday, business info, and bot details.
func (c *ClientController) GetFullUser(ctx context.Context, userID int64) (*models.UserFullDetails, error) {
	if c.api == nil {
		return nil, errors.New("client api not initialized")
	}

	normID, _ := NormalizeChatID(userID)
	var inputUser tg.InputUserClass = &tg.InputUserSelf{}
	if self := c.state.GetUser(); self == nil || self.ID != normID {
		if ent, ok := c.state.GetEntity(normID); ok && ent.AccessHash != 0 {
			inputUser = &tg.InputUser{UserID: normID, AccessHash: ent.AccessHash}
		} else {
			p, err := c.resolveInputPeer(ctx, normID)
			if err != nil {
				return nil, err
			}
			if pu, ok := p.(*tg.InputPeerUser); ok {
				inputUser = &tg.InputUser{UserID: pu.UserID, AccessHash: pu.AccessHash}
			} else {
				return nil, fmt.Errorf("peer %d is not a user", normID)
			}
		}
	}

	res, err := c.api.UsersGetFullUser(ctx, inputUser)
	if err != nil {
		return nil, errors.Wrap(err, "get full user")
	}

	fu := res.FullUser
	details := &models.UserFullDetails{
		ID:                fu.ID,
		About:             fu.About,
		PersonalChannelID: fu.PersonalChannelID,
		StarGiftsCount:    fu.StargiftsCount,
		CommonChatsCount:  fu.CommonChatsCount,
		PinnedMsgID:       fu.PinnedMsgID,
	}

	if fu.Birthday.Day > 0 && fu.Birthday.Month > 0 {
		months := []string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
		mName := fmt.Sprintf("%d", fu.Birthday.Month)
		if fu.Birthday.Month >= 1 && fu.Birthday.Month <= 12 {
			mName = months[fu.Birthday.Month]
		}
		if fu.Birthday.Year > 0 {
			details.Birthday = fmt.Sprintf("%s %d, %d", mName, fu.Birthday.Day, fu.Birthday.Year)
		} else {
			details.Birthday = fmt.Sprintf("%s %d", mName, fu.Birthday.Day)
		}
	}

	if fu.BusinessLocation.Address != "" {
		details.BusinessAddress = fu.BusinessLocation.Address
	}

	if fu.BotInfo.Description != "" {
		details.BotDescription = fu.BotInfo.Description
	}

	for _, cmd := range fu.BotInfo.Commands {
		details.BotCommands = append(details.BotCommands, models.BotCommandItem{
			Command:     cmd.Command,
			Description: cmd.Description,
		})
	}

	if fu.BotInfo.MenuButton != nil {
		switch mb := fu.BotInfo.MenuButton.(type) {
		case *tg.BotMenuButtonCommands:
			details.BotMenuButton = &models.BotMenuButtonItem{Type: "commands"}
		case *tg.BotMenuButton:
			details.BotMenuButton = &models.BotMenuButtonItem{Type: "web_app", Text: mb.Text, URL: mb.URL}
		case *tg.BotMenuButtonDefault:
			details.BotMenuButton = &models.BotMenuButtonItem{Type: "default"}
		}
	}

	// Fetch up to 30 gifts for user profile display
	if gifts, _, err := c.GetSavedStarGifts(ctx, normID, "", 30); err == nil && len(gifts) > 0 {
		details.Gifts = gifts
		if details.StarGiftsCount == 0 {
			details.StarGiftsCount = len(gifts)
		}
	}

	return details, nil
}

// GetBotMenuButton fetches the bot menu button for a chat.
func (c *ClientController) GetBotMenuButton(ctx context.Context, chatID int64) (*models.BotMenuButtonItem, error) {
	if c.api == nil {
		return nil, errors.New("client api not initialized")
	}

	normID, _ := NormalizeChatID(chatID)
	var inputUser tg.InputUserClass
	if ent, ok := c.state.GetEntity(normID); ok && ent.AccessHash != 0 {
		inputUser = &tg.InputUser{UserID: normID, AccessHash: ent.AccessHash}
	} else {
		p, err := c.resolveInputPeer(ctx, normID)
		if err != nil {
			return nil, err
		}
		if pu, ok := p.(*tg.InputPeerUser); ok {
			inputUser = &tg.InputUser{UserID: pu.UserID, AccessHash: pu.AccessHash}
		} else {
			return nil, fmt.Errorf("peer %d is not a user", normID)
		}
	}

	res, err := c.api.BotsGetBotMenuButton(ctx, inputUser)
	if err != nil {
		return nil, errors.Wrap(err, "get bot menu button")
	}

	switch mb := res.(type) {
	case *tg.BotMenuButtonCommands:
		return &models.BotMenuButtonItem{Type: "commands"}, nil
	case *tg.BotMenuButton:
		return &models.BotMenuButtonItem{Type: "web_app", Text: mb.Text, URL: mb.URL}, nil
	default:
		return &models.BotMenuButtonItem{Type: "default"}, nil
	}
}

// DownloadAvatar fetches the full resolution profile photo for a user, group, or channel.
func (c *ClientController) DownloadAvatar(ctx context.Context, peerID int64, big bool) ([]byte, error) {
	if c.api == nil {
		return nil, errors.New("client api not initialized")
	}

	normID, _ := NormalizeChatID(peerID)

	// Helper to extract StrippedThumb bytes as fallback
	getStrippedThumb := func() ([]byte, error) {
		thumbStr := ""
		for _, id := range []int64{normID, peerID} {
			if ent, ok := c.state.GetEntity(id); ok && ent.StrippedThumb != "" {
				thumbStr = ent.StrippedThumb
				break
			}
			if chat, ok := c.state.GetChat(id); ok && chat.StrippedThumb != "" {
				thumbStr = chat.StrippedThumb
				break
			}
		}
		if thumbStr == "" {
			for _, id := range []int64{normID, peerID} {
				if ent, ok := c.state.GetEntity(id); ok && strings.HasPrefix(ent.PhotoURL, "data:") {
					thumbStr = ent.PhotoURL
					break
				}
				if chat, ok := c.state.GetChat(id); ok && strings.HasPrefix(chat.PhotoURL, "data:") {
					thumbStr = chat.PhotoURL
					break
				}
			}
		}
		if thumbStr != "" {
			parts := strings.Split(thumbStr, ",")
			raw := parts[len(parts)-1]
			if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) > 0 {
				return b, nil
			}
		}
		return nil, errors.New("no avatar photo available for peer")
	}

	// 1. Check local cache dir first
	cacheDir := filepath.Join("session", "avatars")
	_ = os.MkdirAll(cacheDir, 0755)
	sizeTag := "small"
	if big {
		sizeTag = "big"
	}
	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("%d_%s.jpg", normID, sizeTag))
	if data, err := os.ReadFile(cacheFile); err == nil && len(data) > 0 {
		return data, nil
	}

	// 2. Try getting profile photo using chat.PhotoURL first
	var photoURL string
	var photoID int64
	for _, id := range []int64{normID, peerID} {
		if chat, ok := c.state.GetChat(id); ok {
			if photoURL == "" && chat.PhotoURL != "" {
				photoURL = chat.PhotoURL
			}
			if photoID == 0 && chat.PhotoID != 0 {
				photoID = chat.PhotoID
			}
		}
		if ent, ok := c.state.GetEntity(id); ok {
			if photoURL == "" && ent.PhotoURL != "" {
				photoURL = ent.PhotoURL
			}
			if photoID == 0 && ent.PhotoID != 0 {
				photoID = ent.PhotoID
			}
		}
	}

	if photoURL != "" {
		if (strings.HasPrefix(photoURL, "http://") || strings.HasPrefix(photoURL, "https://")) && !strings.Contains(photoURL, "/api/avatar") {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, photoURL, nil)
			if err == nil {
				client := &http.Client{Timeout: 5 * time.Second}
				resp, err := client.Do(req)
				if err == nil {
					defer resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						if data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)); err == nil && len(data) > 0 {
							_ = os.WriteFile(cacheFile, data, 0644)
							return data, nil
						}
					}
				}
			}
		} else if strings.HasPrefix(photoURL, "data:") {
			parts := strings.Split(photoURL, ",")
			if b, err := base64.StdEncoding.DecodeString(parts[len(parts)-1]); err == nil && len(b) > 0 {
				if photoID == 0 || !big {
					_ = os.WriteFile(cacheFile, b, 0644)
					return b, nil
				}
			}
		} else if !strings.HasPrefix(photoURL, "/api/") {
			if data, err := os.ReadFile(photoURL); err == nil && len(data) > 0 {
				return data, nil
			}
		}
	}

	// 3. If failed, use photoID and InputPeer
	inputPeer, err := c.resolveInputPeer(ctx, normID)
	if err == nil && inputPeer != nil && photoID != 0 {
		loc := &tg.InputPeerPhotoFileLocation{
			Peer:    inputPeer,
			PhotoID: photoID,
			Big:     big,
		}

		dlCtx, dlCancel := context.WithTimeout(ctx, 4*time.Second)
		res, err := c.api.UploadGetFile(dlCtx, &tg.UploadGetFileRequest{
			Location: loc,
			Offset:   0,
			Limit:    1024 * 1024,
		})
		dlCancel()

		if err == nil && res != nil {
			if uf, ok := res.(*tg.UploadFile); ok && len(uf.Bytes) > 0 {
				_ = os.WriteFile(cacheFile, uf.Bytes, 0644)
				return uf.Bytes, nil
			}
		}
	}

	// 4. Fallback to StrippedThumb if direct MTProto download fails or photoID is missing
	return getStrippedThumb()
}

// ForwardMessages forwards one or more messages to another chat or saved messages.
func (c *ClientController) ForwardMessages(ctx context.Context, fromChatID, toChatID int64, messageIDs []int) error {
	if c.api == nil {
		return errors.New("client api not initialized")
	}
	if len(messageIDs) == 0 {
		return errors.New("no messages specified to forward")
	}

	normFromID, _ := NormalizeChatID(fromChatID)
	fromPeer, err := c.resolveInputPeer(ctx, normFromID)
	if err != nil {
		return errors.Wrap(err, "resolve from peer")
	}

	normToID, _ := NormalizeChatID(toChatID)
	toPeer, err := c.resolveInputPeer(ctx, normToID)
	if err != nil {
		return errors.Wrap(err, "resolve to peer")
	}

	randIDs := make([]int64, len(messageIDs))
	for i := range randIDs {
		randIDs[i] = rand.Int63()
	}

	_, err = c.api.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
		FromPeer: fromPeer,
		ToPeer:   toPeer,
		ID:       messageIDs,
		RandomID: randIDs,
	})
	if err != nil {
		return errors.Wrap(err, "forward messages")
	}

	return nil
}

func (c *ClientController) extractStarGiftFromAction(a *tg.MessageActionStarGift, e tg.Entities, out bool) *models.StarGiftInfo {
	if a == nil {
		return nil
	}
	info := &models.StarGiftInfo{
		ConvertStars: a.ConvertStars,
		IsUpgrade:    a.UpgradeSeparate || a.PrepaidUpgrade,
		CanExportAt:  0,
	}

	if a.Message.Text != "" {
		info.Message = a.Message.Text
	}

	if a.FromID != nil {
		info.FromID = c.extractPeerID(a.FromID)
		if u, ok := e.Users[info.FromID]; ok {
			info.FromName = strings.TrimSpace(u.FirstName + " " + u.LastName)
		} else if ent, ok := c.state.GetEntity(info.FromID); ok {
			info.FromName = ent.Title
		}
	}
	if a.ToID != nil {
		info.ToID = c.extractPeerID(a.ToID)
		if u, ok := e.Users[info.ToID]; ok {
			info.ToName = strings.TrimSpace(u.FirstName + " " + u.LastName)
		} else if ent, ok := c.state.GetEntity(info.ToID); ok {
			info.ToName = ent.Title
		}
	}

	if a.Gift != nil {
		c.fillStarGiftClass(a.Gift, info)
	}

	if info.Title == "" {
		info.Title = "Telegram Star Gift"
	}
	return info
}

func (c *ClientController) extractUniqueStarGiftFromAction(a *tg.MessageActionStarGiftUnique, e tg.Entities, out bool) *models.StarGiftInfo {
	if a == nil {
		return nil
	}
	info := &models.StarGiftInfo{
		IsUnique:    true,
		IsUpgrade:   a.Upgrade,
		IsRefunded:  a.Refunded,
		CanExportAt: a.CanExportAt,
		CanTransfer: a.CanTransferAt == 0 || a.CanTransferAt <= int(time.Now().Unix()),
	}

	if a.FromID != nil {
		info.FromID = c.extractPeerID(a.FromID)
		if u, ok := e.Users[info.FromID]; ok {
			info.FromName = strings.TrimSpace(u.FirstName + " " + u.LastName)
		} else if ent, ok := c.state.GetEntity(info.FromID); ok {
			info.FromName = ent.Title
		}
	}

	if a.Gift != nil {
		c.fillStarGiftClass(a.Gift, info)
	}

	if info.Title == "" {
		info.Title = "Unique Collectible Gift"
	}
	return info
}

func (c *ClientController) fillStarGiftClass(gift tg.StarGiftClass, info *models.StarGiftInfo) {
	if gift == nil {
		return
	}
	switch g := gift.(type) {
	case *tg.StarGift:
		info.GiftID = g.ID
		info.Stars = g.Stars
		if g.ConvertStars > 0 {
			info.ConvertStars = g.ConvertStars
		}
		if g.Title != "" {
			info.Title = g.Title
		}
		if bg, ok := g.GetBackground(); ok {
			info.CenterColor = fmt.Sprintf("#%06X", bg.CenterColor)
			info.EdgeColor = fmt.Sprintf("#%06X", bg.EdgeColor)
			info.TextColor = fmt.Sprintf("#%06X", bg.TextColor)
		}
		if doc, ok := g.Sticker.(*tg.Document); ok {
			info.StickerURL = fmt.Sprintf("/api/media?chat_id=%d&message_id=0&doc_id=%d", info.GiftID, doc.ID)
			for _, t := range doc.Thumbs {
				if s, ok := t.(*tg.PhotoStrippedSize); ok {
					info.ThumbURL = ExtractStrippedThumbURL(s.Bytes)
				}
			}
		}
	case *tg.StarGiftUnique:
		info.GiftID = g.GiftID
		info.IsUnique = true
		info.Title = g.Title
		info.Slug = g.Slug
		info.Num = g.Num

		for _, attrClass := range g.Attributes {
			switch attr := attrClass.(type) {
			case *tg.StarGiftAttributeModel:
				info.Model = attr.Name
				var rarityStr string
				if attr.Rarity != nil {
					switch r := attr.Rarity.(type) {
					case *tg.StarGiftAttributeRarityRare:
						rarityStr = "Rare"
					case *tg.StarGiftAttributeRarityEpic:
						rarityStr = "Epic"
					case *tg.StarGiftAttributeRarityLegendary:
						rarityStr = "Legendary"
					case *tg.StarGiftAttributeRarityUncommon:
						rarityStr = "Uncommon"
					case *tg.StarGiftAttributeRarity:
						rarityStr = fmt.Sprintf("%d‰", r.Permille)
					}
				}
				info.Attributes = append(info.Attributes, models.StarGiftAttribute{
					Name:    attr.Name,
					Type:    "model",
					Rarity:  rarityStr,
					Crafted: attr.Crafted,
				})
				if doc, ok := attr.Document.(*tg.Document); ok {
					for _, t := range doc.Thumbs {
						if s, ok := t.(*tg.PhotoStrippedSize); ok {
							info.ThumbURL = ExtractStrippedThumbURL(s.Bytes)
						}
					}
				}
			case *tg.StarGiftAttributePattern:
				info.Symbol = attr.Name
				info.Attributes = append(info.Attributes, models.StarGiftAttribute{
					Name: attr.Name,
					Type: "pattern",
				})
			case *tg.StarGiftAttributeBackdrop:
				info.Backdrop = attr.Name
				info.CenterColor = fmt.Sprintf("#%06X", attr.CenterColor)
				info.EdgeColor = fmt.Sprintf("#%06X", attr.EdgeColor)
				info.PatternColor = fmt.Sprintf("#%06X", attr.PatternColor)
				info.TextColor = fmt.Sprintf("#%06X", attr.TextColor)
				info.Attributes = append(info.Attributes, models.StarGiftAttribute{
					Name:         attr.Name,
					Type:         "backdrop",
					CenterColor:  attr.CenterColor,
					EdgeColor:    attr.EdgeColor,
					PatternColor: attr.PatternColor,
					TextColor:    attr.TextColor,
				})
			case *tg.StarGiftAttributeOriginalDetails:
				if attr.Date > 0 {
					info.Date = time.Unix(int64(attr.Date), 0)
				}
				if attr.Message.Text != "" {
					info.Message = attr.Message.Text
				}
				if attr.SenderID != nil {
					info.FromID = c.extractPeerID(attr.SenderID)
				}
				if attr.RecipientID != nil {
					info.ToID = c.extractPeerID(attr.RecipientID)
				}
			}
		}
	}
}

// GetSavedStarGifts loads profile gifts for a peer (user or channel)
func (c *ClientController) GetSavedStarGifts(ctx context.Context, peerID int64, offset string, limit int) ([]*models.StarGiftInfo, int, error) {
	if c.api == nil {
		return nil, 0, errors.New("client api not initialized")
	}

	normID, _ := NormalizeChatID(peerID)
	inputPeer, err := c.resolveInputPeer(ctx, normID)
	if err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 30
	}

	res, err := c.api.PaymentsGetSavedStarGifts(ctx, &tg.PaymentsGetSavedStarGiftsRequest{
		Peer:   inputPeer,
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return nil, 0, errors.Wrap(err, "get saved star gifts")
	}

	var list []*models.StarGiftInfo
	entities := tg.Entities{
		Users: make(map[int64]*tg.User),
		Chats: make(map[int64]*tg.Chat),
	}
	for _, uClass := range res.Users {
		if u, ok := uClass.(*tg.User); ok {
			entities.Users[u.ID] = u
		}
	}

	for _, g := range res.Gifts {
		info := &models.StarGiftInfo{
			Date:         time.Unix(int64(g.Date), 0),
			ConvertStars: g.ConvertStars,
			CanExportAt:  g.CanExportAt,
			CanTransfer:  g.CanTransferAt == 0 || g.CanTransferAt <= int(time.Now().Unix()),
			Num:          g.GiftNum,
		}
		if g.Message.Text != "" {
			info.Message = g.Message.Text
		}
		if g.FromID != nil {
			info.FromID = c.extractPeerID(g.FromID)
			if u, ok := entities.Users[info.FromID]; ok {
				info.FromName = strings.TrimSpace(u.FirstName + " " + u.LastName)
			}
		}
		if g.Gift != nil {
			c.fillStarGiftClass(g.Gift, info)
		}
		list = append(list, info)
	}

	return list, res.Count, nil
}


