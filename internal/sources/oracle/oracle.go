// Package oracle implements a DetailSource for Oracle Recruiting Cloud
// career sites ("Candidate Experience"), using the public REST endpoints
// the site itself calls. The token is "host/site": the oraclecloud.com
// host without the domain and the site number, e.g. "jpmc.fa/CX_1001" for
// https://jpmc.fa.oraclecloud.com/hcmUI/CandidateExperience/en/sites/CX_1001.
package oracle

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"wera/internal/normalize"
	"wera/internal/sources"
)

const (
	name     = "oracle"
	pageSize = 200 // the most the endpoint returns per page
	// maxListed bounds how many postings are listed per company per run
	// (the site sorts newest first; very large boards keep their newest).
	maxListed = 2000
	// maxDetails bounds new postings detailed per company per run.
	maxDetails = 100
)

// throttle is shared by every Oracle site.
var throttle = sources.NewThrottle(4, 100*time.Millisecond)

// Adapter fetches jobs from Oracle Recruiting Cloud career sites.
type Adapter struct {
	http    *sources.HTTP
	baseURL string // tests only: replaces https://<host>.oraclecloud.com
}

// New returns an Oracle adapter using the shared HTTP client.
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
	host, number string
}

func parseToken(token string) (site, error) {
	host, number, ok := strings.Cut(token, "/")
	if !ok || host == "" || number == "" || strings.ContainsAny(host, "/:") {
		return site{}, fmt.Errorf("oracle token %q: want host/site, e.g. jpmc.fa/CX_1001", token)
	}
	return site{host: "https://" + host + ".oraclecloud.com", number: number}, nil
}

func (a *Adapter) api(s site) string {
	host := s.host
	if a.baseURL != "" {
		host = a.baseURL
	}
	return host + "/hcmRestApi/resources/latest/"
}

type listResponse struct {
	Items []struct {
		TotalJobsCount  int `json:"TotalJobsCount"`
		RequisitionList []struct {
			ID                 string `json:"Id"`
			Title              string `json:"Title"`
			PostedDate         string `json:"PostedDate"`
			PrimaryLocation    string `json:"PrimaryLocation"`
			WorkplaceTypeCode  string `json:"WorkplaceTypeCode"`
			WorkplaceType      string `json:"WorkplaceType"`
			SecondaryLocations []struct {
				Name string `json:"Name"`
			} `json:"secondaryLocations"`
		} `json:"requisitionList"`
	} `json:"items"`
}

// page lists one page of postings, newest first.
func (a *Adapter) page(ctx context.Context, s site, offset int) ([]sources.RawJob, int, error) {
	q := url.Values{}
	q.Set("onlyData", "true")
	q.Set("expand", "requisitionList.secondaryLocations")
	q.Set("finder", fmt.Sprintf("findReqs;siteNumber=%s,limit=%d,offset=%d,sortBy=POSTING_DATES_DESC", s.number, pageSize, offset))
	var resp listResponse
	if err := throttle.Do(ctx, func() error {
		return a.http.GetJSON(ctx, a.api(s)+"recruitingCEJobRequisitions?"+q.Encode(), &resp)
	}); err != nil {
		return nil, 0, err
	}
	if len(resp.Items) == 0 {
		return nil, 0, nil
	}
	var jobs []sources.RawJob
	for _, r := range resp.Items[0].RequisitionList {
		if r.ID == "" {
			continue
		}
		locs := []string{strings.TrimSpace(r.PrimaryLocation)}
		for _, l := range r.SecondaryLocations {
			if n := strings.TrimSpace(l.Name); n != "" && n != locs[0] {
				locs = append(locs, n)
			}
		}
		raw := sources.RawJob{
			ExtID:       r.ID,
			Title:       strings.TrimSpace(r.Title),
			LocationRaw: strings.Trim(strings.Join(locs, "; "), "; "),
			URL:         fmt.Sprintf("%s/hcmUI/CandidateExperience/en/sites/%s/job/%s", s.host, s.number, r.ID),
		}
		remote := r.WorkplaceTypeCode == "ORA_REMOTE" || strings.EqualFold(r.WorkplaceType, "remote") ||
			strings.Contains(strings.ToLower(raw.LocationRaw), "remote")
		raw.IsRemote = &remote
		if t, err := time.Parse("2006-01-02", r.PostedDate); err == nil {
			raw.PostedAt = &t
		}
		jobs = append(jobs, raw)
	}
	return jobs, resp.Items[0].TotalJobsCount, nil
}

