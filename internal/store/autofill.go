package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AutofillRun is one application the extension helped fill.
type AutofillRun struct {
	ID        int64     `json:"id"`
	JobID     *int64    `json:"job_id"`
	Host      string    `json:"host"`
	Pages     int       `json:"pages"`
	Filled    int       `json:"filled"`
	Suggested int       `json:"suggested"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StartAutofillRun records a new run. A job id the user does not have is
// dropped, so a run never points at someone else's job.
func StartAutofillRun(ctx context.Context, pool *pgxpool.Pool, userID int64, jobID *int64, host string, filled, suggested int) (*AutofillRun, error) {
	r := &AutofillRun{}
	err := pool.QueryRow(ctx, `
		INSERT INTO autofill_runs (user_id, job_id, host, filled, suggested)
		VALUES ($1, (SELECT job_id FROM user_jobs WHERE user_id = $1 AND job_id = $2), $3, $4, $5)
		RETURNING id, job_id, host, pages, filled, suggested, created_at, updated_at`,
		userID, jobID, host, filled, suggested).Scan(&r.ID, &r.JobID, &r.Host, &r.Pages, &r.Filled, &r.Suggested, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// AddAutofillPage counts one more page of a run and its fields
// (pgx.ErrNoRows if the run is not the user's).
func AddAutofillPage(ctx context.Context, pool *pgxpool.Pool, userID, id int64, filled, suggested int) (*AutofillRun, error) {
	r := &AutofillRun{}
	err := pool.QueryRow(ctx, `
		UPDATE autofill_runs SET pages = pages + 1, filled = filled + $3, suggested = suggested + $4, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, job_id, host, pages, filled, suggested, created_at, updated_at`,
		id, userID, filled, suggested).Scan(&r.ID, &r.JobID, &r.Host, &r.Pages, &r.Filled, &r.Suggested, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// ListAutofillRuns returns the user's runs, newest first: for one job
// when jobID is set, else the latest 50.
func ListAutofillRuns(ctx context.Context, pool *pgxpool.Pool, userID int64, jobID *int64) ([]AutofillRun, error) {
	var rows pgx.Rows
	var err error
	if jobID != nil {
		rows, err = pool.Query(ctx, `
			SELECT id, job_id, host, pages, filled, suggested, created_at, updated_at FROM autofill_runs
			WHERE user_id = $1 AND job_id = $2 ORDER BY created_at DESC LIMIT 50`, userID, *jobID)
	} else {
		rows, err = pool.Query(ctx, `
			SELECT id, job_id, host, pages, filled, suggested, created_at, updated_at FROM autofill_runs
			WHERE user_id = $1 ORDER BY created_at DESC LIMIT 50`, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutofillRun{}
	for rows.Next() {
		var r AutofillRun
		if err := rows.Scan(&r.ID, &r.JobID, &r.Host, &r.Pages, &r.Filled, &r.Suggested, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
