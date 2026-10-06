package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunTotals is the summary written to a run row when it finishes.
type RunTotals struct {
	Status           string // ok | partial | failed
	CompaniesOK      int
	CompaniesFailed  int
	JobsSeen         int
	JobsNew          int
	JobsExcluded     int
	JobsScored       int
	PromptTokens     int64
	CachedTokens     int64
	CompletionTokens int64
	CostUSD          float64
	Error            string // non-empty only for fatal errors
}

// StartRun inserts a runs row and returns its id. The caller must finish
// it with FinishRun, even on error (deferred with a suitable status).
func StartRun(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `INSERT INTO runs DEFAULT VALUES RETURNING id`).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert run row: %w", err)
	}
	return id, nil
}

// FinishRun writes the summary and closes the run.
func FinishRun(ctx context.Context, pool *pgxpool.Pool, id int64, t RunTotals) error {
	_, err := pool.Exec(ctx, `
		UPDATE runs
		SET finished_at = now(),
		    status = $2,
		    companies_ok = $3,
		    companies_failed = $4,
		    jobs_seen = $5,
		    jobs_new = $6,
		    jobs_excluded = $7,
		    jobs_scored = $8,
		    prompt_tokens = $9,
		    cached_tokens = $10,
		    completion_tokens = $11,
		    cost_usd = $12,
		    error = NULLIF($13, '')
		WHERE id = $1`,
		id, t.Status, t.CompaniesOK, t.CompaniesFailed, t.JobsSeen, t.JobsNew,
		t.JobsExcluded, t.JobsScored, t.PromptTokens, t.CachedTokens,
		t.CompletionTokens, t.CostUSD, t.Error)
	if err != nil {
		return fmt.Errorf("finish run %d: %w", id, err)
	}
	return nil
}
