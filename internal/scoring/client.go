package scoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// request is the Coral chat-completions request body.
type request struct {
	Model          string    `json:"model"`
	PromptCacheKey string    `json:"prompt_cache_key,omitempty"`
	Temperature    float64   `json:"temperature"`
	MaxTokens      int       `json:"max_tokens"`
	Messages       []Message `json:"messages"`
}

// completionUsage mirrors the OpenAI-style usage block. Cached tokens are
// nested in prompt_tokens_details.
type completionUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// response is the (subset of the) chat-completions reply we consume.
type response struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage completionUsage `json:"usage"`
	// Cost is Coral's own authoritative per-call cost in USD; used when
	// present, otherwise we compute from the price table.
	Cost *float64 `json:"cost"`
}

// Completion is one successful model reply.
type Completion struct {
	Content string
	Usage   UsageStats
	// CostUSD is the API-reported cost when the provider supplies one.
	CostUSD *float64
}

// UsageStats carries the token accounting for one call.
type UsageStats struct {
	PromptTokens     int64
	CachedTokens     int64
	CompletionTokens int64
}

// Client talks to a Coral Bricks (OpenAI-compatible) chat-completions
// endpoint. It applies 60s timeouts and retries 429s with backoff.
type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	model   string

	// OnRateLimit, when set, is invoked once for every 429 response so
	// the scorer can halve its effective concurrency.
	OnRateLimit func()

	// logFirstCall ensures the full usage object is logged once so field
	// names can be checked against what Coral actually returns.
	logFirst sync.Once
	log      *slog.Logger

	maxTokens   int
	baseBackoff time.Duration // first 429 backoff; doubles each retry
}

// ClientOptions configures NewClient.
type ClientOptions struct {
	BaseURL   string
	APIKey    string
	Model     string
	Timeout   time.Duration // default 60s
	MaxTokens int           // default 600
	Log       *slog.Logger
	// Backoff is the base delay before the first 429 retry; the sequence
	// doubles each time (1s, 2s, 4s, 8s by default). Tests shrink it.
	Backoff time.Duration
}

// NewClient builds a scoring client.
func NewClient(opts ClientOptions) *Client {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 600
	}
	backoff := opts.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	return &Client{
		http:        &http.Client{Timeout: timeout},
		baseURL:     trimTrailingSlash(opts.BaseURL),
		apiKey:      opts.APIKey,
		model:       opts.Model,
		log:         opts.Log,
		maxTokens:   maxTokens,
		baseBackoff: backoff,
	}
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// Chat sends the messages to {baseURL}/chat/completions with the shared
// prompt cache key. On 429 it retries up to 5 attempts, honoring
// Retry-After when present and exponential backoff (1s, 2s, 4s, 8s)
// otherwise, notifying OnRateLimit each time.
func (c *Client) Chat(ctx context.Context, cacheKey string, messages []Message) (*Completion, error) {
	body, err := json.Marshal(request{
		Model:          c.model,
		PromptCacheKey: cacheKey,
		Temperature:    0.1,
		MaxTokens:      c.maxTokens,
		Messages:       messages,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	endpoint := c.baseURL + "/chat/completions"

	backoffs := []time.Duration{
		c.baseBackoff, c.baseBackoff * 2, c.baseBackoff * 4, c.baseBackoff * 8,
	}
	var lastErr error
	attempt := 0
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK:
			if readErr != nil {
				return nil, fmt.Errorf("read response: %w", readErr)
			}
			return c.decode(raw)
		case http.StatusTooManyRequests:
			lastErr = fmt.Errorf("HTTP 429 from %s", endpoint)
			if c.OnRateLimit != nil {
				c.OnRateLimit()
			}
		default:
			return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, endpoint, truncate(raw, 300))
		}

		if attempt >= len(backoffs) {
			break
		}
		delay := backoffs[attempt]
		if secs, perr := strconv.Atoi(resp.Header.Get("Retry-After")); perr == nil && secs > 0 {
			if d := time.Duration(secs) * time.Second; d > delay {
				delay = d
			}
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		attempt++
	}
	return nil, fmt.Errorf("giving up after 429 retries: %w", lastErr)
}

// decode parses a 200 response body and logs the full usage object once.
func (c *Client) decode(raw []byte) (*Completion, error) {
	var resp response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("response has no choices")
	}
	u := resp.Usage
	if c.log != nil {
		c.logFirst.Do(func() {
			var pretty map[string]any
			if json.Unmarshal(raw, &pretty) == nil {
				if usageRaw, ok := pretty["usage"]; ok {
					c.log.Info("first scoring usage object", "usage", usageRaw)
				}
			}
		})
	}
	return &Completion{
		Content: resp.Choices[0].Message.Content,
		Usage: UsageStats{
			PromptTokens:     int64(u.PromptTokens),
			CachedTokens:     int64(u.PromptTokensDetails.CachedTokens),
			CompletionTokens: int64(u.CompletionTokens),
		},
		CostUSD: resp.Cost,
	}, nil
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}
