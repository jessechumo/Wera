package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"wera/internal/resume"
	"wera/internal/store"
)

// resumeView is a stored resume plus, for tailored copies, how it covers
// the job's keywords.
type resumeView struct {
	*store.ResumeDoc
	Coverage *resume.Coverage `json:"coverage,omitempty"`
}

func (s *Server) renderer(w http.ResponseWriter) bool {
	if !s.Resumes.Available() {
		s.writeError(w, http.StatusServiceUnavailable, resume.ErrUnavailable.Error())
		return false
	}
	return true
}

func (s *Server) resumeParam(w http.ResponseWriter, r *http.Request) (*store.ResumeDoc, bool) {
	id, ok := jobIDParam(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "bad resume id")
		return nil, false
	}
	doc, err := store.GetResume(r.Context(), s.Pool, currentUser(r).ID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "resume not found")
		return nil, false
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return nil, false
	}
	return doc, true
}

// view adds keyword coverage for a tailored copy.
func (s *Server) view(r *http.Request, doc *store.ResumeDoc) resumeView {
	v := resumeView{ResumeDoc: doc}
	if doc.JobID != nil {
		if kws, err := store.JobKeywords(r.Context(), s.Pool, currentUser(r).ID, *doc.JobID); err == nil {
			c := resume.KeywordCoverage(doc.Data.PlainText(), kws)
			v.Coverage = &c
		}
	}
	return v
}

func (s *Server) listResumes(w http.ResponseWriter, r *http.Request) {
	list, err := store.ListResumes(r.Context(), s.Pool, currentUser(r).ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"resumes": list})
}

func (s *Server) getResumeHandler(w http.ResponseWriter, r *http.Request) {
	if doc, ok := s.resumeParam(w, r); ok {
		s.writeJSON(w, http.StatusOK, s.view(r, doc))
	}
}

// importResume is POST /api/resumes/import: create (or with replace,
// overwrite) the base resume from LaTeX, from the uploaded resume (AI),
// or as a starter.
func (s *Server) importResume(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source  string `json:"source"` // tex | profile | blank
		TeX     string `json:"tex"`
		Replace bool   `json:"replace"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	user := currentUser(r)
	var doc *resume.Resume
	var warnings []string
	switch in.Source {
	case "tex":
		if len(in.TeX) > 200_000 {
			s.writeError(w, http.StatusBadRequest, "that LaTeX file is too large")
			return
		}
		var err error
		if doc, warnings, err = resume.ImportTeX(in.TeX); err != nil {
			s.writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	case "profile":
		prof, err := store.GetProfile(r.Context(), s.Pool, user.ID)
		if err != nil || prof == nil || strings.TrimSpace(prof.ResumeText) == "" {
			s.writeError(w, http.StatusBadRequest, "upload your resume on the Profile page first")
			return
		}
		if !s.aiReady(w, r, "importing resumes") {
			return
		}
		reply, err := s.chatForUser(r.Context(), user.ID, "resume_import", 4000, resume.ImportMessages(prof.ResumeText))
		if err != nil {
			s.writeError(w, http.StatusBadGateway, "the AI service did not answer; try again")
			return
		}
		if doc, warnings, err = resume.ParseImport(reply, prof.ResumeText); err != nil {
			s.writeError(w, http.StatusBadGateway, "the AI could not read your resume; try LaTeX import or start from blank")
			return
		}
	case "blank":
		doc = starterResume(user)
	default:
		s.writeError(w, http.StatusBadRequest, "source must be tex, profile or blank")
		return
	}
	existing, err := store.BaseResume(r.Context(), s.Pool, user.ID)
	saved := &store.ResumeDoc{Title: "My resume", Data: *doc, Layout: resume.DefaultLayout, Notes: warnings}
	switch {
	case err == nil && !in.Replace:
		s.writeError(w, http.StatusConflict, "you already have a resume; import with replace to overwrite it")
		return
	case err == nil:
		saved.ID = existing.ID
	case !errors.Is(err, pgx.ErrNoRows):
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if s.Resumes.Available() {
		if res, err := s.Resumes.Fit(r.Context(), &saved.Data, nil); err == nil {
			saved.Layout, saved.Fit = res.Layout, res
		}
	}
	id, err := store.SaveResume(r.Context(), s.Pool, user.ID, saved)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "could not save the resume")
		return
	}
	doc2, _ := store.GetResume(r.Context(), s.Pool, user.ID, id)
	s.writeJSON(w, http.StatusCreated, s.view(r, doc2))
}

func starterResume(u *store.User) *resume.Resume {
	r := &resume.Resume{Name: u.Name, Email: u.Email, Sections: []resume.Section{
		{Kind: resume.KindEntries, Title: "Education", Entries: []resume.Entry{{Heading: "University", Subheading: "Degree", Dates: "2022 – 2026", Location: "City, ST"}}},
		{Kind: resume.KindSkills, Title: "Technical Skills", Skills: []resume.SkillGroup{{Name: "Languages", Items: "Python, Go, SQL"}}},
		{Kind: resume.KindEntries, Title: "Experience", Entries: []resume.Entry{{Heading: "Company", Subheading: "Job title", Dates: "2025 – Present", Location: "City, ST",
			Bullets: []resume.Bullet{{Text: "What you built or improved, how, and the result."}}}}},
		{Kind: resume.KindProjects, Title: "Projects", Entries: []resume.Entry{{Heading: "Project name", Tech: "Stack", Text: "What it does and why it matters."}}},
	}}
	if r.Name == "" {
		r.Name = strings.Split(u.Email, "@")[0]
	}
	_ = r.Normalize()
	return r
}

// putResume saves edits (the whole document) and an optional layout.
func (s *Server) putResume(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.resumeParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Title  *string        `json:"title"`
		Data   resume.Resume  `json:"data"`
		Layout *resume.Layout `json:"layout"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if err := in.Data.Normalize(); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	doc.Data = in.Data
	if in.Title != nil {
		doc.Title = *in.Title
	}
	if in.Layout != nil {
		doc.Layout = in.Layout.Clamp()
	}
	doc.Fit = nil // edits may change the length; fit again to know
	if _, err := store.SaveResume(r.Context(), s.Pool, currentUser(r).ID, doc); err != nil {
		s.writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	saved, _ := store.GetResume(r.Context(), s.Pool, currentUser(r).ID, doc.ID)
	s.writeJSON(w, http.StatusOK, s.view(r, saved))
}

func (s *Server) deleteResumeHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := jobIDParam(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "bad resume id")
		return
	}
	if err := store.DeleteResume(r.Context(), s.Pool, currentUser(r).ID, id); errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "resume not found")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fitResume is POST /api/resumes/{id}/fit: make it one page.
