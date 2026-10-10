package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/relevance"
)

// RankDocs returns the user's open jobs waiting to be scored as relevance
// documents (title plus the start of the description).
func RankDocs(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]relevance.Doc, error) {
	rows, err := pool.Query(ctx, `
		SELECT j.id, j.title, left(COALESCE(j.description, ''), 4000)
		FROM user_jobs uj JOIN jobs j ON j.id = uj.job_id
		WHERE uj.user_id = $1 AND uj.stage = 'pending_score' AND j.closed_at IS NULL`, userID)
	if err != nil {
		return nil, fmt.Errorf("load jobs to rank: %w", err)
	}
	defer rows.Close()
	var docs []relevance.Doc
	for rows.Next() {
		var d relevance.Doc
		if err := rows.Scan(&d.ID, &d.Title, &d.Body); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

// SetEstimates stores relevance estimates for a user's jobs.
func SetEstimates(ctx context.Context, pool *pgxpool.Pool, userID int64, scores map[int64]int) error {
	if len(scores) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for id, s := range scores {
		b.Queue(`UPDATE user_jobs SET estimated_score = $3 WHERE user_id = $1 AND job_id = $2`, userID, id, s)
	}
	if err := pool.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("save estimates: %w", err)
	}
	return nil
}

// PendingJobIDs lists the user's open jobs waiting to be scored.
func PendingJobIDs(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]int64, error) {
	rows, err := pool.Query(ctx, `
		SELECT uj.job_id FROM user_jobs uj JOIN jobs j ON j.id = uj.job_id
		WHERE uj.user_id = $1 AND uj.stage = 'pending_score' AND j.closed_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
