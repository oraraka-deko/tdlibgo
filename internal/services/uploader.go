package services

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/iyear/tdl/core/dcpool"
	"github.com/iyear/tdl/core/util/mediautil"

	"tdlibgo/internal/logger"
	"tdlibgo/internal/models"
)

// UploaderService manages concurrent file uploading and MTProto media dispatch.
type UploaderService struct {
	pool    dcpool.Pool
	mainAPI *tg.Client
	upPool  *uploader.Uploader
}

// NewUploaderService creates an uploader worker engine with dedicated data pool and primary API.
func NewUploaderService(pool dcpool.Pool, mainAPI *tg.Client) *UploaderService {
	var poolAPI *tg.Client
	if pool != nil {
		poolAPI = pool.Default(context.Background())
	} else {
		poolAPI = mainAPI
	}

	// Configure uploader with 4 parallel threads and 512KB chunks
	u := uploader.NewUploader(poolAPI).
		WithThreads(4).
		WithPartSize(512 * 1024)

	return &UploaderService{
		pool:    pool,
		mainAPI: mainAPI,
		upPool:  u,
	}
}

// UploadAndSendMedia uploads file bytes and dispatches MessagesSendMedia to Telegram.
func (s *UploaderService) UploadAndSendMedia(
	ctx context.Context,
	peer tg.InputPeerClass,
	normChatID int64,
	fileName string,
	data []byte,
	mediaType string,
	caption string,
	replyToMsgID int,
) (*models.Message, error) {
	if s.mainAPI == nil || s.upPool == nil {
		return nil, errors.New("uploader service not initialized")
	}

	start := time.Now()
	details := DetectMedia(fileName, data)
	if mediaType != "" {
		details.Type = MediaType(mediaType)
	}

	// For video, extract MP4 container duration and resolution using tdl/core/util/mediautil
	if mediautil.IsVideo(details.MimeType) {
		if dur, w, h, err := mediautil.GetMP4Info(bytes.NewReader(data)); err == nil {
			if details.Width == 0 {
				details.Width = w
			}
			if details.Height == 0 {
				details.Height = h
			}
			details.Duration = dur
		}
	}

	logger.Upload("Starting upload: %s (%s, %d bytes) to chat %d", fileName, details.MimeType, len(data), normChatID)

	// Upload file chunks using parallel uploader
	inputFile, err := s.upPool.FromBytes(ctx, fileName, data)
	if err != nil {
		logger.Error("UPLOAD", "Failed uploading %s to MTProto: %v", fileName, err)
		return nil, errors.Wrap(err, "upload file chunks")
	}

	uploadDuration := time.Since(start)
	logger.Upload("Uploaded %s chunks in %v, constructing MTProto media payload...", fileName, uploadDuration)

	// Construct MTProto InputMedia
	var inputMedia tg.InputMediaClass

	switch details.Type {
	case MediaPhoto:
		inputMedia = &tg.InputMediaUploadedPhoto{
			File: inputFile,
		}

	case MediaVideo:
		videoAttr := &tg.DocumentAttributeVideo{
			SupportsStreaming: true,
			W:                 details.Width,
			H:                 details.Height,
		}
		if details.Duration > 0 {
			videoAttr.Duration = float64(details.Duration)
		}
		inputMedia = &tg.InputMediaUploadedDocument{
			File:     inputFile,
			MimeType: details.MimeType,
			Attributes: []tg.DocumentAttributeClass{
				videoAttr,
				&tg.DocumentAttributeFilename{
					FileName: fileName,
				},
			},
		}

	case MediaAudio, MediaVoice:
		isVoice := (details.Type == MediaVoice)
		inputMedia = &tg.InputMediaUploadedDocument{
			File:     inputFile,
			MimeType: details.MimeType,
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeAudio{
					Voice: isVoice,
					Title: fileName,
				},
				&tg.DocumentAttributeFilename{
					FileName: fileName,
				},
			},
		}

	default: // Document / General file
		inputMedia = &tg.InputMediaUploadedDocument{
			File:     inputFile,
			MimeType: details.MimeType,
			Attributes: []tg.DocumentAttributeClass{
				&tg.DocumentAttributeFilename{
					FileName: fileName,
				},
			},
		}
	}

	randomID := rand.Int63()
	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		Media:    inputMedia,
		Message:  caption,
		RandomID: randomID,
	}

	if replyToMsgID > 0 {
		req.SetReplyTo(&tg.InputReplyToMessage{
			ReplyToMsgID: replyToMsgID,
		})
	}

	// Dispatch media message to Telegram via primary MTProto session
	sendStart := time.Now()
	updatesClass, err := s.mainAPI.MessagesSendMedia(ctx, req)
	if err != nil {
		logger.Error("UPLOAD", "MessagesSendMedia failed for %s: %v", fileName, err)
		return nil, errors.Wrap(err, "send media message")
	}

	totalDuration := time.Since(start)
	logger.Upload("Media %s sent successfully in %v (upload: %v, send: %v)", fileName, totalDuration, uploadDuration, time.Since(sendStart))

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

	// Create local media model so UI renders media immediately
	mediaURL := fmt.Sprintf("/api/media?chat_id=%d&message_id=%d", normChatID, msgID)
	mediaModel := &models.MessageMedia{
		Type:     string(details.Type),
		URL:      mediaURL,
		FileName: fileName,
		FileSize: int64(len(data)),
		MimeType: details.MimeType,
		Width:    details.Width,
		Height:   details.Height,
	}

	// For photos, create mini data URL if reasonably small
	if details.Type == MediaPhoto && len(data) < 2*1024*1024 {
		mediaModel.ThumbURL = mediaURL
	}

	return &models.Message{
		ID:           msgID,
		ChatID:       normChatID,
		SenderName:   "You",
		Text:         caption,
		Date:         date,
		Out:          true,
		ReplyToMsgID: replyToMsgID,
		Media:        mediaModel,
		Status:       "sent",
	}, nil
}
