package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"wera/internal/profile"
	"wera/internal/store"
)

// selfID are application details the AI never sees: forms ask them for
// equal-opportunity reporting, and only the user's own choice fills them.
var selfID = map[string]bool{
	"gender": true, "pronouns": true, "transgender": true, "sexual_orientation": true,
	"race": true, "hispanic": true, "veteran": true, "disability": true,
}

// suggestFill is POST /api/ext/autofill: the AI answers the fields of one
// form page the extension could not fill from the details alone, from
// the profile, resume, details and earlier answers. Nothing is saved.
func (s *Server) suggestFill(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Fields      []profile.FormField `json:"fields"`
		JobID       int64               `json:"job_id"`
		Company     string              `json:"company"`
		Title       string              `json:"title"`
		Description string              `json:"description"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if len(in.Fields) == 0 || len(in.Fields) > profile.MaxAutofillFields {
		s.writeError(w, http.StatusBadRequest, "send 1 to 60 fields")
		return
	}
	for i := range in.Fields {
		f := &in.Fields[i]
		if f.ID == "" || len(f.ID) > 40 || len(f.Label) > 1000 || len(f.Options) > 120 {
			s.writeError(w, http.StatusBadRequest, "a field is malformed or too long")
			return
		}
		for _, o := range f.Options {
			if len(o) > 300 {
				s.writeError(w, http.StatusBadRequest, "an option is too long")
				return
			}
		}
	}
	if len(in.Description) > 60000 {
		in.Description = in.Description[:60000]
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
	if !s.aiReady(w, r, "suggesting answers") {
		return
	}
	a, _, err := store.GetApplicant(r.Context(), s.Pool, user)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	raw, _ := json.Marshal(a)
	var all map[string]string
	_ = json.Unmarshal(raw, &all)
	details := map[string]string{}
	for k, v := range all {
		if v != "" && !selfID[k] {
			details[k] = v
		}
	}
	saved, err := store.ListAnswers(r.Context(), s.Pool, user.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	prior := make([]profile.PriorAnswer, 0, len(saved))
	for _, sa := range saved {
		if sa.Kind != "long" {
			prior = append(prior, profile.PriorAnswer{Question: sa.Question, Answer: sa.Answer})
		}
	}
	reply, err := s.chatForUser(r.Context(), user.ID, "autofill", 2000, profile.AutofillMessages(profile.AutofillInput{
		Fields: in.Fields, Profile: prof.Markdown, Resume: prof.ResumeText, Details: details, Prior: prior,
		Company: company, Title: title, Description: desc,
	}))
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "the AI service did not answer; try again")
		return
	}
	out, err := profile.ParseAutofill(reply, in.Fields)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "the AI returned unusable answers; try again")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"answers": out})
}
