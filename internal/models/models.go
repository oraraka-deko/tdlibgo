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
	StatusText       string    `json:"status_text,omitempty"`
	IsMuted          bool      `json:"is_muted,omitempty"`
	EmojiStatus      string    `json:"emoji_status,omitempty"`
	TopMessageMedia  string    `json:"top_message_media,omitempty"`
	TopMessageOut    bool      `json:"top_message_out,omitempty"`
	TopMessageRead   bool      `json:"top_message_read,omitempty"`
	TypingUser       string    `json:"typing_user,omitempty"`
	MembersCount     int       `json:"members_count,omitempty"`
	IsVerified       bool      `json:"is_verified,omitempty"`
	NoForwards       bool      `json:"noforwards,omitempty"`
	PhotoID          int64     `json:"-"`
	StrippedThumb    string    `json:"stripped_thumb,omitempty"`
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
	Poll          *PollInfo `json:"poll,omitempty"`          // poll details if Type == "poll"
	Transcription string    `json:"transcription,omitempty"` // speech-to-text transcription for voice/audio
}

// StarGiftAttribute describes unique collectible gift attribute (model, pattern, backdrop).
type StarGiftAttribute struct {
	Name         string `json:"name"`
	Type         string `json:"type"` // "model", "pattern", "backdrop"
	CenterColor  int    `json:"center_color,omitempty"`
	EdgeColor    int    `json:"edge_color,omitempty"`
	PatternColor int    `json:"pattern_color,omitempty"`
	TextColor    int    `json:"text_color,omitempty"`
	Rarity       string `json:"rarity,omitempty"` // e.g. "Rare", "Epic", "Legendary"
	Crafted      bool   `json:"crafted,omitempty"`
}

// StarGiftInfo describes an official or unique Telegram star gift.
type StarGiftInfo struct {
	GiftID       int64               `json:"gift_id"`
	Title        string              `json:"title"`
	Slug         string              `json:"slug,omitempty"`
	Num          int                 `json:"num,omitempty"`
	Stars        int64               `json:"stars"`
	ConvertStars int64               `json:"convert_stars,omitempty"`
	FromID       int64               `json:"from_id,omitempty"`
	FromName     string              `json:"from_name,omitempty"`
	ToID         int64               `json:"to_id,omitempty"`
	ToName       string              `json:"to_name,omitempty"`
	Message      string              `json:"message,omitempty"`
	IsUnique     bool                `json:"is_unique"`
	IsUpgrade    bool                `json:"is_upgrade"`
	IsRefunded   bool                `json:"is_refunded,omitempty"`
	CanExportAt  int                 `json:"can_export_at,omitempty"`
	CanTransfer  bool                `json:"can_transfer,omitempty"`
	Model        string              `json:"model,omitempty"`
	Symbol       string              `json:"symbol,omitempty"`
	Backdrop     string              `json:"backdrop,omitempty"`
	CenterColor  string              `json:"center_color,omitempty"`
	EdgeColor    string              `json:"edge_color,omitempty"`
	TextColor    string              `json:"text_color,omitempty"`
	PatternColor string              `json:"pattern_color,omitempty"`
	StickerURL   string              `json:"sticker_url,omitempty"`
	ThumbURL     string              `json:"thumb_url,omitempty"`
	Date         time.Time           `json:"date,omitempty"`
	Attributes   []StarGiftAttribute `json:"attributes,omitempty"`
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
	Media         *MessageMedia   `json:"media,omitempty"`
	StarGift      *StarGiftInfo   `json:"star_gift,omitempty"`
	Reactions     []ReactionCount `json:"reactions,omitempty"`
	EditDate      time.Time       `json:"edit_date,omitempty"`
	Views         int             `json:"views,omitempty"`
	Forwards      int             `json:"forwards,omitempty"`
	NoForwards    bool            `json:"noforwards,omitempty"`
	Status        string          `json:"status"` // "sending", "sent", "read"
	IsService     bool            `json:"is_service,omitempty"`
}

// ReactionCount describes message reaction emoji and count.
type ReactionCount struct {
	Reaction string `json:"reaction"`
	Count    int    `json:"count"`
	Chosen   bool   `json:"chosen"`
}

// BotCommandItem represents a bot slash command.
type BotCommandItem struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// BotMenuButtonItem represents bot custom menu button.
type BotMenuButtonItem struct {
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
	Type string `json:"type"` // "commands", "web_app", "default"
}

// UserFullDetails represents complete user or bot profile details.
type UserFullDetails struct {
	ID                int64              `json:"id"`
	About             string             `json:"about,omitempty"`
	Birthday          string             `json:"birthday,omitempty"`
	PersonalChannelID int64              `json:"personal_channel_id,omitempty"`
	StarGiftsCount    int                `json:"stargifts_count,omitempty"`
	BusinessAddress   string             `json:"business_address,omitempty"`
	BusinessHours     string             `json:"business_hours,omitempty"`
	BotDescription    string             `json:"bot_description,omitempty"`
	BotCommands       []BotCommandItem   `json:"bot_commands,omitempty"`
	BotMenuButton     *BotMenuButtonItem `json:"bot_menu_button,omitempty"`
	CommonChatsCount  int                `json:"common_chats_count,omitempty"`
	PinnedMsgID       int                `json:"pinned_msg_id,omitempty"`
	Gifts             []*StarGiftInfo    `json:"gifts,omitempty"`
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
	StatusText   string
	IsOnline     bool
	MembersCount  int
	IsVerified    bool
	NoForwards    bool
	PhotoID       int64
	StrippedThumb string
}
