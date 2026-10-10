package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"wera/internal/config"
	"wera/internal/store"
)

func (s *Server) putApplication(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, http.StatusBadRequest, "bad job id")
		return
	}
	var body struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if !store.ValidApplicationStatuses[body.Status] {
		s.writeError(w, http.StatusBadRequest,
			"status must be saved, applied, interviewing, offer, rejected or not_interested")
		return
	}
	user := currentUser(r)
	err = store.PutApplication(r.Context(), s.Pool, user.ID, id, store.ApplicationInput{
		Status: body.Status, Notes: body.Notes,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		s.Log.Error("put application failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	job, err := store.GetJob(r.Context(), s.Pool, user.ID, id)
	if err != nil {
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
		return
	}
	s.writeJSON(w, http.StatusOK, job)
}

func (s *Server) today(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	jobs, err := store.TodayJobs(r.Context(), s.Pool, currentUser(r).ID, limit)
	if err != nil {
		s.Log.Error("today failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if jobs == nil {
		jobs = []store.JobView{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "count": len(jobs)})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := store.Stats(r.Context(), s.Pool, currentUser(r).ID)
	if err != nil {
		s.Log.Error("stats failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, st)
}

func (s *Server) runs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := store.RecentRuns(r.Context(), s.Pool, limit)
	if err != nil {
		s.Log.Error("runs failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if list == nil {
		list = []store.RunView{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"runs": list})
}

func (s *Server) triggerRun(w http.ResponseWriter, r *http.Request) {
	if s.RunPipeline == nil {
		s.writeError(w, http.StatusNotImplemented, "pipeline trigger not configured")
		return
	}
	// Fast pre-check so the dashboard gets an immediate 409 while a run
	// (this process's or the worker's) is in flight; RunOnce re-checks
	// under its own lock so overlap is still impossible.
	got, release, err := store.TryAdvisoryLock(r.Context(), s.Pool, AdvisoryLockKey)
	if err != nil {
		s.Log.Error("lock check failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "lock check failed")
		return
	}
	if !got {
		s.writeError(w, http.StatusConflict, "a pipeline run is already in progress")
		return
	}
	unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	release(unlockCtx)
	cancel()

	go func() {
		if _, err := s.RunPipeline(context.Background()); err != nil {
			s.Log.Error("triggered pipeline run failed", "err", err)
		}
	}()
	s.writeJSON(w, http.StatusAccepted, map[string]string{"status": "triggered"})
}

func (s *Server) companies(w http.ResponseWriter, r *http.Request) {
	list, err := store.ListCompanies(r.Context(), s.Pool, currentUser(r).ID)
	if err != nil {
		s.Log.Error("companies failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if list == nil {
		list = []store.CompanyView{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"companies": list})
}

// usage is the admin report: overall token usage plus each user's spend
// this month.
func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	u, err := store.Usage(r.Context(), s.Pool)
	if err != nil {
		s.Log.Error("usage failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	users, err := store.SpendByUser(r.Context(), s.Pool)
	if err != nil {
		s.Log.Error("usage by user failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if users == nil {
		users = []store.UserSpend{}
	}
	s.writeJSON(w, http.StatusOK, struct {
		*store.UsageView
		Users []store.UserSpend `json:"users"`
	}{u, users})
}

// myUsage returns the current user's budget and spend this month.
func (s *Server) myUsage(w http.ResponseWriter, r *http.Request) {
	b, err := store.UserBudget(r.Context(), s.Pool, currentUser(r).ID)
	if err != nil {
		s.Log.Error("my usage failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]float64{
		"monthly_budget_usd": b.MonthlyUSD,
		"month_spend_usd":    b.SpentUSD,
		"remaining_usd":      b.Remaining(),
	})
}

func (s *Server) excluded(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	jobs, err := store.ExcludedJobs(r.Context(), s.Pool, currentUser(r).ID, q.Get("reason"), limit)
	if err != nil {
		s.Log.Error("excluded failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if jobs == nil {
		jobs = []store.JobView{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "count": len(jobs)})
}

// industryView is one row of GET /api/industries: the taxonomy entry
// plus its counts.
type industryView struct {
	config.Industry
	store.IndustryCounts
}

func (s *Server) industries(w http.ResponseWriter, r *http.Request) {
	counts, err := store.CountByIndustry(r.Context(), s.Pool, currentUser(r).ID)
	if err != nil {
		s.Log.Error("industries failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	list := make([]industryView, 0, len(s.Industries))
	for _, ind := range s.Industries {
		list = append(list, industryView{Industry: ind, IndustryCounts: counts[ind.ID]})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"industries": list})
}
