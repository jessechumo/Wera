// Package embed turns jobs and profiles into embedding vectors through an
// OpenAI-compatible /embeddings endpoint (by default a small local model
// served by the wera-embed container), so every pending job can be ranked
// for a user without an AI call. Each posting is embedded once and shared
// by everyone; each profile text once.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// batchSize keeps each request small enough for a CPU-only server.
const batchSize = 32

// Client calls the embeddings endpoint.
type Client struct {
	BaseURL string // e.g. http://wera-embed/v1
	Model   string
	APIKey  string // optional
	HTTP    *http.Client
}

// New returns a client, or nil when no endpoint is configured.
func New(baseURL, model, apiKey string) *Client {
	if baseURL == "" {
		return nil
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Model: model, APIKey: apiKey,
		HTTP: &http.Client{Timeout: 2 * time.Minute}}
}

// Embed returns one unit-length vector per text, in order.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		vecs, err := c.batch(ctx, texts[start:min(start+batchSize, len(texts))])
		if err != nil {
			return out, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (c *Client) batch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{"input": texts, "model": c.Model})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("embeddings: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("embeddings: decode: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: got %d vectors for %d texts", len(parsed.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	for i, d := range parsed.Data {
		idx := d.Index
		if idx < 0 || idx >= len(texts) || out[idx] != nil {
			idx = i // servers that omit index answer in order
		}
		if len(d.Embedding) == 0 {
			return nil, errors.New("embeddings: empty vector")
		}
		out[idx] = Normalize(d.Embedding)
	}
	return out, nil
}

// Normalize scales v to unit length (in place) and returns it.
func Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
	return v
}

// Cosine is the similarity of two unit vectors (0 when sizes differ).
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var s float32
	for i, x := range a {
		s += x * b[i] //nolint:gosec // len(a) == len(b), checked above
	}
	return float64(s)
}

// JobText is what a posting is embedded as: its title and the facts the
// AI extracted (seniority, skills, a short digest). Facts read far better
// than raw postings, which open with company boilerplate.
func JobText(title, seniority string, skills []string, digest string) string {
	return fmt.Sprintf("%s. Seniority: %s. Skills: %s. %s", title, seniority, strings.Join(skills, ", "), digest)
}

var heading = regexp.MustCompile(`(?m)^## `)

// ProfileText is what a profile is embedded as: its target roles, skills
// and licenses sections (what the user wants and can do; older profiles
// call the skills section "Technical skills"). Education
// and history add noise and push past the model's input limit. A profile
// without those sections is used whole.
func ProfileText(markdown string) string {
	var keep []string
	locs := heading.FindAllStringIndex(markdown, -1)
	for i, loc := range locs {
		end := len(markdown)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		sec := markdown[loc[0]:end]
		title := strings.ToLower(strings.TrimSpace(strings.SplitN(sec, "\n", 2)[0]))
		if strings.HasPrefix(title, "## target roles") || strings.HasPrefix(title, "## technical skills") ||
			strings.HasPrefix(title, "## skills") || strings.HasPrefix(title, "## licenses") {
			keep = append(keep, strings.TrimSpace(sec))
		}
	}
	if len(keep) == 0 {
		return markdown
	}
	return strings.Join(keep, "\n\n")
}
