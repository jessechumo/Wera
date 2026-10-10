package sources

import (
	"context"
	"sync"
	"time"
)

// Throttle bounds the requests one adapter sends to a shared backend
// (all Workday tenants sit behind the same infrastructure): at most n in
// flight and at least interval between request starts.
type Throttle struct {
	sem      chan struct{}
	interval time.Duration

	mu   sync.Mutex
	next time.Time
}

// NewThrottle allows n concurrent requests started at most every interval.
func NewThrottle(n int, interval time.Duration) *Throttle {
	return &Throttle{sem: make(chan struct{}, n), interval: interval}
}

// Do runs fn once a slot is free and the start spacing allows it.
func (t *Throttle) Do(ctx context.Context, fn func() error) error {
	select {
	case t.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-t.sem }()

	t.mu.Lock()
	now := time.Now()
	start := t.next
	if start.Before(now) {
		start = now
	}
	t.next = start.Add(t.interval)
	t.mu.Unlock()
	if wait := time.Until(start); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fn()
}

// DetailLimiter is implemented by a DetailSource that wants fewer new
// postings detailed per company per run than the fetcher's default.
type DetailLimiter interface {
	MaxDetailsPerRun() int
}
