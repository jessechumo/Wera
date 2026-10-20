package api

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"wera/internal/store"
)

// listAnswers is GET /api/applicant/answers: answers remembered from forms.
func (s *Server) listAnswers(w http.ResponseWriter, r *http.Request) {
	list, err := store.ListAnswers(r.Context(), s.Pool, currentUser(r).ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"answers": list})
}

// saveAnswers is PUT /api/applicant/answers: remember answers the user
// gave on a form (the same question replaces its earlier answer).
func (s *Server) saveAnswers(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Answers []store.AnswerInput `json:"answers"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	if len(in.Answers) == 0 || len(in.Answers) > 80 {
		s.writeError(w, http.StatusBadRequest, "send 1 to 80 answers")
		return
	}
	for i := range in.Answers {
		if err := in.Answers[i].Validate(); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := store.SaveAnswers(r.Context(), s.Pool, currentUser(r).ID, in.Answers); err != nil {
		s.writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	s.listAnswers(w, r)
}

// updateAnswer is PATCH /api/applicant/answers/{id}: edit one saved answer.
func (s *Server) updateAnswer(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id", "answer")
	if !ok {
		return
	}
	var in struct {
		Answer string `json:"answer"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	a := store.AnswerInput{Question: "edit", Answer: in.Answer}
	if err := a.Validate(); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := store.UpdateAnswer(r.Context(), s.Pool, currentUser(r).ID, id, a.Answer); errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "no such answer")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "save failed")
		return
	}
	s.listAnswers(w, r)
}

// deleteAnswer is DELETE /api/applicant/answers/{id}: forget one answer.
func (s *Server) deleteAnswer(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "id", "answer")
	if !ok {
		return
	}
	if err := store.DeleteAnswer(r.Context(), s.Pool, currentUser(r).ID, id); errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "no such answer")
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteAllAnswers is DELETE /api/applicant/answers: forget every answer.
func (s *Server) deleteAllAnswers(w http.ResponseWriter, r *http.Request) {
	if err := store.DeleteAnswers(r.Context(), s.Pool, currentUser(r).ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
