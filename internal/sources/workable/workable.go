// Package workable implements a DetailSource for Workable job boards
// (apply.workable.com/<account>), using the public endpoints the board
// itself calls. The token is the account, e.g. "thorlabs".
package workable

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"wera/internal/normalize"
	"wera/internal/sources"
)

const (
	defaultBaseURL = "https://apply.workable.com"
	name           = "workable"
	// maxDetails bounds new postings detailed per company per run.
	maxDetails = 100
)

// throttle is shared by every Workable account: they sit behind the same
// Cloudflare front, which answers 429 to bursts.
var throttle = sources.NewThrottle(2, 250*time.Millisecond)

// Adapter fetches jobs from Workable job boards.
type Adapter struct {
	http    *sources.HTTP
	baseURL string
}

// New returns a Workable adapter using the shared HTTP client.
func New(h *sources.HTTP) *Adapter { return &Adapter{http: h, baseURL: defaultBaseURL} }

// NewWithBaseURL is New with a custom base URL (used by tests).
func NewWithBaseURL(h *sources.HTTP, baseURL string) *Adapter {
	return &Adapter{http: h, baseURL: strings.TrimRight(baseURL, "/")}
}

// Name implements sources.Source.
func (a *Adapter) Name() string { return name }

// MaxDetailsPerRun implements sources.DetailLimiter.
func (a *Adapter) MaxDetailsPerRun() int { return maxDetails }

type location struct {
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country"`
	Hidden  bool   `json:"hidden"`
}

func (l location) String() string {
	var parts []string
	for _, p := range []string{l.City, l.Region, l.Country} {
		if p = strings.TrimSpace(p); p != "" && (len(parts) == 0 || parts[len(parts)-1] != p) {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

type widgetResponse struct {
	Jobs []struct {
		Title         string     `json:"title"`
		Shortcode     string     `json:"shortcode"`
		URL           string     `json:"url"`
		Department    string     `json:"department"`
		Telecommuting bool       `json:"telecommuting"`
		PublishedOn   string     `json:"published_on"`
		Locations     []location `json:"locations"`
	} `json:"jobs"`
}

// List implements sources.DetailSource: every published posting, without
// descriptions.
func (a *Adapter) List(ctx context.Context, token string) ([]sources.RawJob, error) {
	var resp widgetResponse
	endpoint := fmt.Sprintf("%s/api/v1/widget/accounts/%s", a.baseURL, url.PathEscape(token))
	if err := throttle.Do(ctx, func() error { return a.http.GetJSON(ctx, endpoint, &resp) }); err != nil {
		return nil, err
	}
	jobs := make([]sources.RawJob, 0, len(resp.Jobs))
	seen := map[string]bool{}
	for _, j := range resp.Jobs {
		if j.Shortcode == "" || seen[j.Shortcode] {
			continue
		}
		seen[j.Shortcode] = true
		locs := make([]string, 0, len(j.Locations))
		for _, l := range j.Locations {
			if s := l.String(); s != "" && !l.Hidden {
				locs = append(locs, s)
			}
		}
		raw := sources.RawJob{
			ExtID:       j.Shortcode,
			Title:       strings.TrimSpace(j.Title),
			LocationRaw: strings.Join(locs, "; "),
			URL:         j.URL,
			Department:  strings.TrimSpace(j.Department),
		}
		if raw.URL == "" {
			raw.URL = fmt.Sprintf("%s/%s/j/%s", defaultBaseURL, token, j.Shortcode)
		}
		remote := j.Telecommuting || strings.Contains(strings.ToLower(raw.LocationRaw), "remote")
		raw.IsRemote = &remote
		if t, err := time.Parse("2006-01-02", j.PublishedOn); err == nil {
			raw.PostedAt = &t
		}
		jobs = append(jobs, raw)
	}
	return jobs, nil
}

type detailResponse struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	Requirements string `json:"requirements"`
	Benefits     string `json:"benefits"`
	Workplace    string `json:"workplace"`
	Remote       bool   `json:"remote"`
}

// Detail implements sources.DetailSource: the description, requirements
// and benefits of one posting.
func (a *Adapter) Detail(ctx context.Context, token string, j *sources.RawJob) error {
	var resp detailResponse
	endpoint := fmt.Sprintf("%s/api/v2/accounts/%s/jobs/%s", a.baseURL, url.PathEscape(token), url.PathEscape(j.ExtID))
	if err := throttle.Do(ctx, func() error { return a.http.GetJSON(ctx, endpoint, &resp) }); err != nil {
		return err
	}
	if t := strings.TrimSpace(resp.Title); t != "" {
		j.Title = t
	}
	var html []string
	for _, part := range []string{resp.Description, resp.Requirements, resp.Benefits} {
		if strings.TrimSpace(part) != "" {
			html = append(html, part)
		}
	}
	j.DescriptionHTML = strings.Join(html, "\n")
	j.DescriptionText = normalize.HTMLToText(j.DescriptionHTML)
	if resp.Remote || strings.EqualFold(resp.Workplace, "remote") {
		remote := true
		j.IsRemote = &remote
	}
	return nil
}

// Fetch implements sources.Source (list plus every detail).
func (a *Adapter) Fetch(ctx context.Context, token string) ([]sources.RawJob, error) {
	return sources.FetchAll(ctx, a, token)
}
