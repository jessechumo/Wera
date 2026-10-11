package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"wera/internal/auth"
	"wera/internal/pipeline"
	"wera/internal/profile"
	"wera/internal/store"
)

// extTokenPrefix marks extension tokens, so a leaked one is recognizable.
const extTokenPrefix = "wera_ext_" //nolint:gosec // a prefix, not a credential

// bearerHash returns the hash of an "Authorization: Bearer" extension
// token, or "".
func bearerHash(r *http.Request) string {
	v := r.Header.Get("Authorization")
	tok, ok := strings.CutPrefix(v, "Bearer ")
	if !ok || !strings.HasPrefix(tok, extTokenPrefix) {
		return ""
	}
	return auth.HashToken(tok)
}

// checkCredentials verifies an email and password with the login rate
// limits; on failure it has already written the response.
func (s *Server) checkCredentials(w http.ResponseWriter, r *http.Request, email, password string) *store.User {
	if !s.loginLimit.Allow(s.clientIP(r)) || !s.accountLimit.Allow(strings.ToLower(strings.TrimSpace(email))) {
		s.writeError(w, http.StatusTooManyRequests, "too many login attempts; try again in a few minutes")
		return nil
	}
	u, hash, err := store.UserCredentials(r.Context(), s.Pool, strings.TrimSpace(email))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.Log.Error("login lookup failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "login failed")
		return nil
	}
	if u == nil || hash == "" {
		auth.EqualizeTiming(password)
		s.writeError(w, http.StatusUnauthorized, "wrong email or password")
		return nil
	}
	if !auth.CheckPassword(hash, password) {
		s.writeError(w, http.StatusUnauthorized, "wrong email or password")
		return nil
	}
	return u
}

// createExtToken is POST /api/ext/tokens: the extension signs in with an
// email and password and receives a long-lived token (shown once).
func (s *Server) createExtToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	u := s.checkCredentials(w, r, in.Email, in.Password)
	if u == nil {
		return
	}
	token, _, err := auth.NewToken()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "could not create a token")
		return
	}
	token = extTokenPrefix + token
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 80 {
		name = "Chrome extension"
	}
	t, err := store.CreateAPIToken(r.Context(), s.Pool, u.ID, auth.HashToken(token), name)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "could not create a token")
		return
	}
	s.Log.Info("extension connected", "user_id", u.ID, "token_id", t.ID)
	s.writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user": u, "connection": t})
}

func (s *Server) listExtTokens(w http.ResponseWriter, r *http.Request) {
	ts, err := store.ListAPITokens(r.Context(), s.Pool, currentUser(r).ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"connections": ts})
}

// deleteExtToken is DELETE /api/ext/tokens/{id}, or /current for the
// token making the request (the extension's sign out).
func (s *Server) deleteExtToken(w http.ResponseWriter, r *http.Request) {
	if chi.URLParam(r, "id") == "current" {
		h := bearerHash(r)
		if h == "" {
			s.writeError(w, http.StatusBadRequest, "not an extension request")
			return
		}
		if err := store.DeleteAPITokenByHash(r.Context(), s.Pool, h); err != nil {
			s.writeError(w, http.StatusInternalServerError, "revoke failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	id, ok := jobIDParam(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := store.DeleteAPIToken(r.Context(), s.Pool, currentUser(r).ID, id); errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "no such connection")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getApplicant(w http.ResponseWriter, r *http.Request) {
	a, saved, err := store.GetApplicant(r.Context(), s.Pool, currentUser(r))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"applicant": a, "saved": saved})
}

func (s *Server) putApplicant(w http.ResponseWriter, r *http.Request) {
	var a store.Applicant
	if err := decodeJSON(r, &a); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if err := a.Validate(); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := store.SaveApplicant(r.Context(), s.Pool, currentUser(r).ID, &a); err != nil {
		s.writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	s.getApplicant(w, r)
}

// aiReady checks the server and the user's budget before an AI call; on
// failure it has written the response.
func (s *Server) aiReady(w http.ResponseWriter, r *http.Request, what string) bool {
	if s.Env == nil || s.Env.CoralAPIKey == "" {
		s.writeError(w, http.StatusServiceUnavailable, what+" is not configured on this server")
		return false
	}
	if !s.extAILimit.Allow(strconv.FormatInt(currentUser(r).ID, 10)) {
		s.writeError(w, http.StatusTooManyRequests, "too many AI requests this hour; try again later")
		return false
	}
	allowance, why, err := pipeline.Allowance(r.Context(), s.Pool, s.Env, currentUser(r).ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "budget check failed")
		return false
	}
	if allowance <= 0 {
		s.writeError(w, http.StatusPaymentRequired, what+" is unavailable: "+why)
		return false
	}
	return true
}

// suggestApplicant is POST /api/applicant/suggest: details read off the
// stored resume (links and phone verbatim), for the user to review.
func (s *Server) suggestApplicant(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	prof, err := store.GetProfile(r.Context(), s.Pool, user.ID)
	if err != nil || prof == nil || strings.TrimSpace(prof.ResumeText) == "" {
		s.writeError(w, http.StatusBadRequest, "upload a resume first")
		return
	}
	if !s.aiReady(w, r, "reading your resume") {
		return
	}
	reply, err := s.chatForUser(r.Context(), user.ID, "applicant", 600, profile.ApplicantMessages(prof.ResumeText))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "the AI service did not answer; try again")
		return
	}
	sug, err := profile.ParseApplicantSuggestions(reply, prof.ResumeText)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "the AI returned unusable details; try again")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"suggestions": sug})
}

