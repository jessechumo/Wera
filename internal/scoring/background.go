package scoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// responsesRequest is the Coral Responses-API body (PLAN.md section 8:
// background mode for the deep review). "input" is the OpenAI Responses
// shape: an array of {role, content} items.
type responsesRequest struct {
	Model           string    `json:"model"`
	Instructions    string    `json:"instructions,omitempty"`
	Input           []Message `json:"input"`
	Background      bool      `json:"background"`
	PromptCacheKey  string    `json:"prompt_cache_key,omitempty"`
	MaxOutputTokens int       `json:"max_output_tokens,omitempty"`
	Temperature     float64   `json:"temperature"`
}

// responsesUsage mirrors the usage block on the Responses API. It nests
// cost inside usage (unlike chat completions, where cost is top-level).
// Coral reports Responses-API names (input_tokens/output_tokens/
// input_tokens_details); the chat-completions aliases are accepted too.
type responsesUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	InputTokens         int `json:"input_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	OutputTokens        int `json:"output_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	InputTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	Cost *float64 `json:"cost"`
}

// tokens normalizes the two naming schemes: prompt, cached, completion.
func (u responsesUsage) tokens() (prompt, cached, completion int) {
	prompt = u.PromptTokens
	if prompt == 0 {
		prompt = u.InputTokens
	}
	completion = u.CompletionTokens
	if completion == 0 {
		completion = u.OutputTokens
	}
	cached = u.PromptTokensDetails.CachedTokens
	if cached == 0 {
		cached = u.InputTokensDetails.CachedTokens
	}
	return prompt, cached, completion
}

// responseStatusError carries the OpenAI error shape on a failed response.
type responseStatusError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// outputContent is one content item inside an output message
// ({"type":"output_text","text":"..."}).
type outputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// outputItem is one item of the response output array. Only "message"
// items with "output_text" content are consumed.
type outputItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content []outputContent `json:"content"`
}

// responsesStatus is the subset of the Responses-API object we consume
// while polling a background response.
type responsesStatus struct {
	ID         string               `json:"id"`
	Object     string               `json:"object"`
	Status     string               `json:"status"`
	Model      string               `json:"model"`
	OutputText string               `json:"output_text,omitempty"`
	Output     []outputItem         `json:"output"`
	Usage      responsesUsage       `json:"usage"`
	Error      *responseStatusError `json:"error"`
	Incomplete *incompleteDetails   `json:"incomplete_details"`
}

// incompleteDetails explains an "incomplete" status.
type incompleteDetails struct {
	Reason string `json:"reason"`
}

// Background response statuses (PLAN.md section 8: the status moves
// through queued, in_progress, then a terminal state). "incomplete" is
// a reasoning-model outcome: generation stopped at max_output_tokens
// before any visible output was produced.
const (
	StatusQueued     = "queued"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
	StatusIncomplete = "incomplete"
)

// TerminalStatus reports whether a status can no longer change:
// anything other than queued/in_progress (and the empty pre-parse
// value, which keeps a malformed poll retriable).
func TerminalStatus(s string) bool {
	return s != "" && s != StatusQueued && s != StatusInProgress
}

// ResponseState is one poll result for a background response. Content,
// Usage, and CostUSD are only set once Status is StatusCompleted.
type ResponseState struct {
	ID      string
	Status  string
	Content string
	Usage   UsageStats
	CostUSD *float64
	ErrText string // failure detail when Status is failed/cancelled
}

// CreateBackgroundResponse submits a background request to
// {baseURL}/responses (with background: true) and returns the queued
// response id. The 429 retry policy matches Chat. Instructions and
// input should keep the shared profile prefix byte-identical across
// calls so the prompt cache serves every job after the first.
func (c *Client) CreateBackgroundResponse(ctx context.Context, cacheKey, instructions string, input []Message, maxOutputTokens int) (string, error) {
	body, err := json.Marshal(responsesRequest{
		Model:           c.model,
		Instructions:    instructions,
		Input:           input,
		Background:      true,
		PromptCacheKey:  cacheKey,
		MaxOutputTokens: maxOutputTokens,
		Temperature:     0.1,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	endpoint := c.baseURL + "/responses"

	backoffs := []time.Duration{
		c.baseBackoff, c.baseBackoff * 2, c.baseBackoff * 4, c.baseBackoff * 8,
	}
	var lastErr error
	attempt := 0
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return "", fmt.Errorf("request: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK:
			if readErr != nil {
				return "", fmt.Errorf("read response: %w", readErr)
			}
			var rs responsesStatus
			if err := json.Unmarshal(raw, &rs); err != nil {
				return "", fmt.Errorf("decode response: %w", err)
			}
			if rs.ID == "" {
				return "", fmt.Errorf("response has no id: %s", truncate(raw, 300))
			}
			return rs.ID, nil
		case http.StatusTooManyRequests:
			lastErr = fmt.Errorf("HTTP 429 from %s", endpoint)
			if c.OnRateLimit != nil {
				c.OnRateLimit()
			}
		default:
			return "", fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, endpoint, truncate(raw, 300))
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
			return "", ctx.Err()
		}
		attempt++
	}
	return "", fmt.Errorf("giving up after 429 retries: %w", lastErr)
}

// RetrieveResponse polls GET {baseURL}/responses/{id}. On a terminal
// failed/cancelled status the error detail (if any) is carried in
// ErrText, not as a Go error.
func (c *Client) RetrieveResponse(ctx context.Context, id string) (*ResponseState, error) {
	endpoint := c.baseURL + "/responses/" + id
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, endpoint, truncate(raw, 300))
	}

	var rs responsesStatus
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	state := &ResponseState{ID: rs.ID, Status: rs.Status}

	// Prefer the aggregate output_text when present, else assemble the
	// message items' output_text content in order.
	state.Content = rs.OutputText
	if state.Content == "" {
		for _, item := range rs.Output {
			if item.Type != "message" {
				continue
			}
			for _, ctn := range item.Content {
				if ctn.Type == "output_text" {
					state.Content += ctn.Text
				}
			}
		}
	}

	switch {
	case rs.Status == StatusCompleted:
		prompt, cached, completion := rs.Usage.tokens()
		state.Usage = UsageStats{
			PromptTokens:     int64(prompt),
			CachedTokens:     int64(cached),
			CompletionTokens: int64(completion),
		}
		state.CostUSD = rs.Usage.Cost
	case TerminalStatus(rs.Status):
		switch {
		case rs.Error != nil && rs.Error.Message != "":
			state.ErrText = rs.Error.Message
		case rs.Incomplete != nil && rs.Incomplete.Reason != "":
			state.ErrText = "incomplete: " + rs.Incomplete.Reason
		default:
			state.ErrText = "response " + rs.Status
		}
	}
	return state, nil
}