func (s *Server) fitResume(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.resumeParam(w, r)
	if !ok || !s.renderer(w) {
		return
	}
	prio := resume.DefaultPriority
	if doc.JobID != nil {
		if kws, err := store.JobKeywords(r.Context(), s.Pool, currentUser(r).ID, *doc.JobID); err == nil && len(kws) > 0 {
			prio = resume.KeywordPriority(kws)
		}
	}
	res, err := s.Resumes.Fit(r.Context(), &doc.Data, prio)
	if err != nil {
		s.Log.Error("fit failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not lay out the resume")
		return
	}
	doc.Layout, doc.Fit = res.Layout, res
	if _, err := store.SaveResume(r.Context(), s.Pool, currentUser(r).ID, doc); err != nil {
		s.writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	s.writeJSON(w, http.StatusOK, s.view(r, doc))
}

// previewResume is POST /api/resumes/preview: render unsaved edits as SVG
// pages (shown as images, which cannot run scripts) and measure them.
func (s *Server) previewResume(w http.ResponseWriter, r *http.Request) {
	if !s.renderer(w) {
		return
	}
	if !s.previewLimit.Allow(strconv.FormatInt(currentUser(r).ID, 10)) {
		s.writeError(w, http.StatusTooManyRequests, "too many previews; slow down a little")
		return
	}
	var in struct {
		Data   resume.Resume `json:"data"`
		Layout resume.Layout `json:"layout"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if err := in.Data.Normalize(); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	pages, err := s.Resumes.SVG(ctx, &in.Data, in.Layout)
	if err != nil {
		s.Log.Warn("preview failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not render the preview")
		return
	}
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = string(p)
	}
	m, _ := s.Resumes.Measure(ctx, &in.Data, in.Layout)
	s.writeJSON(w, http.StatusOK, map[string]any{"pages": out, "measure": m})
}

func resumeFilename(name, ext string) string {
	base := safeFilename(strings.ReplaceAll(strings.TrimSpace(name), " ", "_") + "_Resume." + ext)
	if base == "" || strings.HasPrefix(base, ".") {
		return "Resume." + ext
	}
	return base
}

// resumePDF is GET /api/resumes/{id}/pdf (?download=1).
func (s *Server) resumePDF(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.resumeParam(w, r)
	if !ok || !s.renderer(w) {
		return
	}
	pdf, err := s.Resumes.PDF(r.Context(), &doc.Data, doc.Layout)
	if err != nil {
		s.Log.Error("pdf failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not render the resume")
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, resumeFilename(doc.Data.Name, "pdf")))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; object-src 'self'; frame-ancestors 'self'")
	w.Write(pdf) //nolint:gosec // a PDF we rendered, served with its type and nosniff
}

// resumeTeX is GET /api/resumes/{id}/tex: the LaTeX source.
func (s *Server) resumeTeX(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.resumeParam(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/x-tex; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, resumeFilename(doc.Data.Name, "tex")))
	_, _ = w.Write([]byte(doc.Data.ExportTeX()))
}

// jobKeywords is GET /api/jobs/{id}/keywords: the posting's keywords and
// how the user's resume for it (tailored, else base) covers them.
func (s *Server) jobKeywords(w http.ResponseWriter, r *http.Request) {
	id, ok := jobIDParam(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "bad job id")
		return
	}
	user := currentUser(r)
	kws, err := store.JobKeywords(r.Context(), s.Pool, user.ID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "job not found")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	doc, err := store.JobResume(r.Context(), s.Pool, user.ID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		doc, err = store.BaseResume(r.Context(), s.Pool, user.ID)
	}
	out := map[string]any{"keywords": kws, "coverage": nil, "resume_id": nil}
	if err == nil {
		out["coverage"] = resume.KeywordCoverage(doc.Data.PlainText(), kws)
		out["resume_id"] = doc.ID
	}
	s.writeJSON(w, http.StatusOK, out)
}

// getJobResume is GET /api/jobs/{id}/resume: the copy tailored for a job.
func (s *Server) getJobResume(w http.ResponseWriter, r *http.Request) {
	id, ok := jobIDParam(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "bad job id")
		return
	}
	doc, err := store.JobResume(r.Context(), s.Pool, currentUser(r).ID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "no tailored resume for this job yet")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, s.view(r, doc))
}

// tailorJobResume is POST /api/jobs/{id}/resume: a copy of the base resume
// tailored to the job by the AI (edits keyed by bullet, checked), then
// fitted to one page keeping the bullets that match the job.
func (s *Server) tailorJobResume(w http.ResponseWriter, r *http.Request) {
	id, ok := jobIDParam(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "bad job id")
		return
	}
	user := currentUser(r)
	base, err := store.BaseResume(r.Context(), s.Pool, user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusBadRequest, "create your resume first (Resume page)")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	company, title, desc, err := s.jobContext(r, id, "", "", "")
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "job not found")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if !s.aiReady(w, r, "tailoring resumes") {
		return
	}
	kws, _ := store.JobKeywords(r.Context(), s.Pool, user.ID, id)
	before := resume.KeywordCoverage(base.Data.PlainText(), kws)
	reply, err := s.chatForUser(r.Context(), user.ID, "tailor", 2500, resume.TailorMessages(&base.Data, company, title, desc, before.Missing))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "the AI service did not answer; try again")
		return
	}
	tailored, notes, err := resume.ApplyTailoring(&base.Data, reply)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "the AI returned an unusable edit; try again")
		return
	}
	doc := &store.ResumeDoc{Title: fmt.Sprintf("%s, %s", title, company), JobID: &id, Data: *tailored, Layout: base.Layout, Notes: notes}
	if s.Resumes.Available() {
		if res, err := s.Resumes.Fit(r.Context(), &doc.Data, resume.KeywordPriority(kws)); err == nil {
			doc.Layout, doc.Fit = res.Layout, res
		}
	}
	rid, err := store.SaveResume(r.Context(), s.Pool, user.ID, doc)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "could not save the tailored resume")
		return
	}
	saved, _ := store.GetResume(r.Context(), s.Pool, user.ID, rid)
	v := s.view(r, saved)
	s.writeJSON(w, http.StatusOK, map[string]any{"resume": v, "coverage_before": before})
}
