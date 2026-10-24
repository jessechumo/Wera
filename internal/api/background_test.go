package api

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedWriter is a log sink the test reads while a goroutine writes to it.
type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}

func TestBackgroundSurvivesAPanic(t *testing.T) {
	buf := &lockedWriter{w: &bytes.Buffer{}}
	s := &Server{Log: slog.New(slog.NewTextHandler(buf, nil))}
	done := make(chan struct{})
	s.background("test task", func() {
		defer close(done)
		panic("boom")
	})
	<-done
	// The recover runs after fn's own deferred close: give it a moment to log.
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(buf.String(), "background task panicked") {
		if time.Now().After(deadline) {
			t.Fatalf("the panic was not logged: %q", buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "task=\"test task\"") || !strings.Contains(buf.String(), "panic=boom") {
		t.Fatalf("log lacks the task or the panic: %q", buf.String())
	}
}
