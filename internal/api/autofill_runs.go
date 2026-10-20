package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"wera/internal/store"
)

type autofillCounts struct {
	JobID     int64  `json:"job_id"`
	Host      string `json:"host"`
	Filled    int    `json:"filled"`
	Suggested int    `json:"suggested"`
}

func (c *autofillCounts) valid() bool {
	return c.Filled >= 0 && c.Filled <= 500 && c.Suggested >= 0 && c.Suggested <= 500
}

// startAutofillRun is POST /api/ext/autofill-runs: the extension filled
// the first page of an application.
func (s *Server) startAutofillRun(w http.ResponseWriter, r *http.Request) {
	var in autofillCounts
	if err := decodeJSON(r, &in); err != nil || !in.valid() {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	in.Host = strings.ToLower(strings.TrimSpace(in.Host))
	if in.Host == "" || len(in.Host) > 255 || strings.ContainsAny(in.Host, "/ ") {
		s.writeError(w, http.StatusBadRequest, "host must be a site name")
		return
	}
	var job *int64
	if in.JobID > 0 {
		job = &in.JobID
	}
	run, err := store.StartAutofillRun(r.Context(), s.Pool, currentUser(r).ID, job, in.Host, in.Filled, in.Suggested)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	s.writeJSON(w, http.StatusCreated, run)
}

// addAutofillPage is POST /api/ext/autofill-runs/{id}/pages: one more page filled.
func (s *Server) addAutofillPage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id", "run")
	if !ok {
		return
	}
	var in autofillCounts
	if err := decodeJSON(r, &in); err != nil || !in.valid() {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	run, err := store.AddAutofillPage(r.Context(), s.Pool, currentUser(r).ID, id, in.Filled, in.Suggested)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "no such run")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	s.writeJSON(w, http.StatusOK, run)
}

// listAutofillRuns is GET /api/autofill-runs[?job_id=]: what the extension filled.
func (s *Server) listAutofillRuns(w http.ResponseWriter, r *http.Request) {
	var job *int64
	if v := r.URL.Query().Get("job_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			s.writeError(w, http.StatusBadRequest, "bad job id")
			return
		}
		job = &id
	}
	runs, err := store.ListAutofillRuns(r.Context(), s.Pool, currentUser(r).ID, job)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}
