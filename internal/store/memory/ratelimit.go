package memory

import (
	"context"
	"sync"
	"time"

	"tdlibgo/internal/store"
)

// RateLimiter is a thread-safe, in-memory sliding window rate limiter
// implementing store.RateLimiter for Zero-Docker mode and tests.
type RateLimiter struct {
	mu      sync.Mutex
	records map[string][]time.Time
}

var _ store.RateLimiter = (*RateLimiter)(nil)

// NewRateLimiter creates a new in-memory rate limiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		records: make(map[string][]time.Time),
	}
}

// Allow reports whether a single action is allowed under the rate limit.
func (r *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error) {
	return r.AllowN(ctx, key, 1, limit, window)
}

// AllowN reports whether N actions are allowed under the rate limit.
func (r *RateLimiter) AllowN(ctx context.Context, key string, cost, limit int, window time.Duration) (bool, int, error) {
	if limit <= 0 || cost <= 0 {
		return true, 0, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	// Filter out expired timestamps
	timestamps := r.records[key]
	valid := timestamps[:0]
	for _, t := range timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid)+cost > limit {
		// Calculate retryAfter in seconds
		retryAfter := 1
		if len(valid) > 0 {
			oldest := valid[0]
			remaining := oldest.Add(window).Sub(now)
			if remaining > 0 {
				retryAfter = int(remaining.Seconds()) + 1
			}
		}
		r.records[key] = valid
		return false, retryAfter, nil
	}

	// Record the new timestamps
	for i := 0; i < cost; i++ {
		valid = append(valid, now)
	}
	r.records[key] = valid

	// Opportunistic cleanup if map gets large
	if len(r.records) > 5000 {
		for k, v := range r.records {
			if len(v) == 0 || v[len(v)-1].Before(cutoff) {
				delete(r.records, k)
			}
		}
	}

	return true, 0, nil
}