func (s *Server) industryIDs() ([]string, map[string]bool) {
	ids := []string{}
	set := map[string]bool{}
	for _, in := range s.Industries {
		ids = append(ids, in.ID)
		set[in.ID] = true
	}
	if !set["other"] {
		ids, set["other"] = append(ids, "other"), true
	}
	sort.Strings(ids)
	return ids, set
}

// extractJob is POST /api/ext/extract: the AI reads a page's text into job
// fields when the page has no structured job data.
func (s *Server) extractJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL   string `json:"url"`
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if len(strings.TrimSpace(in.Text)) < 200 {
		s.writeError(w, http.StatusUnprocessableEntity, "this page has too little text to read a job from")
		return
	}
	if !s.aiReady(w, r, "reading job pages") {
		return
	}
	ids, set := s.industryIDs()
	reply, err := s.chatForUser(r.Context(), currentUser(r).ID, "extract", 500, profile.ExtractMessages(in.URL, in.Title, in.Text, ids))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "the AI service did not answer; try again")
		return
	}
	d, err := profile.ParseJobDraft(reply, in.Text, set)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "the AI could not read this page; fill the form yourself")
		return
	}
	s.writeJSON(w, http.StatusOK, d)
}

// lookupJob is GET /api/ext/jobs/lookup?url=: the user's job for a page.
func (s *Server) lookupJob(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := store.LookupJobByURL(r.Context(), s.Pool, user.ID, r.URL.Query().Get("url"))
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	job, err := store.GetJob(r.Context(), s.Pool, user.ID, id)
	if err != nil {
		s.writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

// addJob is POST /api/ext/jobs: save a job from any page to the user's
// lists (and tracker). Known URLs return the existing job (200).
func (s *Server) addJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL         string `json:"url"`
		Company     string `json:"company"`
		Title       string `json:"title"`
		Location    string `json:"location"`
		WorkMode    string `json:"work_mode"`
		Department  string `json:"department"`
		Description string `json:"description"`
		Industry    string `json:"industry"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	in.Company, in.Title, in.URL = strings.TrimSpace(in.Company), strings.TrimSpace(in.Title), strings.TrimSpace(in.URL)
	switch {
	case in.Company == "" || in.Title == "":
		s.writeError(w, http.StatusBadRequest, "company and title are required")
		return
	case !strings.HasPrefix(in.URL, "https://") && !strings.HasPrefix(in.URL, "http://"):
		s.writeError(w, http.StatusBadRequest, "the job needs its web address (http or https)")
		return
	case len(in.Company) > 140 || len(in.Title) > 200 || len(in.Location) > 200 || len(in.URL) > 2000 || len(in.Description) > 60000:
		s.writeError(w, http.StatusBadRequest, "a field is too long")
		return
	}
	if _, set := s.industryIDs(); !set[in.Industry] {
		in.Industry = "other"
	}
	var remote *bool
	if in.WorkMode == "remote" {
		t := true
		remote = &t
	}
	user := currentUser(r)
	id, created, err := store.AddManualJob(r.Context(), s.Pool, user.ID, store.ManualJob{
		URL: in.URL, Company: in.Company, Title: in.Title, Location: in.Location, Remote: remote,
		Department: in.Department, Description: in.Description, Industry: in.Industry,
	})
	if err != nil {
		s.Log.Error("add job failed", "user_id", user.ID, "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not add the job")
		return
	}
	if created && s.ScoreNow != nil {
		go func() { //nolint:gosec // scoring outlives the request on purpose
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := s.ScoreNow(ctx, user.ID, id); err != nil {
				s.Log.Warn("scoring an added job failed", "user_id", user.ID, "job_id", id, "err", err)
			}
		}()
	}
	job, err := store.GetJob(r.Context(), s.Pool, user.ID, id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	s.writeJSON(w, status, job)
}

// jobContext returns a job's company, title and description for prompts:
// the user's job when jobID is set, else the fields sent by the extension.
func (s *Server) jobContext(r *http.Request, jobID int64, company, title, desc string) (string, string, string, error) {
	if jobID <= 0 {
		return company, title, desc, nil
	}
	job, err := store.GetJob(r.Context(), s.Pool, currentUser(r).ID, jobID)
	if err != nil {
		return "", "", "", err
	}
	d, err := store.JobDescription(r.Context(), s.Pool, jobID)
	if err != nil {
		return "", "", "", err
	}
	return job.Company, job.Title, d, nil
}

// answerQuestion is POST /api/ext/answer: a draft answer to one
// application question, for the user to edit.
func (s *Server) answerQuestion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Question    string `json:"question"`
		MaxWords    int    `json:"max_words"`
		JobID       int64  `json:"job_id"`
		Company     string `json:"company"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if q := strings.TrimSpace(in.Question); len(q) < 5 || len(q) > 1000 {
		s.writeError(w, http.StatusBadRequest, "the question needs 5 to 1000 characters")
		return
	}
	user := currentUser(r)
	prof, err := store.GetProfile(r.Context(), s.Pool, user.ID)
	if err != nil || prof == nil || prof.Markdown == "" {
		s.writeError(w, http.StatusBadRequest, "finish your profile first")
		return
	}
	company, title, desc, err := s.jobContext(r, in.JobID, in.Company, in.Title, in.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "job not found")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if !s.aiReady(w, r, "drafting answers") {
		return
	}
	reply, err := s.chatForUser(r.Context(), user.ID, "answer", 700, profile.AnswerMessages(profile.AnswerInput{
		Question: in.Question, MaxWords: in.MaxWords, Profile: prof.Markdown, Resume: prof.ResumeText,
		Company: company, Title: title, Description: desc,
	}))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "the AI service did not answer; try again")
		return
	}
	s.writeJSON(w, http.StatusOK, profile.ParseAnswer(reply, in.MaxWords))
}
