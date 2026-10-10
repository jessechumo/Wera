package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/scoring"
)

// FactsFor returns the shared facts of the given jobs that still describe
// the job's current content.
func FactsFor(ctx context.Context, pool *pgxpool.Pool, jobIDs []int64) (map[int64]*scoring.Facts, error) {
	out := map[int64]*scoring.Facts{}
	if len(jobIDs) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT f.job_id, f.facts FROM job_facts f JOIN jobs j ON j.id = f.job_id
		WHERE f.job_id = ANY($1::bigint[]) AND f.content_hash = j.content_hash`, jobIDs)
	if err != nil {
		return nil, fmt.Errorf("load job facts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var f scoring.Facts
		if json.Unmarshal(raw, &f) == nil {
			out[id] = &f
		}
	}
	return out, rows.Err()
}

// SaveFacts stores a job's facts for its current content.
func SaveFacts(ctx context.Context, pool *pgxpool.Pool, r scoring.FactsResult, model string) error {
	raw, err := json.Marshal(r.Facts)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO job_facts (job_id, content_hash, model, facts, prompt_tokens, cached_tokens, completion_tokens, cost_usd)
		SELECT $1, content_hash, $2, $3, $4, $5, $6, $7 FROM jobs WHERE id = $1
		ON CONFLICT (job_id) DO UPDATE
		SET content_hash = EXCLUDED.content_hash, model = EXCLUDED.model, facts = EXCLUDED.facts,
		    prompt_tokens = EXCLUDED.prompt_tokens, cached_tokens = EXCLUDED.cached_tokens,
		    completion_tokens = EXCLUDED.completion_tokens, cost_usd = EXCLUDED.cost_usd, created_at = now()`,
		r.JobID, model, raw, r.Usage.PromptTokens, r.Usage.CachedTokens, r.Usage.CompletionTokens, r.CostUSD)
	if err != nil {
		return fmt.Errorf("save facts for job %d: %w", r.JobID, err)
	}
	return nil
}

// ExcludeUserJob marks one of a user's jobs excluded without an analysis
// (a rule applied to the job's shared facts).
func ExcludeUserJob(ctx context.Context, pool *pgxpool.Pool, userID, jobID int64, reason, evidence string) error {
	_, err := pool.Exec(ctx, `
		UPDATE user_jobs SET stage = 'excluded', exclude_reason = $3, exclude_evidence = $4, updated_at = now()
		WHERE user_id = $1 AND job_id = $2`, userID, jobID, reason, nilIfEmpty(evidence))
	return err
}
