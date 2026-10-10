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
func TodayJobs(ctx context.Context, pool *pgxpool.Pool, userID int64, limit int) ([]JobView, error) {
	sqlText := jobViewSelect + `
	  AND j.closed_at IS NULL
	  AND uj.stage = 'scored'
	  AND (ap.status IS NULL OR ap.status = 'saved')
	ORDER BY a.fit_score DESC NULLS LAST, j.first_seen_at DESC
	LIMIT $2`
	rows, err := pool.Query(ctx, sqlText, userID, normalizeLimit(limit, 100, 200))
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

// PutApplication upserts the user's application row; applied_at is set
// the first time the status becomes 'applied' and never overwritten. A
// job the user has no match row for is pgx.ErrNoRows.
func PutApplication(ctx context.Context, pool *pgxpool.Pool, userID, jobID int64, in ApplicationInput) error {
	tag, err := pool.Exec(ctx, `
		INSERT INTO applications (user_id, job_id, status, notes, applied_at, updated_at)
		SELECT $4, $1, $2, $3, CASE WHEN $2 = 'applied' THEN now() END, now()
		WHERE EXISTS (SELECT 1 FROM user_jobs WHERE user_id = $4 AND job_id = $1)
		ON CONFLICT (user_id, job_id) DO UPDATE
		SET status     = EXCLUDED.status,
		    notes      = EXCLUDED.notes,
		    applied_at = CASE WHEN applications.status <> 'applied'
		                       AND EXCLUDED.status = 'applied'
		                      THEN now() ELSE applications.applied_at END,
		    updated_at = now()`,
		jobID, in.Status, in.Notes, userID)
	if err != nil {
		return fmt.Errorf("put application for job %d: %w", jobID, err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ExcludedJobs returns the user's audit view: excluded jobs with reason
// and evidence, optionally narrowed to one reason (GET /api/excluded).
func ExcludedJobs(ctx context.Context, pool *pgxpool.Pool, userID int64, reason string, limit int) ([]JobView, error) {
	where := "uj.stage = 'excluded' AND j.closed_at IS NULL"
	args := []any{userID}
	if reason != "" {
		args = append(args, reason)
		where += fmt.Sprintf(" AND uj.exclude_reason = $%d", len(args))
	}
	args = append(args, normalizeLimit(limit, 50, 200))
	sqlText := jobViewSelect + "\nAND " + where +
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