// List implements sources.DetailSource: the newest postings, up to
// maxListed, without descriptions.
func (a *Adapter) List(ctx context.Context, token string) ([]sources.RawJob, error) {
	jobs, _, err := a.ListNew(ctx, token, nil)
	return jobs, err
}

// ListNew implements sources.IncrementalLister: pages newest first and,
// when known is set, stops after the first page with no unknown posting.
func (a *Adapter) ListNew(ctx context.Context, token string, known map[string]bool) ([]sources.RawJob, bool, error) {
	s, err := parseToken(token)
	if err != nil {
		return nil, false, err
	}
	var jobs []sources.RawJob
	seen := map[string]bool{}
	for offset := 0; offset < maxListed; offset += pageSize {
		page, total, err := a.page(ctx, s, offset)
		if err != nil {
			if len(jobs) > 0 && !errors.Is(err, sources.ErrUnavailable) {
				return jobs, false, nil // keep what was listed
			}
			return nil, false, err
		}
		unknown := 0
		for _, j := range page {
			if seen[j.ExtID] {
				continue
			}
			seen[j.ExtID] = true
			if !known[j.ExtID] {
				unknown++
			}
			jobs = append(jobs, j)
		}
		if len(page) < pageSize || offset+pageSize >= total {
			return jobs, true, nil // reached the end of the board
		}
		if known != nil && unknown == 0 {
			return jobs, false, nil
		}
	}
	return jobs, known == nil, nil
}

type detailResponse struct {
	Items []struct {
		Title                       string `json:"Title"`
		ExternalDescriptionStr      string `json:"ExternalDescriptionStr"`
		ExternalResponsibilitiesStr string `json:"ExternalResponsibilitiesStr"`
		ExternalQualificationsStr   string `json:"ExternalQualificationsStr"`
		ExternalPostedStartDate     string `json:"ExternalPostedStartDate"`
		WorkplaceTypeCode           string `json:"WorkplaceTypeCode"`
	} `json:"items"`
}

// Detail implements sources.DetailSource: the description,
// responsibilities and qualifications of one posting.
func (a *Adapter) Detail(ctx context.Context, token string, j *sources.RawJob) error {
	s, err := parseToken(token)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("onlyData", "true")
	q.Set("expand", "all")
	q.Set("finder", fmt.Sprintf("ById;Id=%q,siteNumber=%s", j.ExtID, s.number))
	var resp detailResponse
	if err := throttle.Do(ctx, func() error {
		return a.http.GetJSON(ctx, a.api(s)+"recruitingCEJobRequisitionDetails?"+q.Encode(), &resp)
	}); err != nil {
		return err
	}
	if len(resp.Items) == 0 {
		return fmt.Errorf("%w: oracle posting %s", sources.ErrBoardNotFound, j.ExtID)
	}
	d := resp.Items[0]
	if t := strings.TrimSpace(d.Title); t != "" {
		j.Title = t
	}
	var html []string
	for _, part := range []string{d.ExternalDescriptionStr, d.ExternalResponsibilitiesStr, d.ExternalQualificationsStr} {
		if strings.TrimSpace(part) != "" {
			html = append(html, part)
		}
	}
	j.DescriptionHTML = strings.Join(html, "\n")
	j.DescriptionText = normalize.HTMLToText(j.DescriptionHTML)
	if t, err := time.Parse(time.RFC3339, d.ExternalPostedStartDate); err == nil {
		j.PostedAt = &t
	}
	if d.WorkplaceTypeCode == "ORA_REMOTE" {
		remote := true
		j.IsRemote = &remote
	}
	return nil
}

// Fetch implements sources.Source (list plus every detail).
func (a *Adapter) Fetch(ctx context.Context, token string) ([]sources.RawJob, error) {
	return sources.FetchAll(ctx, a, token)
}
