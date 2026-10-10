// Package workday implements a DetailSource for Workday career sites,
// using the same public JSON endpoints ("cxs") the career site itself
// calls. The token is "tenant.wdN/site", e.g. "nvidia.wd5/NVIDIAExternalCareerSite"
// for https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite.
package workday

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"wera/internal/normalize"
	"wera/internal/sources"
)

const (
	name     = "workday"
	pageSize = 20 // the most Workday returns per page
	// maxListed bounds how many postings are listed per company per run
	// (Workday sorts newest first; very large boards keep their newest).
	maxListed = 400
	// maxDetails bounds new postings detailed per company per run, so a
	// first fill spreads over a few runs.
	maxDetails = 60
)

// throttle is shared by every Workday site: they sit behind the same
// infrastructure, which starts answering 429/500 or a maintenance page
// when a client bursts across many tenants at once.
var throttle = sources.NewThrottle(4, 150*time.Millisecond)

// Adapter fetches jobs from Workday career sites.
type Adapter struct {
	http    *sources.HTTP
	baseURL string // tests only: replaces https://<tenant>.<wdN>.myworkdayjobs.com
}

// New returns a Workday adapter using the shared HTTP client.
func New(h *sources.HTTP) *Adapter { return &Adapter{http: h} }

// NewWithBaseURL is New with a fixed host (used by tests).
func NewWithBaseURL(h *sources.HTTP, baseURL string) *Adapter {
	return &Adapter{http: h, baseURL: strings.TrimRight(baseURL, "/")}
}

// Name implements sources.Source.
func (a *Adapter) Name() string { return name }

// MaxDetailsPerRun implements sources.DetailLimiter.
func (a *Adapter) MaxDetailsPerRun() int { return maxDetails }

// site is a parsed token.
type site struct {
	host, tenant, name string
}

func parseToken(token string) (site, error) {
	hostPart, siteName, ok := strings.Cut(token, "/")
	tenant, _, dotted := strings.Cut(hostPart, ".")
	if !ok || !dotted || tenant == "" || siteName == "" {
		return site{}, fmt.Errorf("workday token %q: want tenant.wdN/site", token)
	}
	return site{host: "https://" + hostPart + ".myworkdayjobs.com", tenant: tenant, name: siteName}, nil
}

// apiBase is the cxs endpoint root for a site.
func (a *Adapter) apiBase(s site) string {
	host := s.host
	if a.baseURL != "" {
		host = a.baseURL
	}
	return fmt.Sprintf("%s/wday/cxs/%s/%s", host, s.tenant, s.name)
}

type searchRequest struct {
	AppliedFacets map[string]any `json:"appliedFacets"`
	Limit         int            `json:"limit"`
	Offset        int            `json:"offset"`
	SearchText    string         `json:"searchText"`
}

type searchResponse struct {
	Total       int `json:"total"`
	JobPostings []struct {
		Title         string `json:"title"`
		ExternalPath  string `json:"externalPath"`
		LocationsText string `json:"locationsText"`
	} `json:"jobPostings"`
}

// pageConcurrency is how many result pages are requested at once.
const pageConcurrency = 4

// List implements sources.DetailSource: the newest postings, up to
// maxListed. The first page gives the total; the rest are fetched
// pageConcurrency at a time.
func (a *Adapter) List(ctx context.Context, token string) ([]sources.RawJob, error) {
	s, err := parseToken(token)
	if err != nil {
		return nil, err
	}
	page := func(ctx context.Context, offset int) (searchResponse, error) {
		var resp searchResponse
		req := searchRequest{AppliedFacets: map[string]any{}, Limit: pageSize, Offset: offset}
		err := throttle.Do(ctx, func() error {
			return a.http.PostJSON(ctx, a.apiBase(s)+"/jobs", req, &resp)
		})
		return resp, err
	}
	first, err := page(ctx, 0)
	if err != nil {
		return nil, err
	}
	pages := []searchResponse{first}
	if limit := min(first.Total, maxListed); limit > pageSize {
		rest := make([]searchResponse, (limit-1)/pageSize)
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(pageConcurrency)
		for i := range rest {
			g.Go(func() error {
				// A failed deep page drops only that page.
				rest[i], _ = page(gctx, (i+1)*pageSize)
				return nil
			})
		}
		_ = g.Wait()
		pages = append(pages, rest...)
	}

	var jobs []sources.RawJob
	seen := map[string]bool{}
	for _, resp := range pages {
		for _, p := range resp.JobPostings {
			if p.ExternalPath == "" || seen[p.ExternalPath] {
				continue
			}
			seen[p.ExternalPath] = true
			jobs = append(jobs, sources.RawJob{
				ExtID:       p.ExternalPath,
				Title:       strings.TrimSpace(p.Title),
				LocationRaw: p.LocationsText,
				URL:         s.host + "/" + s.name + p.ExternalPath,
			})
		}
	}
	return jobs, ctx.Err()
}

type detailResponse struct {
	JobPostingInfo struct {
		Title               string   `json:"title"`
		JobDescription      string   `json:"jobDescription"`
		Location            string   `json:"location"`
		AdditionalLocations []string `json:"additionalLocations"`
		StartDate           string   `json:"startDate"`
		ExternalURL         string   `json:"externalUrl"`
		Country             struct {
			Descriptor string `json:"descriptor"`
		} `json:"country"`
	} `json:"jobPostingInfo"`
}

// Detail implements sources.DetailSource.
func (a *Adapter) Detail(ctx context.Context, token string, j *sources.RawJob) error {
	s, err := parseToken(token)
	if err != nil {
		return err
	}
	var resp detailResponse
	if err := throttle.Do(ctx, func() error { return a.http.GetJSON(ctx, a.apiBase(s)+j.ExtID, &resp) }); err != nil {
		return err
	}
	info := resp.JobPostingInfo
	if info.Title != "" {
		j.Title = strings.TrimSpace(info.Title)
	}
	locs := append([]string{info.Location}, info.AdditionalLocations...)
	j.LocationRaw = strings.Trim(strings.Join(locs, "; "), "; ")
	if c := info.Country.Descriptor; c != "" && !strings.Contains(j.LocationRaw, c) {
		j.LocationRaw += " (" + c + ")"
	}
	if info.ExternalURL != "" {
		j.URL = info.ExternalURL
	}
	j.DescriptionHTML = info.JobDescription
	j.DescriptionText = normalize.HTMLToText(info.JobDescription)
	if t, err := time.Parse("2006-01-02", info.StartDate); err == nil {
		j.PostedAt = &t
	}
	remote := strings.Contains(strings.ToLower(j.LocationRaw), "remote")
	j.IsRemote = &remote
	return nil
}

// Fetch implements sources.Source (list plus every detail).
func (a *Adapter) Fetch(ctx context.Context, token string) ([]sources.RawJob, error) {
	return sources.FetchAll(ctx, a, token)
}
