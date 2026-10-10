package sources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fastHTTP() *HTTP {
	h := NewHTTP("wera-test/1.0 (+https://example.com)")
	h.BaseDelay = time.Millisecond
	return h
}

func TestGetJSONSendsUserAgentAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "wera-test/1.0 (+https://example.com)" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("headers: %v", r.Header)
		}
		w.Write([]byte(`{"n": 3}`))
	}))
	defer srv.Close()
	var out struct{ N int }
	if err := fastHTTP().GetJSON(context.Background(), srv.URL, &out); err != nil || out.N != 3 {
		t.Fatalf("got %+v, %v", out, err)
	}
}

func TestConditionalGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	h := fastHTTP()
	var out map[string]any
	etag, err := h.GetJSONIfChanged(context.Background(), srv.URL, "", &out)
	if err != nil || etag != `"v1"` {
		t.Fatalf("first: %q %v", etag, err)
	}
	if _, err := h.GetJSONIfChanged(context.Background(), srv.URL, etag, &out); !errors.Is(err, ErrNotModified) {
		t.Errorf("want ErrNotModified, got %v", err)
	}
}

func TestRetriesServerErrorsThenSucceeds(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var out map[string]any
	if err := fastHTTP().GetJSON(context.Background(), srv.URL, &out); err != nil || calls.Load() != 3 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}

func TestGivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	err := fastHTTP().GetJSON(context.Background(), srv.URL, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "giving up after 4 attempts") || calls.Load() != 4 {
		t.Errorf("calls=%d err=%v", calls.Load(), err)
	}
}

func TestLongRetryAfterStopsAtOnce(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	start := time.Now()
	err := fastHTTP().GetJSON(context.Background(), srv.URL, &struct{}{})
	if err == nil || calls.Load() != 1 || time.Since(start) > 5*time.Second {
		t.Errorf("calls=%d took=%s err=%v", calls.Load(), time.Since(start), err)
	}
}

func TestNotFoundAndBadStatusesFailFast(t *testing.T) {
	for status, want := range map[int]error{http.StatusNotFound: ErrBoardNotFound, http.StatusForbidden: nil} {
		var calls atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(status)
		}))
		err := fastHTTP().GetJSON(context.Background(), srv.URL, &struct{}{})
		srv.Close()
		if err == nil || calls.Load() != 1 || (want != nil && !errors.Is(err, want)) {
			t.Errorf("status %d: calls=%d err=%v", status, calls.Load(), err)
		}
	}
}

func TestMaintenanceRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/maintenance-page", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("down")) })
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/maintenance-page", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	if err := fastHTTP().GetJSON(context.Background(), srv.URL+"/jobs", &struct{}{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("want ErrUnavailable, got %v", err)
	}
}

func TestPostJSONSendsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || string(b) != `{"limit":20}` || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request: %s %s %v", r.Method, b, r.Header)
		}
		w.Write([]byte(`{"total": 1}`))
	}))
	defer srv.Close()
	var out struct{ Total int }
	if err := fastHTTP().PostJSON(context.Background(), srv.URL, map[string]int{"limit": 20}, &out); err != nil || out.Total != 1 {
		t.Errorf("%+v %v", out, err)
	}
}

func TestBadJSONAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	if err := fastHTTP().GetJSON(context.Background(), srv.URL, &struct{}{}); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("bad JSON: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fastHTTP().GetJSON(ctx, srv.URL, &struct{}{}); err == nil {
		t.Error("canceled context should fail")
	}
}

func TestRegistryGetAndFetchAll(t *testing.T) {
	if _, err := Get(map[string]Source{}, "nope"); err == nil {
		t.Error("unknown ATS should error")
	}
	if truncate([]byte("abcdef"), 3) != "abc..." || truncate([]byte("ab"), 3) != "ab" {
		t.Error("truncate")
	}
}
