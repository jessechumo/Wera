package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"wera/internal/store"
)

func (s *Server) interviewDomains(w http.ResponseWriter, r *http.Request) {
	d, err := store.InterviewDomains(r.Context(), s.Pool, currentUser(r).ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"domains": d})
}

// interviewQuiz is GET /api/interview/quiz?domain=&difficulty=&limit=.
func (s *Server) interviewQuiz(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	diff := q.Get("difficulty")
	if diff != "" && diff != "easy" && diff != "medium" && diff != "hard" {
		s.writeError(w, http.StatusBadRequest, "difficulty must be easy, medium or hard")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	qs, err := store.QuizQuestions(r.Context(), s.Pool, currentUser(r).ID, q.Get("domain"), diff, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"questions": qs})
}

// interviewAnswer is POST /api/interview/questions/{id}/answer {"choice": 0-3}.
func (s *Server) interviewAnswer(w http.ResponseWriter, r *http.Request) {
	id, ok := jobIDParam(r)
	if !ok {
		s.writeError(w, http.StatusBadRequest, "bad question id")
		return
	}
	var in struct {
		Choice int `json:"choice"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Choice < 0 || in.Choice > 3 {
		s.writeError(w, http.StatusBadRequest, "choice must be 0-3")
		return
	}
	a, err := store.AnswerQuestion(r.Context(), s.Pool, currentUser(r).ID, id, in.Choice)
	if errors.Is(err, pgx.ErrNoRows) {
		s.writeError(w, http.StatusNotFound, "question not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "could not record the answer")
		return
	}
	s.writeJSON(w, http.StatusOK, a)
}
