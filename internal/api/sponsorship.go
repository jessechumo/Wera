package api

import (
	"net/http"
	"strconv"

	"wera/internal/store"
)

// sponsorship is GET /api/sponsorship?q=&limit=.
func (s *Server) sponsorship(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	query := q.Get("q")
	if len(query) > 80 {
		query = query[:80]
	}
	stats, err := store.SponsorshipByCompany(r.Context(), s.Pool, query, limit)
	if err != nil {
		s.Log.Error("sponsorship query failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"companies": stats})
}
