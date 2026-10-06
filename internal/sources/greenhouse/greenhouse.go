// Package greenhouse implements the Source interface for the Greenhouse
// public job board API.
package greenhouse

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"

	"wera/internal/normalize"
	"wera/internal/sources"
)

const (
	defaultBaseURL = "https://boards-api.greenhouse.io"
	name           = "greenhouse"
)

// Adapter fetches jobs from the Greenhouse job board API.
type Adapter struct {
	http    *sources.HTTP
	baseURL string
}

// New returns a Greenhouse adapter using the shared HTTP client.
func New(h *sources.HTTP) *Adapter {
	return &Adapter{http: h, baseURL: defaultBaseURL}
}

// NewWithBaseURL is New with a custom API base URL (used by tests).
func NewWithBaseURL(h *sources.HTTP, baseURL string) *Adapter {
	return &Adapter{http: h, baseURL: strings.TrimRight(baseURL, "/")}
}

// Name implements sources.Source.
func (a *Adapter) Name() string { return name }

type boardResponse struct {
	Jobs []boardJob `json:"jobs"`
}

type boardJob struct {
	ID             int64  `json:"id"`
	Title          string `json:"title"`
	AbsoluteURL    string `json:"absolute_url"`
	UpdatedAt      string `json:"updated_at"`
	FirstPublished string `json:"first_published"`
	// Content is HTML-escaped HTML; it must be unescaped before stripping.
	Content  string `json:"content"`
	Location struct {
		Name string `json:"name"`
	} `json:"location"`
	Departments []struct {
		Name string `json:"name"`
	} `json:"departments"`
}

// Fetch implements sources.Source: it returns all open jobs on the board.
func (a *Adapter) Fetch(ctx context.Context, token string) ([]sources.RawJob, error) {
	endpoint := fmt.Sprintf("%s/v1/boards/%s/jobs?content=true", a.baseURL, url.PathEscape(token))
	var resp boardResponse
	if err := a.http.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}

	jobs := make([]sources.RawJob, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		raw := sources.RawJob{
			ExtID:           strconv.FormatInt(j.ID, 10),
			Title:           strings.TrimSpace(j.Title),
			LocationRaw:     strings.TrimSpace(j.Location.Name),
			URL:             j.AbsoluteURL,
			DescriptionHTML: html.UnescapeString(j.Content),
			DescriptionText: normalize.HTMLToText(j.Content),
			PostedAt:        ParseTimestamp(j.FirstPublished),
		}
		if len(j.Departments) > 0 {
			raw.Department = j.Departments[0].Name
		}
		if raw.IsRemote == nil && strings.Contains(strings.ToLower(raw.LocationRaw), "remote") {
			remote := true
			raw.IsRemote = &remote
		}
		jobs = append(jobs, raw)
	}
	return jobs, nil
}

// ParseTimestamp parses a Greenhouse timestamp ("2026-09-01T12:00:00Z" and
// offset variants), returning nil for empty or unparseable values.
func ParseTimestamp(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}
