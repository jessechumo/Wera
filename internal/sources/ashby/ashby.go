// Package ashby implements the Source interface for the Ashby public
// posting API.
package ashby

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"wera/internal/sources"
)

const (
	defaultBaseURL = "https://api.ashbyhq.com"
	name           = "ashby"
)

// Adapter fetches jobs from the Ashby job board posting API.
type Adapter struct {
	http    *sources.HTTP
	baseURL string
}

// New returns an Ashby adapter using the shared HTTP client.
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
	ID       string `json:"id"`
	Title    string `json:"title"`
	Location string `json:"location"`
	// Department/Team: prefer the department, fall back to the team.
	Department string `json:"department"`
	Team       string `json:"team"`
	JobURL     string `json:"jobUrl"`
	// SecondaryLocations holds additional office locations as objects
	// with a "location" display name (their address may be null).
	SecondaryLocations []struct {
		Location string `json:"location"`
	} `json:"secondaryLocations"`
	// IsListed: postings hidden from the board are skipped entirely.
	IsListed      bool   `json:"isListed"`
	IsRemote      bool   `json:"isRemote"`
	WorkplaceType string `json:"workplaceType"`
	PublishedAt   string `json:"publishedAt"`
	// DescriptionHtml is the HTML posting; DescriptionPlain is the
	// pre-rendered text version. Either may be missing on a board.
	DescriptionHTML  string `json:"descriptionHtml"`
	DescriptionPlain string `json:"descriptionPlain"`
}

// Fetch implements sources.Source: it returns all listed jobs on the board.
func (a *Adapter) Fetch(ctx context.Context, token string) ([]sources.RawJob, error) {
	endpoint := fmt.Sprintf("%s/posting-api/job-board/%s?includeCompensation=true",
		a.baseURL, url.PathEscape(token))
	var resp boardResponse
	if err := a.http.GetJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}

	jobs := make([]sources.RawJob, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		if !j.IsListed {
			continue // hidden posting, not part of the public board
		}
		raw := sources.RawJob{
			ExtID:           j.ID,
			Title:           strings.TrimSpace(j.Title),
			LocationRaw:     joinLocations(j.Location, j.SecondaryLocations),
			URL:             j.JobURL,
			Department:      firstNonEmpty(j.Department, j.Team),
			DescriptionHTML: j.DescriptionHTML,
			DescriptionText: j.DescriptionPlain,
			PostedAt:        ParseTimestamp(j.PublishedAt),
		}
		remote := j.IsRemote || strings.EqualFold(j.WorkplaceType, "remote") ||
			strings.Contains(strings.ToLower(raw.LocationRaw), "remote")
		raw.IsRemote = &remote
		// Some boards omit description (HTML); derive it from the plain
		// text so the field is never empty when the posting has content.
		if raw.DescriptionHTML == "" && raw.DescriptionText != "" {
			raw.DescriptionHTML = "<p>" + strings.ReplaceAll(raw.DescriptionText, "\n\n", "</p><p>") + "</p>"
		}
		jobs = append(jobs, raw)
	}
	return jobs, nil
}

// joinLocations combines the primary location with any secondary office
// locations so the rule filter sees every location a job could be hired in.
func joinLocations(primary string, secondary []struct {
	Location string `json:"location"`
}) string {
	parts := make([]string, 0, 1+len(secondary))
	if p := strings.TrimSpace(primary); p != "" {
		parts = append(parts, p)
	}
	for _, s := range secondary {
		if s.Location != "" {
			parts = append(parts, s.Location)
		}
	}
	return strings.Join(parts, ", ")
}

// firstNonEmpty returns the first non-blank string, trimmed.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// ParseTimestamp parses an Ashby publishedAt instant ("2026-09-05T12:30:00.000Z").
func ParseTimestamp(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}
