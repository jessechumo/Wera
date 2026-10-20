// Package rippling implements a DetailSource for Rippling job boards
// (ats.rippling.com/<board>), using the public endpoints the board itself
// calls. The token is the board slug, e.g. "riot-platforms-careers".
package rippling

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
	defaultBaseURL = "https://ats.rippling.com"
	name           = "rippling"
	pageSize       = 100
	// maxPages bounds a listing (10,000 postings).
	maxPages = 100
	// maxDetails bounds new postings detailed per company per run.
	maxDetails = 100
)

// throttle is shared by every Rippling board (one API host).
var throttle = sources.NewThrottle(3, 150*time.Millisecond)

// Adapter fetches jobs from Rippling job boards.
type Adapter struct {
	http    *sources.HTTP
	baseURL string
}

// New returns a Rippling adapter using the shared HTTP client.
func New(h *sources.HTTP) *Adapter { return &Adapter{http: h, baseURL: defaultBaseURL} }

// NewWithBaseURL is New with a custom base URL (used by tests).
func NewWithBaseURL(h *sources.HTTP, baseURL string) *Adapter {
	return &Adapter{http: h, baseURL: strings.TrimRight(baseURL, "/")}
}

// Name implements sources.Source.
func (a *Adapter) Name() string { return name }

// MaxDetailsPerRun implements sources.DetailLimiter.
func (a *Adapter) MaxDetailsPerRun() int { return maxDetails }

type listResponse struct {
	Items []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		URL        string `json:"url"`
		Department struct {
			Name string `json:"name"`
		} `json:"department"`
		Locations []struct {
			Name          string `json:"name"`
			Country       string `json:"country"`
			WorkplaceType string `json:"workplaceType"`
		} `json:"locations"`
	} `json:"items"`
	TotalPages int `json:"totalPages"`
}

// List implements sources.DetailSource: every posting, without
// descriptions. A posting open in several places is listed once per
// location; those entries become one job with every location.
func (a *Adapter) List(ctx context.Context, token string) ([]sources.RawJob, error) {
	var jobs []sources.RawJob
	index := map[string]int{}
	for page := range maxPages {
		var resp listResponse
		endpoint := fmt.Sprintf("%s/api/v2/board/%s/jobs?page=%d&pageSize=%d", a.baseURL, url.PathEscape(token), page, pageSize)
		if err := throttle.Do(ctx, func() error { return a.http.GetJSON(ctx, endpoint, &resp) }); err != nil {
			return nil, err
		}
		for _, it := range resp.Items {
			if it.ID == "" {
				continue
			}
			var locs []string
			remote := false
			for _, l := range it.Locations {
				loc := strings.TrimSpace(l.Name)
				if l.Country != "" && !strings.Contains(loc, l.Country) {
					loc = strings.TrimLeft(loc+" ("+l.Country+")", " ")
				}
				if loc != "" {
					locs = append(locs, loc)
				}
				remote = remote || strings.EqualFold(l.WorkplaceType, "REMOTE")
			}
			if i, ok := index[it.ID]; ok {
				j := &jobs[i]
				for _, loc := range locs {
					if !strings.Contains(j.LocationRaw, loc) {
						j.LocationRaw = strings.TrimPrefix(j.LocationRaw+"; "+loc, "; ")
					}
				}
				remote = remote || *j.IsRemote
				j.IsRemote = &remote
				continue
			}
			index[it.ID] = len(jobs)
			raw := sources.RawJob{
				ExtID:       it.ID,
				Title:       strings.TrimSpace(it.Name),
				LocationRaw: strings.Join(locs, "; "),
				URL:         it.URL,
				Department:  strings.TrimSpace(it.Department.Name),
				IsRemote:    &remote,
			}
			if raw.URL == "" {
				raw.URL = fmt.Sprintf("%s/%s/jobs/%s", defaultBaseURL, token, it.ID)
			}
			jobs = append(jobs, raw)
		}
		if page+1 >= resp.TotalPages {
			break
		}
	}
	return jobs, nil
}

type detailResponse struct {
	Name        string `json:"name"`
	Description struct {
		Company string `json:"company"`
		Role    string `json:"role"`
	} `json:"description"`
	CreatedOn string `json:"createdOn"`
}

// Detail implements sources.DetailSource: the role and company
// descriptions and the posting date.
func (a *Adapter) Detail(ctx context.Context, token string, j *sources.RawJob) error {
	var resp detailResponse
	endpoint := fmt.Sprintf("%s/api/v2/board/%s/jobs/%s", a.baseURL, url.PathEscape(token), url.PathEscape(j.ExtID))
	if err := throttle.Do(ctx, func() error { return a.http.GetJSON(ctx, endpoint, &resp) }); err != nil {
		return err
	}
	if t := strings.TrimSpace(resp.Name); t != "" {
		j.Title = t
	}
	var html []string
	for _, part := range []string{resp.Description.Role, resp.Description.Company} {
		if strings.TrimSpace(part) != "" {
			html = append(html, part)
		}
	}
	j.DescriptionHTML = strings.Join(html, "\n")
	j.DescriptionText = normalize.HTMLToText(j.DescriptionHTML)
	if t, err := time.Parse(time.RFC3339Nano, resp.CreatedOn); err == nil {
		j.PostedAt = &t
	}
	return nil
}

// Fetch implements sources.Source (list plus every detail).
func (a *Adapter) Fetch(ctx context.Context, token string) ([]sources.RawJob, error) {
	return sources.FetchAll(ctx, a, token)
}
