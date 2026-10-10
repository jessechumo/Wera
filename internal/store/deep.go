package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/scoring"
)

// DeepCandidate is one job queued for a deep review: the job plus the
// fit score of its latest score analysis.
type DeepCandidate struct {
	Job      scoring.Job
	FitScore int
}

// DeepJobs returns the user's top-N scored jobs whose score has fit_score
// >= minScore and that have no deep analysis for the current profile hash
// yet (PLAN.md section 8: deep review of the top jobs).
func DeepJobs(ctx context.Context, pool *pgxpool.Pool, userID int64, top, minScore int, profileHash string) ([]DeepCandidate, error) {
	rows, err := pool.Query(ctx, `
		SELECT j.id, c.name, j.title, COALESCE(j.location_raw, ''), j.url,
		       COALESCE(j.description, ''), a.fit_score
		FROM user_jobs uj
		JOIN jobs j ON j.id = uj.job_id
		JOIN companies c ON c.id = j.company_id
		JOIN analyses a ON a.id = uj.analysis_id
		WHERE uj.user_id = $4
		  AND uj.stage = 'scored'
		  AND j.closed_at IS NULL
		  AND a.fit_score >= $1
		  AND NOT EXISTS (
			SELECT 1 FROM analyses d
			WHERE d.job_id = j.id AND d.kind = 'deep' AND d.profile_hash = $2
		  )
		ORDER BY a.fit_score DESC, j.id
		LIMIT $3`, minScore, profileHash, top, userID)
	if err != nil {
		return nil, fmt.Errorf("load deep-review candidates: %w", err)
	}
	defer rows.Close()

	var out []DeepCandidate
	for rows.Next() {
		var dc DeepCandidate
		if err := rows.Scan(&dc.Job.ID, &dc.Job.Company, &dc.Job.Title,
			&dc.Job.Location, &dc.Job.URL, &dc.Job.Description, &dc.FitScore); err != nil {
			return nil, err
		}
		out = append(out, dc)
	}
	return out, rows.Err()
}

// SaveDeepAnalysis persists one deep-review result as analyses.kind =
// 'deep' (PLAN.md sections 6 and 8). The score-specific columns stay
// null except fit_score (copied from the score analysis so the row is
// sortable) and reason (a one-line summary). The job's stage is
// untouched: a deep review never changes pipeline state.
func SaveDeepAnalysis(ctx context.Context, pool *pgxpool.Pool, userID, jobID int64, model, profileHash string, fitScore int, summary, rawText string, usage scoring.UsageStats, costUSD float64, latencyMS int64) error {
	if rawText == "" {
		rawText = "null"
	}
	const q = `
		INSERT INTO analyses (job_id, kind, model, profile_hash,
		                      fit_score, reason, raw,
		                      prompt_tokens, cached_tokens, completion_tokens,
		                      cost_usd, latency_ms, user_id)
		VALUES ($1, 'deep', $2, $3, $4, $5, to_jsonb($6::text), $7, $8, $9, $10, $11, $12)
		ON CONFLICT (job_id, kind, profile_hash) DO UPDATE
		SET fit_score          = EXCLUDED.fit_score,
		    reason             = EXCLUDED.reason,
		    raw                = EXCLUDED.raw,
		    prompt_tokens      = EXCLUDED.prompt_tokens,
		    cached_tokens      = EXCLUDED.cached_tokens,
		    completion_tokens  = EXCLUDED.completion_tokens,
		    cost_usd           = EXCLUDED.cost_usd,
		    latency_ms         = EXCLUDED.latency_ms`
	if _, err := pool.Exec(ctx, q, jobID, model, profileHash, fitScore,
		nilIfEmpty(summary), rawText,
		usage.PromptTokens, usage.CachedTokens, usage.CompletionTokens,
		costUSD, latencyMS, userID); err != nil {
		return fmt.Errorf("save deep analysis for job %d: %w", jobID, err)
	}
	return nil
}

// DeepView is the deep analysis served by GET /api/jobs/{id}: model,
// accounting, and the model's raw JSON (which is itself the structured
// deep review: why_fit, resume_bullets, gaps, interview_topics).
type DeepView struct {
	Kind             string          `json:"kind"`
	Model            string          `json:"model"`
	CreatedAt        time.Time       `json:"created_at"`
	FitScore         *int            `json:"fit_score"`
	Reason           *string         `json:"reason"`
	PromptTokens     int             `json:"prompt_tokens"`
	CachedTokens     int             `json:"cached_tokens"`
	CompletionTokens int             `json:"completion_tokens"`
	CostUSD          float64         `json:"cost_usd"`
	LatencyMS        int64           `json:"latency_ms"`
	Raw              json.RawMessage `json:"raw"`
}

// GetDeepAnalysis returns the most recent deep analysis of a job for the
// given profile text, or nil when there is none (pgx.ErrNoRows is not an
// error here).
func GetDeepAnalysis(ctx context.Context, pool *pgxpool.Pool, jobID int64, profileHash string) (*DeepView, error) {
	var v DeepView
	err := pool.QueryRow(ctx, `
		SELECT kind, model, created_at, fit_score, reason,
		       prompt_tokens, cached_tokens, completion_tokens,
		       cost_usd, latency_ms, raw
		FROM analyses
		WHERE job_id = $1 AND kind = 'deep' AND profile_hash = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, jobID, profileHash).
		Scan(&v.Kind, &v.Model, &v.CreatedAt, &v.FitScore, &v.Reason,
			&v.PromptTokens, &v.CachedTokens, &v.CompletionTokens,
			&v.CostUSD, &v.LatencyMS, &v.Raw)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get deep analysis for job %d: %w", jobID, err)
	}
	// The raw column stores the model text as a JSON string (mirroring
	// the score rows, so unparseable replies survive). For deep rows we
	// serve the structured object itself when the string parses.
	var inner string
	if json.Unmarshal(v.Raw, &inner) == nil {
		if json.Valid([]byte(inner)) {
			v.Raw = json.RawMessage(inner)
		}
	}
	return &v, nil
}
