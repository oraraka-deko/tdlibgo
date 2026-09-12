package telegram

import (
	"context"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/tg"

	"tdlibgo/internal/models"
)

// SearchGlobalPosts performs a Telegram Premium global post search across public channels.
func (c *ClientController) SearchGlobalPosts(ctx context.Context, query string, offsetID, limit int) ([]models.PostSearchResult, error) {
	if c.api == nil {
		return nil, errors.New("client MTProto API is not initialized")
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	req := &tg.MessagesSearchGlobalRequest{
		BroadcastsOnly: true, // Search exclusively within channel posts (global post search)
		Q:              query,
		Filter:         &tg.InputMessagesFilterEmpty{},
		OffsetPeer:     &tg.InputPeerEmpty{},
		OffsetID:       offsetID,
		Limit:          limit,
	}

	res, err := c.api.MessagesSearchGlobal(ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "search global posts")
	}

	mod, ok := res.AsModified()
	if !ok {
		return []models.PostSearchResult{}, nil
	}

	// Map channels by ID
	channelsMap := make(map[int64]*models.EntityInfo)
	for _, cClass := range mod.GetChats() {
		if ch, ok := cClass.(*tg.Channel); ok {
			var photoURL string
			if p, ok := ch.Photo.(*tg.ChatPhoto); ok {
				photoURL = ExtractStrippedThumbURL(p.StrippedThumb)
			}
			channelsMap[ch.ID] = &models.EntityInfo{
				ID:         ch.ID,
				AccessHash: ch.AccessHash,
				Type:       models.ChatTypeChannel,
				Title:      ch.Title,
				Username:   ch.Username,
				PhotoURL:   photoURL,
			}
		}
	}

	var results []models.PostSearchResult
	for _, mClass := range mod.GetMessages() {
		m, ok := mClass.(*tg.Message)
		if !ok {
			continue
		}

		channelID := c.extractPeerID(m.PeerID)
		channelTitle := "Channel"
		channelUsername := ""
		photoURL := ""

		if ch, exists := channelsMap[channelID]; exists {
			channelTitle = ch.Title
			channelUsername = ch.Username
			photoURL = ch.PhotoURL
		}

		text := m.Message
		if text == "" && m.Media != nil {
			text = InspectMediaText(m.Media)
		}

		results = append(results, models.PostSearchResult{
			ID:              m.ID,
			ChannelID:       channelID,
			ChannelTitle:    channelTitle,
			ChannelUsername: channelUsername,
			PhotoURL:        photoURL,
			Text:            text,
			Date:            time.Unix(int64(m.Date), 0),
			Views:           m.Views,
			Forwards:        m.Forwards,
		})
	}

	return results, nil
}
