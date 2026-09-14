package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-faster/errors"
	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/tmedia"

	"tdlibgo/internal/logger"
)

// MediaInspectionResult contains detailed MTProto and technical metadata for any message's media.
type MediaInspectionResult struct {
	ChatID            int64                  `json:"chat_id"`
	MessageID         int                    `json:"message_id"`
	HasMedia          bool                   `json:"has_media"`
	TLType            string                 `json:"tl_type"`
	MediaType         string                 `json:"media_type"` // photo, document, video, audio, etc.
	FileName          string                 `json:"file_name"`
	FileSize          int64                  `json:"file_size"`
	FileSizeFormatted string                 `json:"file_size_formatted"`
	MimeType          string                 `json:"mime_type"`
	DCID              int                    `json:"dc_id"`
	Date              int64                  `json:"date"`
	HasThumb          bool                   `json:"has_thumb"`
	ThumbSize         int64                  `json:"thumb_size,omitempty"`
	CanConvert        bool                   `json:"can_convert"`
	Attributes        map[string]interface{} `json:"attributes"`
	DirectURL         string                 `json:"direct_url"`
}

// MediaHubService provides access to deep tmedia extraction, inspection, and conversion tools.
type MediaHubService struct {
	mainAPI *tg.Client
}

// NewMediaHubService initializes the media hub service.
func NewMediaHubService(mainAPI *tg.Client) *MediaHubService {
	return &MediaHubService{
		mainAPI: mainAPI,
	}
}

// InspectMessageMedia extracts full tmedia information and MTProto attributes from a message.
func (s *MediaHubService) InspectMessageMedia(
	ctx context.Context,
	chatID int64,
	messageID int,
	peer tg.InputPeerClass,
) (*MediaInspectionResult, error) {
	if s.mainAPI == nil {
		return nil, errors.New("telegram API not ready")
	}

	// Fetch message
	inputMsgs := []tg.InputMessageClass{&tg.InputMessageID{ID: messageID}}
	var fetchedMsgs []tg.MessageClass

	if channel, ok := peer.(*tg.InputPeerChannel); ok {
		chRes, err := s.mainAPI.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{
				ChannelID:  channel.ChannelID,
				AccessHash: channel.AccessHash,
			},
			ID: inputMsgs,
		})
		if err != nil {
			return nil, fmt.Errorf("channels get messages: %w", err)
		}
		switch r := chRes.(type) {
		case *tg.MessagesChannelMessages:
			fetchedMsgs = r.Messages
		case *tg.MessagesMessages:
			fetchedMsgs = r.Messages
		case *tg.MessagesMessagesSlice:
			fetchedMsgs = r.Messages
		}
	} else {
		mRes, err := s.mainAPI.MessagesGetMessages(ctx, inputMsgs)
		if err != nil {
			return nil, fmt.Errorf("messages get messages: %w", err)
		}
		switch r := mRes.(type) {
		case *tg.MessagesMessages:
			fetchedMsgs = r.Messages
		case *tg.MessagesMessagesSlice:
			fetchedMsgs = r.Messages
		case *tg.MessagesChannelMessages:
			fetchedMsgs = r.Messages
		}
	}

	if len(fetchedMsgs) == 0 {
		return nil, errors.New("message not found")
	}

	msg, ok := fetchedMsgs[0].(*tg.Message)
	if !ok {
		return nil, errors.New("not a standard message")
	}

	mediaClass, hasMedia := msg.GetMedia()
	if !hasMedia || mediaClass == nil {
		return &MediaInspectionResult{
			ChatID:    chatID,
			MessageID: messageID,
			HasMedia:  false,
		}, nil
	}

	result := &MediaInspectionResult{
		ChatID:     chatID,
		MessageID:  messageID,
		HasMedia:   true,
		Attributes: make(map[string]interface{}),
		DirectURL:  fmt.Sprintf("/api/media?chat_id=%d&message_id=%d", chatID, messageID),
	}

	// 1. Test ConvInputMedia from tdl/core/tmedia
	_, canConv := tmedia.ConvInputMedia(mediaClass)
	result.CanConvert = canConv

	// 2. Extract media
	mediaInfo, extracted := tmedia.ExtractMedia(mediaClass)
	if extracted && mediaInfo != nil {
		result.FileName = mediaInfo.Name
		result.FileSize = mediaInfo.Size
		result.FileSizeFormatted = formatBytes(mediaInfo.Size)
		result.DCID = mediaInfo.DC
		result.Date = mediaInfo.Date
	}

	// 3. Inspect TL structures
	switch m := mediaClass.(type) {
	case *tg.MessageMediaPhoto:
		result.TLType = "MessageMediaPhoto"
		result.MediaType = "photo"
		result.MimeType = "image/jpeg"
		if p, ok := m.Photo.(*tg.Photo); ok {
			result.Attributes["photo_id"] = p.ID
			result.Attributes["dc_id"] = p.DCID
			result.Attributes["sizes_count"] = len(p.Sizes)
			if tp, size, ok := tmedia.GetPhotoSize(p.Sizes); ok {
				result.Attributes["best_thumb_type"] = tp
				result.Attributes["best_size"] = size
			}
		}

	case *tg.MessageMediaDocument:
		result.TLType = "MessageMediaDocument"
		if doc, ok := m.Document.(*tg.Document); ok {
			result.MimeType = doc.MimeType
			result.Attributes["document_id"] = doc.ID
			result.Attributes["dc_id"] = doc.DCID

			// Check thumb
			if thumb, ok := tmedia.GetDocumentThumb(doc); ok {
				result.HasThumb = true
				result.ThumbSize = thumb.Size
			}

			// Attributes
			for _, attr := range doc.Attributes {
				switch a := attr.(type) {
				case *tg.DocumentAttributeVideo:
					result.MediaType = "video"
					result.Attributes["duration_seconds"] = a.Duration
					result.Attributes["width"] = a.W
					result.Attributes["height"] = a.H
					result.Attributes["supports_streaming"] = a.SupportsStreaming
				case *tg.DocumentAttributeAudio:
					result.MediaType = "audio"
					result.Attributes["duration_seconds"] = a.Duration
					result.Attributes["title"] = a.Title
					result.Attributes["performer"] = a.Performer
					result.Attributes["is_voice"] = a.Voice
				case *tg.DocumentAttributeAnimated:
					result.MediaType = "animation"
					result.Attributes["is_animated"] = true
				case *tg.DocumentAttributeSticker:
					result.MediaType = "sticker"
					result.Attributes["alt"] = a.Alt
				case *tg.DocumentAttributeFilename:
					result.Attributes["original_filename"] = a.FileName
				case *tg.DocumentAttributeCustomEmoji:
					result.MediaType = "custom_emoji"
					result.Attributes["alt"] = a.Alt
				}
			}

			if result.MediaType == "" {
				nameLower := strings.ToLower(result.FileName)
				if strings.HasSuffix(nameLower, ".mp4") || strings.HasSuffix(nameLower, ".mkv") {
					result.MediaType = "video"
				} else if strings.HasSuffix(nameLower, ".mp3") || strings.HasSuffix(nameLower, ".m4a") {
					result.MediaType = "audio"
				} else {
					result.MediaType = "document"
				}
			}
		}

	case *tg.MessageMediaGeo:
		result.TLType = "MessageMediaGeo"
		result.MediaType = "geo"

	case *tg.MessageMediaContact:
		result.TLType = "MessageMediaContact"
		result.MediaType = "contact"
		result.Attributes["phone"] = m.PhoneNumber
		result.Attributes["first_name"] = m.FirstName

	case *tg.MessageMediaVenue:
		result.TLType = "MessageMediaVenue"
		result.MediaType = "venue"
		result.Attributes["title"] = m.Title
		result.Attributes["address"] = m.Address

	case *tg.MessageMediaGame:
		result.TLType = "MessageMediaGame"
		result.MediaType = "game"
		result.Attributes["title"] = m.Game.Title

	case *tg.MessageMediaPoll:
		result.TLType = "MessageMediaPoll"
		result.MediaType = "poll"
		result.Attributes["question"] = m.Poll.Question.Text

	case *tg.MessageMediaDice:
		result.TLType = "MessageMediaDice"
		result.MediaType = "dice"
		result.Attributes["value"] = m.Value
		result.Attributes["emoticon"] = m.Emoticon

	default:
		result.TLType = fmt.Sprintf("%T", mediaClass)
		result.MediaType = "other"
	}

	logger.Info("MEDIA_HUB", "Inspected media for [%d:%d]: %s (%s, %d bytes)", chatID, messageID, result.FileName, result.MediaType, result.FileSize)
	return result, nil
}
