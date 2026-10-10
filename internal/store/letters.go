package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CoverLetter is a user's letter for one job.
type CoverLetter struct {
	Body      string    `json:"body"`
	Model     string    `json:"model"`
	Edited    bool      `json:"edited"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetCoverLetter returns the user's letter for a job, or nil.
func GetCoverLetter(ctx context.Context, pool *pgxpool.Pool, userID, jobID int64) (*CoverLetter, error) {
	var c CoverLetter
	err := pool.QueryRow(ctx, `
		SELECT body, model, edited, created_at, updated_at FROM cover_letters
		WHERE user_id = $1 AND job_id = $2`, userID, jobID).
		Scan(&c.Body, &c.Model, &c.Edited, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveCoverLetter stores a generated (edited=false) or edited letter.
func SaveCoverLetter(ctx context.Context, pool *pgxpool.Pool, userID, jobID int64, body, model string, edited bool) (*CoverLetter, error) {
	if _, err := pool.Exec(ctx, `
		INSERT INTO cover_letters (user_id, job_id, body, model, edited) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, job_id) DO UPDATE
		SET body = EXCLUDED.body, model = CASE WHEN EXCLUDED.edited THEN cover_letters.model ELSE EXCLUDED.model END,
		    edited = EXCLUDED.edited, updated_at = now()`, userID, jobID, body, model, edited); err != nil {
		return nil, err
	}
	return GetCoverLetter(ctx, pool, userID, jobID)
}
