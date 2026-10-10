// Package amazon implements the Source interface for amazon.jobs, whose
// search JSON (used by the site itself) includes full descriptions. The
// token is the country code to search, e.g. "USA".
package amazon

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"wera/internal/normalize"
	"wera/internal/sources"
)

const (
	defaultBaseURL  = "https://www.amazon.jobs"
	name            = "amazon"
	pageSize        = 100
	maxListed       = 1000 // newest first
	pageConcurrency = 4
)

// Adapter fetches jobs from amazon.jobs.
type Adapter struct {
	http    *sources.HTTP
	baseURL string
}

// New returns an Amazon adapter using the shared HTTP client.
func New(h *sources.HTTP) *Adapter { return &Adapter{http: h, baseURL: defaultBaseURL} }

// NewWithBaseURL is New with a custom base URL (used by tests).
func NewWithBaseURL(h *sources.HTTP, baseURL string) *Adapter {
	return &Adapter{http: h, baseURL: strings.TrimRight(baseURL, "/")}
}

// Name implements sources.Source.
func (a *Adapter) Name() string { return name }

type job struct {
	IDIcims                 string `json:"id_icims"`
	Title                   string `json:"title"`
	Location                string `json:"location"`
	NormalizedLocation      string `json:"normalized_location"`
	JobCategory             string `json:"job_category"`
	JobPath                 string `json:"job_path"`
	PostedDate              string `json:"posted_date"`
	Description             string `json:"description"`
	BasicQualifications     string `json:"basic_qualifications"`
	PreferredQualifications string `json:"preferred_qualifications"`
}

type searchResponse struct {
	Hits int   `json:"hits"`
	Jobs []job `json:"jobs"`
}

// Fetch implements sources.Source: the newest postings in the country.
func (a *Adapter) Fetch(ctx context.Context, token string) ([]sources.RawJob, error) {
	page := func(ctx context.Context, offset int) (searchResponse, error) {
		var resp searchResponse
		endpoint := fmt.Sprintf("%s/en/search.json?offset=%d&result_limit=%d&sort=recent&country=%s",
			a.baseURL, offset, pageSize, url.QueryEscape(token))
		err := a.http.GetJSON(ctx, endpoint, &resp)
		return resp, err
	}
	first, err := page(ctx, 0)
	if err != nil {
		return nil, err
	}
	pages := []searchResponse{first}
	if limit := min(first.Hits, maxListed); limit > pageSize {
		rest := make([]searchResponse, (limit-1)/pageSize)
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(pageConcurrency)
		for i := range rest {
			g.Go(func() error {
				var err error
				rest[i], err = page(gctx, (i+1)*pageSize)
				return err
			})
		}
		// A missing page would close its jobs, so any failure fails the fetch.
		if err := g.Wait(); err != nil {
			return nil, err
		}
		pages = append(pages, rest...)
	}

	var out []sources.RawJob
	seen := map[string]bool{}
	for _, p := range pages {
		for _, j := range p.Jobs {
			if j.IDIcims == "" || seen[j.IDIcims] {
				continue
			}
			seen[j.IDIcims] = true
			out = append(out, toRaw(a.baseURL, j))
		}
	}
	return out, nil
}

func toRaw(base string, j job) sources.RawJob {
	var html strings.Builder
	html.WriteString("<p>" + j.Description + "</p>")
	if j.BasicQualifications != "" {
		html.WriteString("<h3>Basic qualifications</h3><p>" + j.BasicQualifications + "</p>")
	}
	if j.PreferredQualifications != "" {
		html.WriteString("<h3>Preferred qualifications</h3><p>" + j.PreferredQualifications + "</p>")
	}
	loc := j.NormalizedLocation
	if loc == "" {
		loc = j.Location
	}
	remote := strings.Contains(strings.ToLower(loc+" "+j.Title), "virtual") ||
		strings.Contains(strings.ToLower(loc), "remote")
	raw := sources.RawJob{
		ExtID:           j.IDIcims,
		Title:           strings.TrimSpace(j.Title),
		LocationRaw:     loc,
		Department:      j.JobCategory,
		URL:             base + j.JobPath,
		DescriptionHTML: html.String(),
		DescriptionText: normalize.HTMLToText(html.String()),
		IsRemote:        &remote,
	}
	if t, err := time.Parse("January 2, 2006", j.PostedDate); err == nil {
		raw.PostedAt = &t
	}
	return raw
}
