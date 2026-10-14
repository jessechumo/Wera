package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"wera/internal/store"
)

// The admin API (/api/admin/*) serves the wera-admin app. Every route
// needs an admin's browser session (extension tokens are refused), and
// every change is written to the audit log.

// audit records an admin action; a failure is logged, never fatal.
func (s *Server) audit(r *http.Request, action, target string, detail any) {
	u := currentUser(r)
	if detail == nil {
		detail = map[string]any{}
	}
	if err := store.RecordAudit(r.Context(), s.Pool, u.ID, u.Email, action, target, detail); err != nil {
		s.Log.Error("audit failed", "action", action, "target", target, "err", err)
	}
	s.Log.Info("admin action", "admin_id", u.ID, "action", action, "target", target)
}

func userTarget(id int64) string { return "user:" + strconv.FormatInt(id, 10) }

// adminOverview is GET /api/admin/overview.
func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	o, err := store.AdminOverview(r.Context(), s.Pool)
	if err != nil {
		s.Log.Error("admin overview failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"overview": o, "monthly_cap_usd": s.monthlyCap(), "email_enabled": s.Notify.Enabled(),
		"run_trigger": s.RunPipeline != nil,
	})
}

func (s *Server) monthlyCap() float64 {
	if s.Env == nil {
		return 0
	}
	return s.Env.MaxMonthlyCostUSD
}

// adminUsers is GET /api/admin/users.
func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := store.AdminUsers(r.Context(), s.Pool)
	if err != nil {
		s.Log.Error("admin users failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// adminUser is GET /api/admin/users/{id}.
func (s *Server) adminUser(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id", "user")
	if !ok {
		return
	}
	u, err := store.AdminUserByID(r.Context(), s.Pool, id)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "no such user")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, u)
}

// updateUser is PATCH /api/admin/users/{id}: name, admin role, monthly
// AI budget. An admin cannot take away their own role, so there is always
// one admin left who can undo a change.
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id", "user")
	if !ok {
		return
	}
	var c store.UserChange
	if err := decodeJSON(r, &c); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	switch {
	case c.Name != nil && len(strings.TrimSpace(*c.Name)) > 80:
		s.writeError(w, http.StatusBadRequest, "the name is too long")
		return
	case c.BudgetUSD != nil && (math.IsNaN(*c.BudgetUSD) || *c.BudgetUSD < 0 || *c.BudgetUSD > 1000):
		s.writeError(w, http.StatusBadRequest, "the budget must be between $0 and $1000 a month")
		return
	case c.IsAdmin != nil && !*c.IsAdmin && id == currentUser(r).ID:
		s.writeError(w, http.StatusConflict, "you cannot remove your own admin role")
		return
	}
	if c.Name != nil {
		n := strings.TrimSpace(*c.Name)
		c.Name = &n
	}
	if err := store.UpdateUser(r.Context(), s.Pool, id, c); errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "no such user")
		return
	} else if err != nil {
		s.Log.Error("update user failed", "user_id", id, "err", err)
		s.writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	s.audit(r, "user.update", userTarget(id), c)
	s.adminUser(w, r)
}

// signOutUser is POST /api/admin/users/{id}/sign-out: ends every session
// and disconnects every extension of the account.
func (s *Server) signOutUser(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id", "user")
	if !ok {
		return
	}
	if id == currentUser(r).ID {
		s.writeError(w, http.StatusConflict, "sign yourself out from the menu instead")
		return
	}
	n, err := store.SignOutEverywhere(r.Context(), s.Pool, id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "sign out failed")
		return
	}
	s.audit(r, "user.sign_out", userTarget(id), map[string]int64{"ended": n})
	s.writeJSON(w, http.StatusOK, map[string]int64{"ended": n})
}

// deleteUser is DELETE /api/admin/users/{id}. The body repeats the
// account's email, so a wrong id cannot delete someone by accident.
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id", "user")
	if !ok {
		return
	}
	var in struct {
		ConfirmEmail string `json:"confirm_email"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if id == currentUser(r).ID {
		s.writeError(w, http.StatusConflict, "delete your own account from the dashboard's settings")
		return
	}
	u, err := store.AdminUserByID(r.Context(), s.Pool, id)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "no such user")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(in.ConfirmEmail), u.Email) {
		s.writeError(w, http.StatusBadRequest, "type the account's email to confirm")
		return
	}
	if err := store.DeleteUser(r.Context(), s.Pool, id); err != nil {
		s.Log.Error("admin delete user failed", "user_id", id, "err", err)
		s.writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.audit(r, "user.delete", userTarget(id), map[string]string{"email": u.Email})
	w.WriteHeader(http.StatusNoContent)
}

// adminTriggerRun is POST /api/admin/runs: start a pipeline run now.
func (s *Server) adminTriggerRun(w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w}
	s.triggerRun(rec, r)
	if rec.status == http.StatusAccepted {
		s.audit(r, "run.trigger", "", nil)
	}
}

// statusRecorder remembers the status a wrapped handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// adminContent is GET /api/admin/content?kind=&status=&limit=&offset=.
func (s *Server) adminContent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind, status := q.Get("kind"), q.Get("status")
	if kind != "" && kind != "post" && kind != "comment" {
		s.writeError(w, http.StatusBadRequest, "kind must be post or comment")
		return
	}
	if status != "" && !store.ContentStatuses[status] {
		s.writeError(w, http.StatusBadRequest, "status must be published or rejected")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, err := store.AdminContent(r.Context(), s.Pool, kind, status, limit, offset)
	if err != nil {
		s.Log.Error("admin content failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// contentParams reads {kind}/{id}; on failure it has written the response.
func (s *Server) contentParams(w http.ResponseWriter, r *http.Request) (string, int64, bool) {
	kind := chi.URLParam(r, "kind")
	if kind != "post" && kind != "comment" {
		s.writeError(w, http.StatusBadRequest, "kind must be post or comment")
		return "", 0, false
	}
	id, ok := s.pathID(w, r, "id", kind)
	return kind, id, ok
}

// setContentStatus is PUT /api/admin/content/{kind}/{id}/status: publish
// or hide a post or comment, overriding the AI moderation.
func (s *Server) setContentStatus(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := s.contentParams(w, r)
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &in); err != nil || !store.ContentStatuses[in.Status] {
		s.writeError(w, http.StatusBadRequest, "status must be published or rejected")
		return
	}
	if err := store.SetContentStatus(r.Context(), s.Pool, kind, id, in.Status); errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	s.audit(r, "content."+in.Status, kind+":"+strconv.FormatInt(id, 10), nil)
	w.WriteHeader(http.StatusNoContent)
}

// deleteContent is DELETE /api/admin/content/{kind}/{id}.
func (s *Server) deleteContent(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := s.contentParams(w, r)
	if !ok {
		return
	}
	if err := store.DeleteContent(r.Context(), s.Pool, kind, id); errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.audit(r, "content.delete", kind+":"+strconv.FormatInt(id, 10), nil)
	w.WriteHeader(http.StatusNoContent)
}

// adminAudit is GET /api/admin/audit?before=&limit=.
func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := store.AuditLog(r.Context(), s.Pool, max(before, 0), limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}
