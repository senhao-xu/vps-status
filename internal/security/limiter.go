package security

import (
	"sync"
	"time"
)

type limitBucket struct {
	windowStart time.Time
	count       int
}

// Limiter is a bounded in-memory fixed-window limiter. When its key capacity is
// exhausted, unseen keys share a fail-closed overflow bucket.
type Limiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	capacity int
	entries  map[string]limitBucket
	overflow limitBucket
	now      func() time.Time
}

// NewLimiter constructs a limiter with a hard key bound.
func NewLimiter(limit int, window time.Duration, capacity int) *Limiter {
	if limit < 1 || window <= 0 || capacity < 1 {
		panic("invalid rate limiter configuration")
	}
	return &Limiter{
		limit: limit, window: window, capacity: capacity,
		entries: make(map[string]limitBucket, capacity), now: time.Now,
	}
}

// Allow consumes one attempt and reports whether it is within the limit.
func (limiter *Limiter) Allow(key string) (bool, time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now()
	bucket, exists := limiter.entries[key]
	if !exists && len(limiter.entries) >= limiter.capacity {
		for candidate, value := range limiter.entries {
			if now.Sub(value.windowStart) >= limiter.window {
				delete(limiter.entries, candidate)
			}
		}
	}
	if !exists && len(limiter.entries) >= limiter.capacity {
		return limiter.consume(now, &limiter.overflow)
	}
	allowed, retry := limiter.consume(now, &bucket)
	limiter.entries[key] = bucket
	return allowed, retry
}

func (limiter *Limiter) consume(now time.Time, bucket *limitBucket) (bool, time.Duration) {
	if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= limiter.window {
		bucket.windowStart = now
		bucket.count = 0
	}
	bucket.count++
	if bucket.count <= limiter.limit {
		return true, 0
	}
	retry := limiter.window - now.Sub(bucket.windowStart)
	if retry < 0 {
		retry = 0
	}
	return false, retry
}
