package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Question is a multiple-choice interview question as served to a user:
// the answer and explanation are revealed only after they choose.
type Question struct {
	ID         int64    `json:"id"`
	Domain     string   `json:"domain"`
	Kind       string   `json:"kind"`
	Difficulty string   `json:"difficulty"`
	Question   string   `json:"question"`
	Choices    []string `json:"choices"`
}

// Answer is the result of answering one question.
type Answer struct {
	Correct     bool   `json:"correct"`
	Answer      int    `json:"answer"`
	Explanation string `json:"explanation"`
}

// DomainStat counts questions and the user's progress in one domain.
type DomainStat struct {
	Domain    string `json:"domain"`
	Kind      string `json:"kind"`
	Questions int64  `json:"questions"`
	Answered  int64  `json:"answered"`
	Correct   int64  `json:"correct"`
}

// InterviewDomains lists domains with question counts and the user's
// answered/correct counts (latest attempt per question).
func InterviewDomains(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]DomainStat, error) {
	rows, err := pool.Query(ctx, `
		WITH last AS (
		  SELECT DISTINCT ON (question_id) question_id, correct
		  FROM interview_attempts WHERE user_id = $1
		  ORDER BY question_id, created_at DESC)
		SELECT q.domain, min(q.kind), count(*), count(l.question_id), count(*) FILTER (WHERE l.correct)
		FROM interview_questions q LEFT JOIN last l ON l.question_id = q.id
		GROUP BY q.domain ORDER BY min(q.kind) DESC, q.domain`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DomainStat{}
	for rows.Next() {
		var d DomainStat
		if err := rows.Scan(&d.Domain, &d.Kind, &d.Questions, &d.Answered, &d.Correct); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// QuizQuestions picks up to limit questions, unanswered (or last answered
// wrong) first, then random. Empty domain or difficulty means any.
func QuizQuestions(ctx context.Context, pool *pgxpool.Pool, userID int64, domain, difficulty string, limit int) ([]Question, error) {
	rows, err := pool.Query(ctx, `
		WITH last AS (
		  SELECT DISTINCT ON (question_id) question_id, correct
		  FROM interview_attempts WHERE user_id = $1
		  ORDER BY question_id, created_at DESC)
		SELECT q.id, q.domain, q.kind, q.difficulty, q.question, q.choices
		FROM interview_questions q LEFT JOIN last l ON l.question_id = q.id
		WHERE ($2 = '' OR q.domain = $2) AND ($3 = '' OR q.difficulty = $3)
		ORDER BY CASE WHEN l.question_id IS NULL THEN 0 WHEN NOT l.correct THEN 1 ELSE 2 END, random()
		LIMIT $4`, userID, domain, difficulty, normalizeLimit(limit, 10, 25))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Question{}
	for rows.Next() {
		var q Question
		if err := rows.Scan(&q.ID, &q.Domain, &q.Kind, &q.Difficulty, &q.Question, &q.Choices); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// AnswerQuestion records the user's choice and reveals the answer.
func AnswerQuestion(ctx context.Context, pool *pgxpool.Pool, userID, questionID int64, choice int) (*Answer, error) {
	if choice < 0 || choice > 3 {
		return nil, fmt.Errorf("choice must be 0-3")
	}
	var a Answer
	err := pool.QueryRow(ctx, `
		WITH q AS (SELECT id, answer, explanation FROM interview_questions WHERE id = $2),
		ins AS (
		  INSERT INTO interview_attempts (user_id, question_id, choice, correct)
		  SELECT $1, q.id, $3, q.answer = $3 FROM q)
		SELECT answer = $3, answer, explanation FROM q`, userID, questionID, choice).Scan(&a.Correct, &a.Answer, &a.Explanation)
	if err != nil {
		return nil, err
	}
	return &a, nil
}
