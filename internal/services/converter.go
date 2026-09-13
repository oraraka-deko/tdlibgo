package services

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// MediaType represents the high-level category of media.
type MediaType string

const (
	MediaPhoto    MediaType = "photo"
	MediaVideo    MediaType = "video"
	MediaAudio    MediaType = "audio"
	MediaVoice    MediaType = "voice"
	MediaDocument MediaType = "document"
)

// MediaDetails holds detected format information.
type MediaDetails struct {
	Type     MediaType
	MimeType string
	Width    int
	Height   int
	Duration int
	FileName string
}

// DetectMedia inspects magic bytes, file extension, and image dimensions.
func DetectMedia(fileName string, data []byte) MediaDetails {
	details := MediaDetails{
		Type:     MediaDocument,
		FileName: fileName,
	}

	if len(data) == 0 {
		details.MimeType = "application/octet-stream"
		return details
	}

	// Sniff HTTP content type
	sniffed := http.DetectContentType(data)
	ext := strings.ToLower(filepath.Ext(fileName))

	// Accurate MIME mapping
	mimeType := sniffed
	if extMime := mime.TypeByExtension(ext); extMime != "" {
		mimeType = strings.Split(extMime, ";")[0]
	}

	// Check magic bytes for common media formats
	switch {
	// JPEG
	case bytes.HasPrefix(data, []byte("\xFF\xD8\xFF")):
		details.Type = MediaPhoto
		details.MimeType = "image/jpeg"

	// PNG
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		details.Type = MediaPhoto
		details.MimeType = "image/png"

	// GIF
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		details.Type = MediaPhoto
		details.MimeType = "image/gif"

	// WebP
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		details.Type = MediaPhoto
		details.MimeType = "image/webp"

	// MP4 / MOV / QuickTime
	case len(data) >= 12 && (string(data[4:8]) == "ftyp" || string(data[4:8]) == "moov"):
		details.Type = MediaVideo
		details.MimeType = "video/mp4"

	// WebM / MKV (Matroska)
	case bytes.HasPrefix(data, []byte("\x1A\x45\xDF\xA3")):
		if strings.HasSuffix(ext, ".webm") {
			details.Type = MediaVideo
			details.MimeType = "video/webm"
		} else {
			details.Type = MediaVideo
			details.MimeType = "video/x-matroska"
		}

	// MP3 (ID3 or sync header)
	case bytes.HasPrefix(data, []byte("ID3")) || (len(data) >= 2 && data[0] == 0xFF && (data[1]&0xE0) == 0xE0):
		details.Type = MediaAudio
		details.MimeType = "audio/mpeg"

	// OGG Opus / Vorbis
	case bytes.HasPrefix(data, []byte("OggS")):
		if strings.HasSuffix(ext, ".oga") || strings.HasSuffix(ext, ".ogg") || strings.HasSuffix(ext, ".opus") {
			details.Type = MediaAudio
			details.MimeType = "audio/ogg"
		} else {
			details.Type = MediaAudio
			details.MimeType = "audio/ogg"
		}

	default:
		if strings.HasPrefix(mimeType, "image/") {
			details.Type = MediaPhoto
		} else if strings.HasPrefix(mimeType, "video/") {
			details.Type = MediaVideo
		} else if strings.HasPrefix(mimeType, "audio/") {
			details.Type = MediaAudio
		} else {
			details.Type = MediaDocument
		}
		details.MimeType = mimeType
	}

	// Try reading image dimensions if photo
	if details.Type == MediaPhoto {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			details.Width = cfg.Width
			details.Height = cfg.Height
		}
	}

	return details
}
