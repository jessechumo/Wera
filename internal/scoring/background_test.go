package scoring

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const queuedReply = `{"id":"resp_123","object":"response","status":"queued","model":"glm-5.3-fast"}`

// TestCreateBackgroundRequestShape checks the POST /responses body Coral
// needs for background mode (PLAN.md section 8) and the returned id.
func TestCreateBackgroundRequestShape(t *testing.T) {
	var gotAuth string
	var gotReq responsesRequest
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := jsonDecode(r, &gotReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Write([]byte(queuedReply))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "glm-5.3-fast"})
	id, err := c.CreateBackgroundResponse(context.Background(), "wera-profile-abc",
		"instructions", []Message{{Role: "user", Content: "hi"}}, 2000)
	if err != nil {
		t.Fatalf("CreateBackgroundResponse: %v", err)
	}
	if id != "resp_123" {
		t.Errorf("id: got %q, want resp_123", id)
	}
	if gotPath != "/responses" {
		t.Errorf("path: got %q, want /responses", gotPath)
	}
	if gotAuth != "Bearer cb_test" {
		t.Errorf("auth header: %q", gotAuth)
	}
	if !gotReq.Background {
		t.Error("background must be true")
	}
	if gotReq.Model != "glm-5.3-fast" || gotReq.Instructions != "instructions" ||
		gotReq.PromptCacheKey != "wera-profile-abc" || gotReq.MaxOutputTokens != 2000 {
		t.Errorf("request: %+v", gotReq)
	}
	if len(gotReq.Input) != 1 || gotReq.Input[0].Content != "hi" {
		t.Errorf("input: %+v", gotReq.Input)
	}
}

// TestRetrieveResponseLifecycle polls a fake that goes in_progress then
// completed, and checks content assembly and usage/cost extraction.
func TestRetrieveResponseLifecycle(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses/resp_123" {
			http.NotFound(w, r)
			return
		}
		calls++
		if calls == 1 {
			w.Write([]byte(`{"id":"resp_123","status":"in_progress"}`))
			return
		}
		w.Write([]byte(`{
		  "id": "resp_123", "status": "completed", "model": "glm-5.3-fast",
		  "output": [{"type":"message","role":"assistant",
		              "content":[{"type":"output_text","text":"deep "},{"type":"output_text","text":"reply"}]}],
		  "usage": {"prompt_tokens": 2100, "completion_tokens": 400,
		            "prompt_tokens_details": {"cached_tokens": 2000},
		            "cost": 0.0021}
		}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "glm-5.3-fast"})
	ctx := context.Background()

	s1, err := c.RetrieveResponse(ctx, "resp_123")
	if err != nil {
		t.Fatalf("poll 1: %v", err)
	}
	if s1.Status != StatusInProgress || s1.Content != "" {
		t.Errorf("poll 1: %+v", s1)
	}

	s2, err := c.RetrieveResponse(ctx, "resp_123")
	if err != nil {
		t.Fatalf("poll 2: %v", err)
	}
	if s2.Status != StatusCompleted {
		t.Fatalf("poll 2 status: %q", s2.Status)
	}
	if s2.Content != "deep reply" {
		t.Errorf("content: %q", s2.Content)
	}
	if s2.Usage.PromptTokens != 2100 || s2.Usage.CachedTokens != 2000 || s2.Usage.CompletionTokens != 400 {
		t.Errorf("usage: %+v", s2.Usage)
	}
	if s2.CostUSD == nil || *s2.CostUSD != 0.0021 {
		t.Errorf("cost: %v", s2.CostUSD)
	}
}

// TestRetrieveResponseAggregateOutputText prefers the top-level
// output_text when the provider includes it.
func TestRetrieveResponseAggregateOutputText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"resp_1","status":"completed","output_text":"aggregated"}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "m"})
	s, err := c.RetrieveResponse(context.Background(), "resp_1")
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if s.Content != "aggregated" {
		t.Errorf("content: %q", s.Content)
	}
}

// TestRetrieveResponseFailed checks the terminal failed path.
func TestRetrieveResponseFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"resp_9","status":"failed","error":{"message":"upstream broke","code":"upstream_error"}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "m", Timeout: 5 * time.Second})
	s, err := c.RetrieveResponse(context.Background(), "resp_9")
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if s.Status != StatusFailed || s.ErrText != "upstream broke" {
		t.Errorf("state: %+v", s)
	}
}

// TestRetrieveResponseIncomplete reproduces the live Coral behavior seen
// during M7 acceptance: a reasoning model that exhausts max_output_tokens
// ends "incomplete" with no output, and the Responses API names usage
// fields input_tokens/output_tokens/input_tokens_details.
func TestRetrieveResponseIncomplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
		  "id": "resp_2", "status": "incomplete", "model": "glm-5.3-fp4",
		  "output": [],
		  "usage": {"input_tokens": 2144, "output_tokens": 2000,
		            "input_tokens_details": {"cached_tokens": 0, "cache_write_tokens": 2144},
		            "cost": 0.0124},
		  "incomplete_details": {"reason": "max_output_tokens"}
		}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "m"})
	s, err := c.RetrieveResponse(context.Background(), "resp_2")
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if !TerminalStatus(s.Status) {
		t.Errorf("incomplete must be terminal, got %q", s.Status)
	}
	if s.ErrText != "incomplete: max_output_tokens" {
		t.Errorf("ErrText: %q", s.ErrText)
	}
	if s.Content != "" {
		t.Errorf("content: %q", s.Content)
	}
}

// TestRetrieveResponseUsageNames checks both naming schemes normalize:
// chat-completions (prompt_tokens/...) and Responses (input_tokens/...).
func TestRetrieveResponseUsageNames(t *testing.T) {
	bodies := []string{
		`{"id":"r","status":"completed","output_text":"ok",
		  "usage":{"prompt_tokens":1000,"completion_tokens":50,
		           "prompt_tokens_details":{"cached_tokens":900},"cost":0.001}}`,
		`{"id":"r","status":"completed","output_text":"ok",
		  "usage":{"input_tokens":1000,"output_tokens":50,
		           "input_tokens_details":{"cached_tokens":900},"cost":0.002}}`,
	}
	for i, body := range bodies {
		srv := httptest.NewServer(okHandler(body))
		c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "m"})
		s, err := c.RetrieveResponse(context.Background(), "r")
		srv.Close()
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if s.Usage.PromptTokens != 1000 || s.Usage.CachedTokens != 900 || s.Usage.CompletionTokens != 50 {
			t.Errorf("case %d usage: %+v", i, s.Usage)
		}
	}
}

// TestCreateBackground429Retries reuses the Chat retry policy on submits.
func TestCreateBackground429Retries(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(queuedReply))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientOptions{BaseURL: srv.URL, APIKey: "cb_test", Model: "m", Timeout: 5 * time.Second, Backoff: time.Millisecond})
	id, err := c.CreateBackgroundResponse(context.Background(), "k", "i", []Message{{Role: "user", Content: "x"}}, 100)
	if err != nil || id != "resp_123" {
		t.Fatalf("want resp_123 after one 429, got %q err %v", id, err)
	}
}
