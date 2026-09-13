package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tdlibgo/internal/logger"
)

var floodWaitRegex = regexp.MustCompile(`FLOOD_WAIT_(\d+)`)

// IsRetryable checks if an MTProto RPC error can be recovered via silent retry.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "deadline exceeded") ||
		strings.Contains(s, "flood_wait") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "bad record mac") ||
		strings.Contains(s, "eof") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "timeout")
}

// ExtractFloodWait extracts the required wait seconds if the error is FLOOD_WAIT_X.
func ExtractFloodWait(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	matches := floodWaitRegex.FindStringSubmatch(err.Error())
	if len(matches) == 2 {
		if sec, err := strconv.Atoi(matches[1]); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second, true
		}
	}
	return 0, false
}

// RetryConfig customizes retry behavior.
type RetryConfig struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	PerAttemptTimeout time.Duration
}

// DefaultRetryConfig returns production-ready resilient defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:       3,
		InitialBackoff:    500 * time.Millisecond,
		MaxBackoff:        4 * time.Second,
		PerAttemptTimeout: 25 * time.Second,
	}
}

// UploadRetryConfig returns retry configuration tailored for media uploads.
func UploadRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:       3,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        6 * time.Second,
		PerAttemptTimeout: 120 * time.Second,
	}
}

// WithRetry executes the given action with exponential backoff and silent recovery.
func WithRetry(ctx context.Context, opName string, cfg RetryConfig, action func(ctx context.Context) error) error {
	var lastErr error
	backoff := cfg.InitialBackoff

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Create an isolated context per attempt so previous cancellations don't infect the retry
		timeout := cfg.PerAttemptTimeout
		if timeout <= 0 {
			timeout = 20 * time.Second
		}
		attemptCtx, cancel := context.WithTimeout(context.Background(), timeout)
		err := action(attemptCtx)
		cancel()

		if err == nil {
			if attempt > 1 {
				logger.Retry("%s succeeded on attempt %d/%d", opName, attempt, cfg.MaxAttempts)
			}
			return nil
		}

		lastErr = err

		// Handle Telegram FLOOD_WAIT specifically
		if waitDur, ok := ExtractFloodWait(err); ok {
			logger.Warn("FLOOD_WAIT detected during %s: waiting %v before retry (attempt %d/%d)", opName, waitDur, attempt, cfg.MaxAttempts)
			if waitDur > 60*time.Second {
				// Don't sleep excessively in interactive flow
				return fmt.Errorf("flood wait too long (%v): %w", waitDur, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(waitDur + 500*time.Millisecond):
				continue
			}
		}

		if !IsRetryable(err) || attempt == cfg.MaxAttempts {
			break
		}

		logger.Retry("%s failed (attempt %d/%d): %v. Retrying in %v...", opName, attempt, cfg.MaxAttempts, err, backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
	}

	return lastErr
}
