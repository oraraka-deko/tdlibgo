package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/oraraka-deko/transcribe"
	"tdlibgo/internal/logger"
	"tdlibgo/internal/storage"
)

// TranscriberService manages whisper audio transcription and persistent caching.
type TranscriberService struct {
	mu          sync.Mutex
	transcriber *transcribe.Transcriber
	modelPath   string
	historyDB   *storage.HistoryDB
	memCache    map[string]string // key: "chatID:messageID"
}

// NewTranscriberService initializes a speech-to-text service.
func NewTranscriberService(historyDB *storage.HistoryDB) *TranscriberService {
	svc := &TranscriberService{
		historyDB: historyDB,
		memCache:  make(map[string]string),
	}

	modelPath := findWhisperModelPath()
	if modelPath != "" {
		svc.modelPath = modelPath
		logger.Info("TRANSCRIBE", "Found Whisper model at: %s", modelPath)
	} else {
		logger.Warn("TRANSCRIBE", "Whisper model not found in standard paths; transcription will initialize when available.")
	}

	return svc
}

// SetHistoryDB sets the persistent database storage.
func (s *TranscriberService) SetHistoryDB(db *storage.HistoryDB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.historyDB = db
}

// ensureTranscriber loads the Whisper model if not already initialized.
func (s *TranscriberService) ensureTranscriber() (*transcribe.Transcriber, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.transcriber != nil {
		return s.transcriber, nil
	}

	if s.modelPath == "" {
		s.modelPath = findWhisperModelPath()
		if s.modelPath == "" {
			return nil, errors.New("whisper model file (ggml-tiny-q8_0.bin or ggml-base-q8_0.bin) not found")
		}
	}

	tr, err := transcribe.New(transcribe.Config{
		ModelPath: s.modelPath,
		Language:  "auto", // multilingual auto-detect
		Threads:   4,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize whisper model %q: %w", s.modelPath, err)
	}

	s.transcriber = tr
	logger.Info("TRANSCRIBE", "Whisper model loaded successfully (%s)", s.modelPath)
	return s.transcriber, nil
}

// GetCachedTranscription checks memory and database cache for an existing transcription.
func (s *TranscriberService) GetCachedTranscription(chatID int64, messageID int) (string, bool) {
	key := fmt.Sprintf("%d:%d", chatID, messageID)

	s.mu.Lock()
	text, found := s.memCache[key]
	s.mu.Unlock()
	if found && text != "" {
		return text, true
	}

	if s.historyDB != nil {
		if dbText, ok := s.historyDB.GetTranscription(chatID, messageID); ok && dbText != "" {
			s.mu.Lock()
			s.memCache[key] = dbText
			s.mu.Unlock()
			return dbText, true
		}
	}

	return "", false
}

// TranscribeAudioFile performs speech-to-text on the provided audio/voice file path.
func (s *TranscriberService) TranscribeAudioFile(ctx context.Context, chatID int64, messageID int, audioPath string) (string, error) {
	// Check cache first
	if cached, ok := s.GetCachedTranscription(chatID, messageID); ok {
		return cached, nil
	}

	tr, err := s.ensureTranscriber()
	if err != nil {
		return "", err
	}

	logger.Info("TRANSCRIBE", "Transcribing audio [%d:%d] from %s...", chatID, messageID, audioPath)
	text, err := tr.TranscribeFile(audioPath)
	if err != nil {
		logger.Error("TRANSCRIBE", "Transcription failed for [%d:%d]: %v", chatID, messageID, err)
		return "", fmt.Errorf("transcription failed: %w", err)
	}

	logger.Info("TRANSCRIBE", "Transcribed [%d:%d]: %q", chatID, messageID, text)

	// Save in memory cache
	key := fmt.Sprintf("%d:%d", chatID, messageID)
	s.mu.Lock()
	s.memCache[key] = text
	s.mu.Unlock()

	// Persist to HistoryDB
	if s.historyDB != nil {
		if err := s.historyDB.SaveTranscription(chatID, messageID, text); err != nil {
			logger.Warn("TRANSCRIBE", "Failed to cache transcription in database: %v", err)
		}
	}

	return text, nil
}

// Close releases the whisper transcriber resources.
func (s *TranscriberService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transcriber != nil {
		err := s.transcriber.Close()
		s.transcriber = nil
		return err
	}
	return nil
}

// findWhisperModelPath searches standard directories for ggml whisper models.
func findWhisperModelPath() string {
	if envPath := os.Getenv("WHISPER_MODEL"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	modelNames := []string{"ggml-tiny-q8_0.bin", "ggml-base-q8_0.bin"}

	// Check current directory and up to 5 parent levels
	if dir, err := os.Getwd(); err == nil {
		curr := dir
		for i := 0; i < 6; i++ {
			for _, name := range modelNames {
				cand1 := filepath.Join(curr, name)
				if _, err := os.Stat(cand1); err == nil {
					return cand1
				}
				cand2 := filepath.Join(curr, "transcribe", name)
				if _, err := os.Stat(cand2); err == nil {
					return cand2
				}
			}
			parent := filepath.Dir(curr)
			if parent == curr {
				break
			}
			curr = parent
		}
	}

	return ""
}
