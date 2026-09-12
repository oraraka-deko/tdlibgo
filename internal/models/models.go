package models

import "time"

// AuthStateType represents the current stage in authentication.
type AuthStateType string

const (
	AuthStateIdle            AuthStateType = "Idle"
	AuthStateSendingCode     AuthStateType = "SendingCode"
	AuthStateWaitingCode     AuthStateType = "WaitingCode"
	AuthStateWaitingPassword AuthStateType = "WaitingPassword"
	AuthStateReady           AuthStateType = "Ready"
	AuthStateError           AuthStateType = "Error"
)

// ConnectionStateType represents MTProto connection status.
type ConnectionStateType string

const (
	ConnDisconnected ConnectionStateType = "Disconnected"
	ConnConnecting   ConnectionStateType = "Connecting"
	ConnUpdating     ConnectionStateType = "Updating"
	ConnReady        ConnectionStateType = "Ready"
)

// AuthState holds auth flow details.
type AuthState struct {
	State      AuthStateType `json:"state"`
	Phone      string        `json:"phone"`
	CodeType   string        `json:"code_type,omitempty"`
	Timeout    int           `json:"timeout,omitempty"`
	Error      string        `json:"error,omitempty"`
	IsLoggedIn bool          `json:"is_logged_in"`
}

// UserProfile represents authenticated user profile.
type UserProfile struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username,omitempty"`
	Phone     string `json:"phone,omitempty"`
	PhotoURL  string `json:"photo_url,omitempty"`
	IsBot     bool   `json:"is_bot"`
}

// ChatType represents peer category.
type ChatType string

const (
	ChatTypeUser    ChatType = "user"
	ChatTypeGroup   ChatType = "group"
	ChatTypeChannel ChatType = "channel"
	ChatTypeBot     ChatType = "bot"
)

// Chat represents a Telegram dialog/chat.
type Chat struct {
	ID               int64     `json:"id"`
	Type             ChatType  `json:"type"`
	Title            string    `json:"title"`
	Username         string    `json:"username,omitempty"`
	UnreadCount      int       `json:"unread_count"`
	Pinned           bool      `json:"pinned"`
	FolderID         int       `json:"folder_id"`
	TopMessageID     int       `json:"top_message_id"`
	TopMessageText   string    `json:"top_message_text"`
	TopMessageSender string    `json:"top_message_sender,omitempty"`
	LastMessageDate  time.Time `json:"last_message_date"`
	PhotoURL         string    `json:"photo_url,omitempty"`
	IsOnline         bool      `json:"is_online,omitempty"`
	TypingUser       string    `json:"typing_user,omitempty"`
	AccessHash       int64     `json:"-"`
}

// PollOption describes an individual answer option in a poll.
type PollOption struct {
	Text    string `json:"text"`
	Voters  int    `json:"voters"`
	Percent int    `json:"percent"`
	Chosen  bool   `json:"chosen"`
}

// PollInfo holds Telegram poll metadata and results.
type PollInfo struct {
	ID          int64        `json:"id"`
	Question    string       `json:"question"`
	Answers     []PollOption `json:"answers"`
	TotalVoters int          `json:"total_voters"`
	Closed      bool         `json:"closed"`
	Quiz        bool         `json:"quiz"`
	Multiple    bool         `json:"multiple"`
}

// MessageMedia describes photos, stickers, gifs, videos, documents, voice notes, or polls.
type MessageMedia struct {
	Type       string    `json:"type"`                  // "photo", "sticker", "video", "gif", "document", "audio", "voice", "poll"
	URL        string    `json:"url,omitempty"`          // streaming/download URL
	ThumbURL   string    `json:"thumb_url,omitempty"`   // base64 mini-thumbnail
	FileName   string    `json:"file_name,omitempty"`   // document filename
	FileSize   int64     `json:"file_size,omitempty"`   // size in bytes
	MimeType   string    `json:"mime_type,omitempty"`   // mime type e.g. image/webp
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	Duration   int       `json:"duration,omitempty"`    // in seconds for video/audio
	AltEmoji   string    `json:"alt_emoji,omitempty"`   // sticker alt emoji
	IsAnimated bool      `json:"is_animated,omitempty"` // animated sticker or gif
	Poll       *PollInfo `json:"poll,omitempty"`        // poll details if Type == "poll"
}

// Message represents a Telegram message.
type Message struct {
	ID            int           `json:"id"`
	ChatID        int64         `json:"chat_id"`
	SenderID      int64         `json:"sender_id"`
	SenderName    string        `json:"sender_name"`
	Text          string        `json:"text"`
	Date          time.Time     `json:"date"`
	Out           bool          `json:"out"`
	ReplyToMsgID  int           `json:"reply_to_msg_id,omitempty"`
	ReplyToSender string        `json:"reply_to_sender,omitempty"`
	ReplyToText   string        `json:"reply_to_text,omitempty"`
	ForwardFrom   string        `json:"forward_from,omitempty"`
	ForwardDate   time.Time     `json:"forward_date,omitempty"`
	ForwardPostID int           `json:"forward_post_id,omitempty"`
	Media         *MessageMedia `json:"media,omitempty"`
	EditDate      time.Time     `json:"edit_date,omitempty"`
	Status        string        `json:"status"` // "sending", "sent", "read"
	IsService     bool          `json:"is_service,omitempty"`
}

// PostSearchResult represents a global public channel post search hit (Telegram Premium feature).
type PostSearchResult struct {
	ID              int       `json:"id"`
	ChannelID       int64     `json:"channel_id"`
	ChannelTitle    string    `json:"channel_title"`
	ChannelUsername string    `json:"channel_username,omitempty"`
	PhotoURL        string    `json:"photo_url,omitempty"`
	Text            string    `json:"text"`
	Date            time.Time `json:"date"`
	Views           int       `json:"views,omitempty"`
	Forwards        int       `json:"forwards,omitempty"`
}

// FullState is returned on /api/state.
type FullState struct {
	Auth       AuthState           `json:"auth"`
	Connection ConnectionStateType `json:"connection"`
	User       *UserProfile        `json:"user,omitempty"`
	ChatsCount int                 `json:"chats_count"`
}

// WSMessage is the transport envelope for real-time WebSocket events.
type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// EntityInfo caches peer entity information for fast resolution and display.
type EntityInfo struct {
	ID         int64
	AccessHash int64
	Type       ChatType
	Title      string
	Username   string
	Phone      string
	PhotoURL   string
}
