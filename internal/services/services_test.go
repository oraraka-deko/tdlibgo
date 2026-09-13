package services

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDetectMedia(t *testing.T) {
	// PNG Header
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	meta := DetectMedia("test.png", pngHeader)
	if meta.Type != MediaPhoto || meta.MimeType != "image/png" {
		t.Fatalf("Unexpected PNG meta: %+v", meta)
	}

	// JPEG Header
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	meta = DetectMedia("test.jpg", jpegHeader)
	if meta.Type != MediaPhoto || meta.MimeType != "image/jpeg" {
		t.Fatalf("Unexpected JPEG meta: %+v", meta)
	}

	// MP3 ID3 Header
	mp3Header := []byte{0x49, 0x44, 0x33, 0x03, 0x00, 0x00}
	meta = DetectMedia("song.mp3", mp3Header)
	if meta.Type != MediaAudio || meta.MimeType != "audio/mpeg" {
		t.Fatalf("Unexpected MP3 meta: %+v", meta)
	}

	// MP4 Header
	mp4Header := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D}
	meta = DetectMedia("clip.mp4", mp4Header)
	if meta.Type != MediaVideo || meta.MimeType != "video/mp4" {
		t.Fatalf("Unexpected MP4 meta: %+v", meta)
	}
}

func TestWithRetry_Success(t *testing.T) {
	attempts := 0
	err := WithRetry(context.Background(), "test_op", RetryConfig{
		MaxAttempts:       3,
		InitialBackoff:    5 * time.Millisecond,
		MaxBackoff:        20 * time.Millisecond,
		PerAttemptTimeout: 1 * time.Second,
	}, func(ctx context.Context) error {
		attempts++
		if attempts < 2 {
			return errors.New("context deadline exceeded")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Expected success, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("Expected 2 attempts, got %d", attempts)
	}
}

func TestDownloaderService_CachePath(t *testing.T) {
	dl := NewDownloaderService(nil, "test_cache")
	path := dl.CachePath(12345, 678)
	expected := "test_cache\\12345_678.bin"
	if path != expected && path != "test_cache/12345_678.bin" {
		t.Fatalf("Unexpected cache path: %s", path)
	}

	_, cached := dl.IsCached(12345, 678)
	if cached {
		t.Fatalf("Expected non-cached file")
	}
}

func TestUploaderService_Init(t *testing.T) {
	up := NewUploaderService(nil, nil)
	if up == nil {
		t.Fatalf("Expected non-nil UploaderService")
	}
}
