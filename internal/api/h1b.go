package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"wera/internal/store"
)

// companyH1B is GET /api/sponsorship/{companyID}/h1b: a tracked
// company's certified H-1B applications (titles, wages, places).
func (s *Server) companyH1B(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "companyID"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad company id")
		return
	}
	keys, err := store.CompanyH1BKeys(r.Context(), s.Pool, id)
	if err == nil && len(keys) == 0 {
		s.writeError(w, http.StatusNotFound, "no H-1B filings matched to this company")
		return
	}
	s.writeH1BDetail(w, r, keys, err)
}

// h1bEmployer is GET /api/h1b/employer?key=: any filing employer's detail.
func (s *Server) h1bEmployer(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" || len(key) > 200 {
		s.writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	s.writeH1BDetail(w, r, []string{key}, nil)
}

func (s *Server) writeH1BDetail(w http.ResponseWriter, r *http.Request, keys []string, err error) {
	var d *store.H1BDetail
	if err == nil {
		var v any
		v, err = s.h1bCache.get("detail|"+strings.Join(keys, "|"), func() (any, error) {
			return store.H1BEmployerDetail(context.WithoutCancel(r.Context()), s.Pool, keys)
		})
		if err == nil {
			d = v.(*store.H1BDetail)
		}
	}
	if err != nil {
		s.Log.Error("h1b detail failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if d.Filings == 0 {
		s.writeError(w, http.StatusNotFound, "no H-1B filings for this employer")
		return
	}
	h1bCacheHeader(w)
	s.writeJSON(w, http.StatusOK, d)
}

// h1bCacheHeader lets the browser keep H-1B answers for a while: the data
// changes only when a new disclosure file is imported.
func h1bCacheHeader(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, max-age=1800")
}

// h1bEmployers is GET /api/h1b/employers?q=: search every filing
// employer by name, tracked by Wera or not.
func (s *Server) h1bEmployers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) > 80 {
		q = q[:80]
	}
	if len(q) < 2 {
		s.writeJSON(w, http.StatusOK, map[string]any{"employers": []store.H1BEmployer{}})
		return
	}
	v, err := s.h1bCache.get("search|"+strings.ToLower(q), func() (any, error) {
		return store.SearchH1BEmployers(context.WithoutCancel(r.Context()), s.Pool, q, 20)
	})
	if err != nil {
		s.Log.Error("h1b search failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	h1bCacheHeader(w)
	s.writeJSON(w, http.StatusOK, map[string]any{"employers": v})
}

// jobH1B is GET /api/jobs/{id}/h1b: the company's H-1B filings and what
// it paid for titles like this job's.
func (s *Server) jobH1B(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad job id")
		return
	}
	cid, title, err := store.JobCompanyTitle(r.Context(), s.Pool, currentUser(r).ID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "job not found")
		return
	}
	var out *store.JobH1B
	if err == nil {
		level := r.URL.Query().Get("level")
		switch level {
		case "I", "II", "III", "IV":
		default:
			level = ""
		}
		var v any
		v, err = s.h1bCache.get(fmt.Sprintf("job|%d|%s|%s", cid, title, level), func() (any, error) {
			return store.JobH1BFor(context.WithoutCancel(r.Context()), s.Pool, cid, title, level)
		})
		if err == nil {
			out = v.(*store.JobH1B)
		}
	}
	if err != nil {
		s.Log.Error("job h1b failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	h1bCacheHeader(w)
	s.writeJSON(w, http.StatusOK, out)
}
