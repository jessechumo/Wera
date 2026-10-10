package auth

import (
	"sync"
	"time"
)

// Limiter is a fixed-window rate limiter keyed by client (IP), kept in
// memory: good enough for one API process.
type Limiter struct {
	Max    int
	Window time.Duration

	mu      sync.Mutex
	windows map[string]*window
}

type window struct {
	start time.Time
	count int
}

// NewLimiter allows max events per key per window.
func NewLimiter(max int, w time.Duration) *Limiter {
	return &Limiter{Max: max, Window: w, windows: map[string]*window{}}
}

// Allow records one event for key and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.windows) > 10000 {
		for k, w := range l.windows { // drop expired windows to bound memory
			if now.Sub(w.start) > l.Window {
				delete(l.windows, k)
			}
		}
	}
	w, ok := l.windows[key]
	if !ok || now.Sub(w.start) > l.Window {
		l.windows[key] = &window{start: now, count: 1}
		return true
	}
	w.count++
	return w.count <= l.Max
}
