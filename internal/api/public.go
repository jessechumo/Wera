package api

import (
	"context"
	"net/http"
)

// PublicStats are the headline numbers for the landing page.
type PublicStats struct {
	Companies    int64 `json:"companies"`
	Industries   int   `json:"industries"`
	OpenJobs     int64 `json:"open_jobs"`
	H1BFilings   int64 `json:"h1b_filings"`
	H1BEmployers int64 `json:"h1b_employers"`
}

// publicStats is GET /api/public/stats (no sign-in): what Wera watches.
func (s *Server) publicStats(w http.ResponseWriter, r *http.Request) {
	v, err := s.sponsorCache.get("public-stats", func() (any, error) {
		st := PublicStats{Industries: len(s.Industries)}
		ctx := context.WithoutCancel(r.Context())
		err := s.Pool.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM companies WHERE enabled),
			       (SELECT count(*) FROM jobs WHERE closed_at IS NULL AND source <> 'manual'),
			       (SELECT count(*) FROM h1b_lca),
			       (SELECT count(DISTINCT employer_key) FROM h1b_lca)`).
			Scan(&st.Companies, &st.OpenJobs, &st.H1BFilings, &st.H1BEmployers)
		return st, err
	})
	if err != nil {
		s.Log.Error("public stats failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=600")
	s.writeJSON(w, http.StatusOK, v)
}
