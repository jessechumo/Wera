package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TodayJobs returns the review queue for GET /api/today: scored, open jobs
// that have no application status yet (or are only saved), ordered by fit
// score descending. Jobs stay until they are applied to, dismissed, or close,
// regardless of when they were first seen.
func TodayJobs(ctx context.Context, pool *pgxpool.Pool, limit int) ([]JobView, error) {
	sqlText := jobViewSelect + `
	WHERE j.closed_at IS NULL
	  AND j.stage = 'scored'
	  AND (ap.status IS NULL OR ap.status = 'saved')
	ORDER BY a.fit_score DESC NULLS LAST, j.first_seen_at DESC
	LIMIT $1`
	rows, err := pool.Query(ctx, sqlText, normalizeLimit(limit, 100, 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobView
	for rows.Next() {
		var v JobView
		if err := rows.Scan(jobViewScan(&v)...); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ApplicationInput is the PUT /api/jobs/{id}/application body.
type ApplicationInput struct {
	Status string
	Notes  string
}

// ValidApplicationStatuses mirrors the applications.status constraint.
var ValidApplicationStatuses = map[string]bool{
	"saved": true, "applied": true, "interviewing": true,
	"offer": true, "rejected": true, "not_interested": true,
}

// PutApplication upserts the application row; applied_at is set the first
// time the status becomes 'applied' and never overwritten. An unknown
// job id is an error.
func PutApplication(ctx context.Context, pool *pgxpool.Pool, jobID int64, in ApplicationInput) error {
	tag, err := pool.Exec(ctx, `
		INSERT INTO applications (job_id, status, notes, applied_at, updated_at)
		VALUES ($1, $2, $3, CASE WHEN $2 = 'applied' THEN now() END, now())
		ON CONFLICT (job_id) DO UPDATE
		SET status     = EXCLUDED.status,
		    notes      = EXCLUDED.notes,
		    applied_at = CASE WHEN applications.status <> 'applied'
		                       AND EXCLUDED.status = 'applied'
		                      THEN now() ELSE applications.applied_at END,
		    updated_at = now()`,
		jobID, in.Status, in.Notes)
	if err != nil {
		return fmt.Errorf("put application for job %d: %w", jobID, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ExcludedJobs returns the audit view: excluded jobs with reason and
// evidence, optionally narrowed to one reason (GET /api/excluded).
func ExcludedJobs(ctx context.Context, pool *pgxpool.Pool, reason string, limit int) ([]JobView, error) {
	where := "j.stage = 'excluded' AND j.closed_at IS NULL"
	args := []any{}
	if reason != "" {
		args = append(args, reason)
		where += fmt.Sprintf(" AND j.exclude_reason = $%d", len(args))
	}
	args = append(args, normalizeLimit(limit, 50, 200))
	sqlText := jobViewSelect + "\nWHERE " + where +
		fmt.Sprintf("\nORDER BY j.first_seen_at DESC LIMIT $%d", len(args))
	rows, err := pool.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobView
	for rows.Next() {
		var v JobView
		if err := rows.Scan(jobViewScan(&v)...); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
