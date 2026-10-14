package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"wera/internal/store"
)

// sponsorSummary counts the whole result, so the page can show totals
// while it loads rows a page at a time.
type sponsorSummary struct {
	Companies      int `json:"companies"`
	Filers         int `json:"filers"`
	Sponsors       int `json:"sponsors"`
	Mixed          int `json:"mixed"`
	DoesNotSponsor int `json:"does_not_sponsor"`
}

func summarize(all []store.SponsorshipStat) sponsorSummary {
	s := sponsorSummary{Companies: len(all)}
	for _, c := range all {
		if c.H1BFilings != nil && *c.H1BFilings > 0 {
			s.Filers++
		}
		switch c.Signal {
		case "sponsors":
			s.Sponsors++
		case "mixed":
			s.Mixed++
		case "does_not_sponsor":
			s.DoesNotSponsor++
		}
	}
	return s
}

// sponsorship is GET /api/sponsorship?q=&filers=1&limit=&offset=: one
// page of companies (busiest H-1B filers first), the total that match,
// and a summary of the whole list. Every company is aggregated once per
// cache period and pages are sliced from that.
func (s *Server) sponsorship(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit = min(max(limit, 1), 200)
	if q.Get("limit") == "" {
		limit = 50
	}
	offset = max(offset, 0)
	query := strings.ToLower(strings.TrimSpace(q.Get("q")))
	if len(query) > 80 {
		query = query[:80]
	}
	// Posting signals change with each run; ten minutes is fresh enough.
	v, err := s.sponsorCache.get("all", func() (any, error) {
		return store.SponsorshipByCompany(context.WithoutCancel(r.Context()), s.Pool, "", 0)
	})
	if err != nil {
		s.Log.Error("sponsorship query failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	all := v.([]store.SponsorshipStat)
	summary := summarize(all)
	rows := make([]store.SponsorshipStat, 0, len(all))
	filersOnly := q.Get("filers") == "1"
	for _, c := range all {
		if query != "" && !strings.Contains(strings.ToLower(c.Company), query) {
			continue
		}
		if filersOnly && (c.H1BFilings == nil || *c.H1BFilings == 0) {
			continue
		}
		rows = append(rows, c)
	}
	total := len(rows)
	page := rows[min(offset, total):min(offset+limit, total)]
	period, err := store.H1BDataPeriod(r.Context(), s.Pool)
	if err != nil {
		s.Log.Error("h1b period failed", "err", err)
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	s.writeJSON(w, http.StatusOK, map[string]any{
		"companies": page, "total": total, "offset": offset, "summary": summary, "h1b_period": period,
	})
}
