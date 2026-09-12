package telegram

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/gotd/td/telegram/thumbnail"
	"github.com/gotd/td/tg"

	"tdlibgo/internal/models"
)

// ExtractStrippedThumbURL converts Telegram's stripped mini-JPEG bytes into a base64 data URL.
func ExtractStrippedThumbURL(thumb []byte) string {
	if len(thumb) == 0 {
		return ""
	}
	expanded, err := thumbnail.Expand(thumb)
	if err != nil {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(expanded)
}

// ExtractMessageMedia parses tg.MessageMediaClass into models.MessageMedia.
func ExtractMessageMedia(msg *tg.Message, chatID int64) (media *models.MessageMedia) {
	defer func() {
		if r := recover(); r != nil {
			media = nil
		}
	}()

	if msg == nil || msg.Media == nil {
		return nil
	}

	mediaURL := fmt.Sprintf("/api/media?chat_id=%d&message_id=%d", chatID, msg.ID)

	switch m := msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		if m.Photo == nil {
			return nil
		}
		photo, ok := m.Photo.AsNotEmpty()
		if !ok {
			return nil
		}

		var thumbURL string
		var maxW, maxH int

		for _, sz := range photo.Sizes {
			switch s := sz.(type) {
			case *tg.PhotoStrippedSize:
				thumbURL = ExtractStrippedThumbURL(s.Bytes)
			case *tg.PhotoSize:
				if s.W > maxW {
					maxW = s.W
					maxH = s.H
				}
			case *tg.PhotoSizeProgressive:
				if s.W > maxW {
					maxW = s.W
					maxH = s.H
				}
			}
		}

		return &models.MessageMedia{
			Type:     "photo",
			URL:      mediaURL,
			ThumbURL: thumbURL,
			Width:    maxW,
			Height:   maxH,
			MimeType: "image/jpeg",
		}

	case *tg.MessageMediaDocument:
		if m.Document == nil {
			return nil
		}
		doc, ok := m.Document.AsNotEmpty()
		if !ok {
			return nil
		}

		mediaType := "document"
		fileName := ""
		altEmoji := ""
		duration := 0
		w := 0
		h := 0
		isAnimated := false

		for _, attr := range doc.Attributes {
			switch a := attr.(type) {
			case *tg.DocumentAttributeSticker:
				mediaType = "sticker"
				altEmoji = a.Alt
			case *tg.DocumentAttributeAnimated:
				mediaType = "gif"
				isAnimated = true
			case *tg.DocumentAttributeVideo:
				if mediaType != "gif" {
					mediaType = "video"
				}
				duration = int(a.Duration)
				w = a.W
				h = a.H
			case *tg.DocumentAttributeAudio:
				if a.Voice {
					mediaType = "voice"
				} else {
					mediaType = "audio"
				}
				duration = a.Duration
				if a.Title != "" {
					fileName = a.Title
				}
			case *tg.DocumentAttributeFilename:
				fileName = a.FileName
			}
		}

		// Extract thumbnail if available
		var thumbURL string
		for _, t := range doc.Thumbs {
			if s, ok := t.(*tg.PhotoStrippedSize); ok {
				thumbURL = ExtractStrippedThumbURL(s.Bytes)
			}
		}

		if fileName == "" {
			fileName = fmt.Sprintf("file_%d", doc.ID)
			switch {
			case strings.Contains(doc.MimeType, "zip"):
				fileName += ".zip"
			case strings.Contains(doc.MimeType, "pdf"):
				fileName += ".pdf"
			case strings.Contains(doc.MimeType, "webp"):
				fileName += ".webp"
			case strings.Contains(doc.MimeType, "mp4"):
				fileName += ".mp4"
			}
		}

		return &models.MessageMedia{
			Type:       mediaType,
			URL:        mediaURL,
			ThumbURL:   thumbURL,
			FileName:   fileName,
			FileSize:   doc.Size,
			MimeType:   doc.MimeType,
			Width:      w,
			Height:     h,
			Duration:   duration,
			AltEmoji:   altEmoji,
			IsAnimated: isAnimated,
		}

	case *tg.MessageMediaPoll:
		if m.Poll.Answers == nil {
			return nil
		}
		poll := m.Poll
		var options []models.PollOption
		totalVotes := 0
		voterMap := make(map[string]int)
		chosenMap := make(map[string]bool)
		if m.Results.Results != nil {
			totalVotes = m.Results.TotalVoters
			for _, r := range m.Results.Results {
				optKey := string(r.Option)
				voterMap[optKey] = r.Voters
				chosenMap[optKey] = r.Chosen
			}
		}
		for _, ansClass := range poll.Answers {
			ans, ok := ansClass.(*tg.PollAnswer)
			if !ok {
				continue
			}
			optKey := string(ans.Option)
			voters := voterMap[optKey]
			percent := 0
			if totalVotes > 0 {
				percent = (voters * 100) / totalVotes
			}
			options = append(options, models.PollOption{
				Text:    ans.Text.Text,
				Voters:  voters,
				Percent: percent,
				Chosen:  chosenMap[optKey],
			})
		}
		return &models.MessageMedia{
			Type: "poll",
			Poll: &models.PollInfo{
				ID:          poll.ID,
				Question:    poll.Question.Text,
				Answers:     options,
				TotalVoters: totalVotes,
				Closed:      poll.Closed,
				Quiz:        poll.Quiz,
				Multiple:    poll.MultipleChoice,
			},
		}
	}

	return nil
}

// InspectMediaText returns a descriptive label when the message text caption is blank.
func InspectMediaText(media tg.MessageMediaClass) (res string) {
	defer func() {
		if r := recover(); r != nil {
			res = ""
		}
	}()

	if media == nil {
		return ""
	}
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		return "📷 Photo"
	case *tg.MessageMediaDocument:
		if doc, ok := m.Document.(*tg.Document); ok {
			for _, attr := range doc.Attributes {
				switch a := attr.(type) {
				case *tg.DocumentAttributeSticker:
					if a.Alt != "" {
						return a.Alt + " Sticker"
					}
					return "Sticker"
				case *tg.DocumentAttributeAnimated:
					return "GIF"
				case *tg.DocumentAttributeAudio:
					if a.Voice {
						return "🎤 Voice message"
					}
					return "🎵 Audio"
				case *tg.DocumentAttributeVideo:
					return "🎬 Video"
				case *tg.DocumentAttributeFilename:
					return "📁 " + a.FileName
				}
			}
		}
		return "📁 Document"
	case *tg.MessageMediaGeo:
		return "📍 Location"
	case *tg.MessageMediaContact:
		return "👤 Contact"
	case *tg.MessageMediaPoll:
		q := m.Poll.Question.Text
		if q != "" {
			return "📊 Poll: " + q
		}
		return "📊 Poll"
	case *tg.MessageMediaDice:
		return fmt.Sprintf("🎲 %s (%d)", m.Emoticon, m.Value)
	case *tg.MessageMediaWebPage:
		return "🔗 Link"
	}
	return ""
}
