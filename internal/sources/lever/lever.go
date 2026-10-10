// Package lever implements the Source interface for the Lever public
// postings API.
package lever

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"wera/internal/normalize"
	"wera/internal/sources"
)

const (
	defaultBaseURL = "https://api.lever.co"
	name           = "lever"
)

// Adapter fetches jobs from the Lever postings API.
type Adapter struct {
	http    *sources.HTTP
	baseURL string
}

// New returns a Lever adapter using the shared HTTP client.
func New(h *sources.HTTP) *Adapter {
	return &Adapter{http: h, baseURL: defaultBaseURL}
}

// NewWithBaseURL is New with a custom API base URL (used by tests).
func NewWithBaseURL(h *sources.HTTP, baseURL string) *Adapter {
	return &Adapter{http: h, baseURL: strings.TrimRight(baseURL, "/")}
}

// Name implements sources.Source.
func (a *Adapter) Name() string { return name }

type posting struct {
	ID          string `json:"id"`
	Text        string `json:"text"` // job title
	HostedURL   string `json:"hostedUrl"`
	CreatedAt   int64  `json:"createdAt"` // epoch milliseconds
	Description string `json:"description"`
	// DescriptionPlain and AdditionalPlain are plain text; Lists are
	// question/answer sections ("What you'll do" / HTML bullets).
	DescriptionPlain string `json:"descriptionPlain"`
	AdditionalPlain  string `json:"additionalPlain"`
	Lists            []struct {
		Text    string `json:"text"`    // section heading
		Content string `json:"content"` // HTML answer
	} `json:"lists"`
	WorkplaceType string `json:"workplaceType"` // "remote" | "hybrid" | "onsite"
	Categories    struct {
		Team     string `json:"team"`
		Location string `json:"location"`
	} `json:"categories"`
}

// Fetch implements sources.Source.
func (a *Adapter) Fetch(ctx context.Context, token string) ([]sources.RawJob, error) {
	jobs, _, err := a.FetchIfChanged(ctx, token, "")
	return jobs, err
}

// FetchIfChanged implements sources.ConditionalSource: with the ETag of the
// previous fetch, an unchanged board answers 304 and costs no download.
func (a *Adapter) FetchIfChanged(ctx context.Context, token, etag string) ([]sources.RawJob, string, error) {
	endpoint := fmt.Sprintf("%s/v0/postings/%s?mode=json", a.baseURL, url.PathEscape(token))
	var resp []posting
	newETag, err := a.http.GetJSONIfChanged(ctx, endpoint, etag, &resp)
	if err != nil {
		return nil, newETag, err
	}

	jobs := make([]sources.RawJob, 0, len(resp))
	for _, p := range resp {
		raw := sources.RawJob{
			ExtID:           p.ID,
			Title:           strings.TrimSpace(p.Text),
			LocationRaw:     strings.TrimSpace(p.Categories.Location),
			URL:             p.HostedURL,
			Department:      strings.TrimSpace(p.Categories.Team),
			DescriptionHTML: composeHTML(p),
			DescriptionText: composeText(p),
		}
		if t := ParseTimestamp(p.CreatedAt); t != nil {
			raw.PostedAt = t
		}
		if strings.EqualFold(p.WorkplaceType, "remote") {
			remote := true
			raw.IsRemote = &remote
		}
		if raw.IsRemote == nil && strings.Contains(strings.ToLower(raw.LocationRaw), "remote") {
			remote := true
			raw.IsRemote = &remote
		}
		jobs = append(jobs, raw)
	}
	return jobs, newETag, nil
}

// composeHTML assembles the full HTML description: body + list sections.
func composeHTML(p posting) string {
	var b strings.Builder
	b.WriteString(p.Description)
	for _, l := range p.Lists {
		if l.Text != "" {
			b.WriteString("<h3>" + l.Text + "</h3>")
		}
		b.WriteString(l.Content)
	}
	return b.String()
}

// composeText assembles the plain-text description, preserving paragraph
// breaks between sections.
func composeText(p posting) string {
	parts := make([]string, 0, 2+2*len(p.Lists))
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	add(p.DescriptionPlain)
	for _, l := range p.Lists {
		heading := strings.TrimSpace(l.Text)
		content := strings.TrimSpace(normalize.HTMLToText(l.Content))
		switch {
		case heading == "":
			add(content)
		case content == "":
			add(heading)
		default:
			add(heading + "\n" + content)
		}
	}
	add(p.AdditionalPlain)
	return strings.Join(parts, "\n\n")
}

// ParseTimestamp converts a Lever createdAt epoch-milliseconds value.
func ParseTimestamp(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}

// FormatTimestamp is ParseTimestamp's inverse, used by tests.
func FormatTimestamp(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }
