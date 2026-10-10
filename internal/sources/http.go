package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrBoardNotFound signals a permanent 404 for a board token. It is never
// retried and is recorded as last_fetch_error = "404 token not found".
var ErrBoardNotFound = errors.New("404 token not found")

// ErrUnavailable signals that the job board is down for maintenance (it
// redirected to a maintenance page). It is not retried; the fetcher
// defers the company instead of counting a failure.
var ErrUnavailable = errors.New("job board is down for maintenance")

// maxRetryAfter is the longest Retry-After the client waits out; a longer
// one ends the attempt (the next run tries again).
const maxRetryAfter = time.Minute

// maxBodyBytes caps one response. Boards with descriptions inline get big
// (Anduril's Greenhouse board was 43 MB in October 2026).
const maxBodyBytes = 96 << 20

// HTTP is the shared, polite HTTP client used by every adapter: 15s
// timeout, a descriptive User-Agent, transparent gzip (Go's transport),
// and up to 3 retries with exponential backoff + jitter on 429/5xx.
// 404s fail immediately.
type HTTP struct {
	Client     *http.Client
	UA         string
	MaxRetries int
	BaseDelay  time.Duration // delay before the first retry
}

// NewHTTP builds the shared client with the plan's defaults.
func NewHTTP(ua string) *HTTP {
	return &HTTP{
		Client:     &http.Client{Timeout: 15 * time.Second},
		UA:         ua,
		MaxRetries: 3,
		BaseDelay:  500 * time.Millisecond,
	}
}

// GetJSON performs GET url and unmarshals the JSON body into out.
func (h *HTTP) GetJSON(ctx context.Context, url string, out any) error {
	_, err := h.doJSON(ctx, http.MethodGet, url, nil, "", out)
	return err
}

// ErrNotModified is returned by GetJSONIfChanged when the server answered
// 304: the content behind the ETag has not changed.
var ErrNotModified = errors.New("not modified")

// GetJSONIfChanged is GetJSON with a conditional request: when etag is
// set it is sent as If-None-Match, and a 304 answer returns
// ErrNotModified without a body. It returns the response's ETag.
func (h *HTTP) GetJSONIfChanged(ctx context.Context, url, etag string, out any) (string, error) {
	return h.doJSON(ctx, http.MethodGet, url, nil, etag, out)
}

// PostJSON sends payload as a JSON POST body to url and unmarshals the
// JSON reply into out (Workday's job search is a POST).
func (h *HTTP) PostJSON(ctx context.Context, url string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	_, err = h.doJSON(ctx, http.MethodPost, url, body, "", out)
	return err
}

// doJSON performs one request with the retry policy described on HTTP and
// returns the response ETag.
func (h *HTTP) doJSON(ctx context.Context, method, url string, payload []byte, etag string, out any) (string, error) {
	maxAttempts := h.MaxRetries + 1
	delay := h.BaseDelay
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var reqBody io.Reader
		if payload != nil {
			reqBody = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
		if err != nil {
			return "", fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("User-Agent", h.UA)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}

		resp, err := h.Client.Do(req)
		if err == nil && redirectedToMaintenance(resp) {
			resp.Body.Close()
			return "", fmt.Errorf("%w (redirected to %s)", ErrUnavailable, resp.Request.URL)
		}
		if err != nil {
			lastErr = fmt.Errorf("request: %w", err)
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
			resp.Body.Close()
			switch {
			case readErr != nil:
				// The connection broke mid-body: retry like a 5xx.
				lastErr = fmt.Errorf("read response from %s: %w", url, readErr)
			case len(body) > maxBodyBytes:
				return "", fmt.Errorf("response from %s is larger than %d MiB", url, maxBodyBytes>>20)
			case resp.StatusCode == http.StatusNotFound:
				return "", fmt.Errorf("%w: %s", ErrBoardNotFound, url)
			case resp.StatusCode == http.StatusNotModified && etag != "":
				return etag, ErrNotModified
			case resp.StatusCode == http.StatusOK:
				if err := json.Unmarshal(body, out); err != nil {
					return "", fmt.Errorf("decode response from %s: %w", url, err)
				}
				return resp.Header.Get("ETag"), nil
			case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
				lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
				if resp.StatusCode == http.StatusTooManyRequests {
					if secs, perr := strconv.Atoi(resp.Header.Get("Retry-After")); perr == nil && secs > 0 {
						d := time.Duration(secs) * time.Second
						if d > maxRetryAfter {
							// The board asked us to back off for a while: stop
							// now and try again next run instead of waiting.
							return "", fmt.Errorf("%w (retry after %s)", lastErr, d)
						}
						if d > delay {
							delay = d
						}
					}
				}
			default:
				return "", fmt.Errorf("unexpected HTTP %d from %s: %s", resp.StatusCode, url, truncate(body, 200))
			}
		}

		if attempt == maxAttempts {
			break
		}
		// Exponential backoff with jitter: [delay, delay*1.5).
		sleep := delay + time.Duration(rand.Int63n(int64(delay/2)+1)) //nolint:gosec // jitter, not security
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		delay *= 2
	}
	return "", fmt.Errorf("giving up after %d attempts: %w", maxAttempts, lastErr)
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}

// redirectedToMaintenance reports whether the request ended on a
// maintenance page after redirects (Workday sends every tenant to
// community.workday.com/maintenance-page during its weekly window).
func redirectedToMaintenance(resp *http.Response) bool {
	final := resp.Request.URL
	return final != nil && strings.Contains(strings.ToLower(final.Path), "maintenance")
}
