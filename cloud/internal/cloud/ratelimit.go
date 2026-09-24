package cloud

import (
	"sync"
	"time"
)

// RateLimiter is a token bucket keyed by caller.
//
// It is deliberately in-process and in-memory: the target is a single
// self-hosted instance, and losing buckets on restart is an acceptable price
// for having no dependency and no shared state to operate.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens replenished per second
	burst   float64 // bucket capacity
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter returns a limiter that refills rate tokens per second up to
// burst.
func NewRateLimiter(rate, burst float64) *RateLimiter {
	if rate <= 0 {
		rate = 1
	}
	if burst <= 0 {
		burst = rate
	}
	return &RateLimiter{
		buckets: map[string]*bucket{},
		rate:    rate,
		burst:   burst,
		now:     time.Now,
	}
}

// Allow reports whether key may proceed, consuming one token when it may.
func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	b, ok := r.buckets[key]
	if !ok {
		b = &bucket{tokens: r.burst, last: now}
		r.buckets[key] = b
	}

	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * r.rate
		if b.tokens > r.burst {
			b.tokens = r.burst
		}
		b.last = now
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Prune drops buckets idle for longer than the given duration, so a flood of
// distinct callers cannot grow memory without bound.
func (r *RateLimiter) Prune(idle time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := r.now().Add(-idle)
	for key, b := range r.buckets {
		if b.last.Before(cutoff) {
			delete(r.buckets, key)
		}
	}
}
