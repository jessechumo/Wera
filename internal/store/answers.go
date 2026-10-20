package store

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SavedAnswer is the user's answer to one application form question.
type SavedAnswer struct {
	ID        int64     `json:"id"`
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Kind      string    `json:"kind"` // text, long or choice
	Company   string    `json:"company"`
	Uses      int       `json:"uses"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AnswerInput is one answer to remember.
type AnswerInput struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Kind     string `json:"kind"`
	Company  string `json:"company"`
}

// Bounds on the answer bank.
const (
	MaxSavedAnswers  = 1000
	maxQuestionChars = 500
	maxAnswerChars   = 6000
)

var (
	keyPunct    = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	keyRequired = regexp.MustCompile(`\s*(\*|\(required\)|required)\s*$`)
)

// AnswerKey normalizes a question so the same question on another form
// (other case, punctuation, a "required" marker) finds the same answer.
func AnswerKey(q string) string {
	q = keyRequired.ReplaceAllString(strings.ToLower(strings.TrimSpace(q)), "")
	return strings.TrimSpace(keyPunct.ReplaceAllString(q, " "))
}

// Validate cleans an answer and checks its bounds.
func (a *AnswerInput) Validate() error {
	a.Question, a.Answer, a.Company = strings.TrimSpace(a.Question), strings.TrimSpace(a.Answer), strings.TrimSpace(a.Company)
	switch {
	case AnswerKey(a.Question) == "" || a.Answer == "":
		return errors.New("each answer needs a question and an answer")
	case utf8.RuneCountInString(a.Question) > maxQuestionChars:
		return errors.New("a question is too long")
	case utf8.RuneCountInString(a.Answer) > maxAnswerChars:
		return errors.New("an answer is too long")
	case utf8.RuneCountInString(a.Company) > 140:
		return errors.New("a company name is too long")
	}
	switch a.Kind {
	case "":
		a.Kind = "text"
	case "text", "long", "choice":
	default:
		return errors.New("kind must be text, long or choice")
	}
	return nil
}

// ListAnswers returns the user's saved answers, most recently used first.
func ListAnswers(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]SavedAnswer, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, question, answer, kind, company, uses, updated_at FROM application_answers
		WHERE user_id = $1 ORDER BY updated_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedAnswer{}
	for rows.Next() {
		var a SavedAnswer
		if err := rows.Scan(&a.ID, &a.Question, &a.Answer, &a.Kind, &a.Company, &a.Uses, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveAnswers remembers answers (validated by the caller). The same
// question replaces the earlier answer and counts one more use. The
// oldest answers go once the bank is full.
func SaveAnswers(ctx context.Context, pool *pgxpool.Pool, userID int64, in []AnswerInput) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, a := range in {
		if _, err := tx.Exec(ctx, `
			INSERT INTO application_answers (user_id, question, question_key, answer, kind, company)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (user_id, question_key) DO UPDATE
			SET question = EXCLUDED.question, answer = EXCLUDED.answer, kind = EXCLUDED.kind,
			    company = EXCLUDED.company, uses = application_answers.uses + 1, updated_at = now()`,
			userID, a.Question, AnswerKey(a.Question), a.Answer, a.Kind, a.Company); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM application_answers WHERE user_id = $1 AND id NOT IN (
			SELECT id FROM application_answers WHERE user_id = $1 ORDER BY updated_at DESC, id DESC LIMIT $2)`,
		userID, MaxSavedAnswers); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateAnswer changes one saved answer's text (pgx.ErrNoRows if not the user's).
func UpdateAnswer(ctx context.Context, pool *pgxpool.Pool, userID, id int64, answer string) error {
	tag, err := pool.Exec(ctx, `UPDATE application_answers SET answer = $3, updated_at = now() WHERE id = $1 AND user_id = $2`,
		id, userID, answer)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// DeleteAnswer forgets one saved answer (pgx.ErrNoRows if not the user's).
func DeleteAnswer(ctx context.Context, pool *pgxpool.Pool, userID, id int64) error {
	tag, err := pool.Exec(ctx, `DELETE FROM application_answers WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// DeleteAnswers forgets all of the user's saved answers.
func DeleteAnswers(ctx context.Context, pool *pgxpool.Pool, userID int64) error {
	_, err := pool.Exec(ctx, `DELETE FROM application_answers WHERE user_id = $1`, userID)
	return err
}
