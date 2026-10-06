package scoring

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// jsonDecode reads the request body into out.
func jsonDecode(r *http.Request, out any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

const fakeReply = `{"choices":[{"message":{"content":"{\"fit_score\":80,\"verdict\":\"good\",\"seniority\":\"junior\",\"years_required\":null,\"sponsorship\":\"unknown\",\"sponsorship_quote\":null,\"us_eligible\":true,\"work_mode\":\"remote\",\"location_summary\":\"Remote\",\"skills_matched\":[],\"skills_missing\":[],\"reason\":\"ok\"}"}}],"usage":{"prompt_tokens":1000,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":900}}}`

func okHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}
}

func TestChatSuccess(t *testing.T) {
	srv := httptest.NewServer(okHandler(fakeReply))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL + "/", APIKey: "cb_test", Model: "glm-5.3-flash-fast"})
	comp, err := c.Chat(context.Background(), "wera-profile-abc", []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if comp.Usage.PromptTokens != 1000 || comp.Usage.CachedTokens != 900 || comp.Usage.CompletionTokens != 50 {
		t.Errorf("usage: %+v", comp.Usage)
	}
	if comp.Content == "" {
		t.Error("empty content")
	}
}

func TestChatAuthAndBody(t *testing.T) {
	var gotAuth, gotCache string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req request
		if err := jsonDecode(r, &req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotCache = req.PromptCacheKey
		w.Write([]byte(fakeReply))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "glm-5.3-flash-fast"})
	if _, err := c.Chat(context.Background(), "wera-profile-xyz", []Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotAuth != "Bearer cb_test" {
		t.Errorf("auth header: %q", gotAuth)
	}
	if gotCache != "wera-profile-xyz" {
		t.Errorf("prompt_cache_key: %q", gotCache)
	}
}

func TestChat429Backoff(t *testing.T) {
	var calls atomic.Int32
	var rateLimitSeen atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(fakeReply))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "m", Timeout: 5 * time.Second, Backoff: time.Millisecond})
	c.OnRateLimit = func() { rateLimitSeen.Add(1) }
	comp, err := c.Chat(context.Background(), "k", []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat after 429s: %v", err)
	}
	if comp.Usage.PromptTokens != 1000 {
		t.Errorf("usage: %+v", comp.Usage)
	}
	if calls.Load() != 3 {
		t.Errorf("want 3 calls (2x429 + success), got %d", calls.Load())
	}
	if rateLimitSeen.Load() != 2 {
		t.Errorf("OnRateLimit: want 2, got %d", rateLimitSeen.Load())
	}
}

func TestChat429GivesUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "m", Timeout: 5 * time.Second, Backoff: time.Millisecond})
	if _, err := c.Chat(context.Background(), "k", nil); err == nil {
		t.Fatal("expected error after exhausting 429 retries")
	}
}

func TestChatServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "m", Timeout: 5 * time.Second})
	if _, err := c.Chat(context.Background(), "k", nil); err == nil {
		t.Fatal("expected error on 500")
	}
}
