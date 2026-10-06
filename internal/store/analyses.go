package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/scoring"
)

// PendingScoreJobs loads up to limit jobs in stage 'pending_score' (oldest
// postings first), with the company name needed for the prompt. limit <= 0
// means no limit.
func PendingScoreJobs(ctx context.Context, pool *pgxpool.Pool, limit int) ([]scoring.Job, error) {
	q := `
		SELECT j.id, c.name, j.title, COALESCE(j.location_raw, ''), j.url,
		       COALESCE(j.description, '')
		FROM jobs j
		JOIN companies c ON c.id = j.company_id
		WHERE j.stage = 'pending_score' AND j.closed_at IS NULL
		ORDER BY j.posted_at ASC NULLS LAST, j.id`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("load pending jobs: %w", err)
	}
	defer rows.Close()

	var jobs []scoring.Job
	for rows.Next() {
		var j scoring.Job
		if err := rows.Scan(&j.ID, &j.Company, &j.Title, &j.Location, &j.URL, &j.Description); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// SaveAnalysis persists one scoring outcome: it upserts the analyses row
// (by job, kind, profile hash) and moves the job to its resulting stage.
// A score_failed outcome saves the raw model text in analyses.raw.
func SaveAnalysis(ctx context.Context, pool *pgxpool.Pool, jobID int64, model, profileHash string, out scoring.Outcome) error {
	a := out.Analysis
	rawText := out.Raw
	if rawText == "" {
		rawText = "null"
	}

	// raw is a JSONB column: store the model's (fence-stripped) text as a
	// JSON string so even unparseable replies are preserved.
	const q = `
		INSERT INTO analyses (job_id, kind, model, profile_hash,
		                      fit_score, verdict, seniority, years_required,
		                      sponsorship, sponsorship_quote, work_mode, us_eligible,
		                      location_summary, skills_matched, skills_missing, reason,
		                      raw, prompt_tokens, cached_tokens, completion_tokens,
		                      cost_usd, latency_ms)
		VALUES ($1, 'score', $2, $3,
		        $4, $5, $6, $7,
		        $8, $9, $10, $11,
		        $12, $13, $14, $15,
		        to_jsonb($16::text), $17, $18, $19,
		        $20, $21)
		ON CONFLICT (job_id, kind, profile_hash) DO UPDATE
		SET fit_score          = EXCLUDED.fit_score,
		    verdict            = EXCLUDED.verdict,
		    seniority          = EXCLUDED.seniority,
		    years_required     = EXCLUDED.years_required,
		    sponsorship        = EXCLUDED.sponsorship,
		    sponsorship_quote  = EXCLUDED.sponsorship_quote,
		    work_mode          = EXCLUDED.work_mode,
		    us_eligible        = EXCLUDED.us_eligible,
		    location_summary   = EXCLUDED.location_summary,
		    skills_matched     = EXCLUDED.skills_matched,
		    skills_missing     = EXCLUDED.skills_missing,
		    reason             = EXCLUDED.reason,
		    raw                = EXCLUDED.raw,
		    prompt_tokens      = EXCLUDED.prompt_tokens,
		    cached_tokens      = EXCLUDED.cached_tokens,
		    completion_tokens  = EXCLUDED.completion_tokens,
		    cost_usd           = EXCLUDED.cost_usd,
		    latency_ms         = EXCLUDED.latency_ms`
	if _, err := pool.Exec(ctx, q,
		jobID, model, profileHash,
		scoreOf(a),
		strOrNil(a, func(x *scoring.Analysis) string { return x.Verdict }),
		strOrNil(a, func(x *scoring.Analysis) string { return x.Seniority }),
		yearsOrNil(a),
		strOrNil(a, func(x *scoring.Analysis) string { return x.Sponsorship }),
		quoteOrNil(a),
		strOrNil(a, func(x *scoring.Analysis) string { return x.WorkMode }),
		boolOrNil(a),
		strOrNil(a, func(x *scoring.Analysis) string { return x.LocationSummary }),
		skillsOf(a, 0), skillsOf(a, 1),
		strOrNil(a, func(x *scoring.Analysis) string { return x.Reason }),
		rawText,
		out.Usage.PromptTokens, out.Usage.CachedTokens, out.Usage.CompletionTokens,
		out.CostUSD, out.LatencyMS,
	); err != nil {
		return fmt.Errorf("save analysis for job %d: %w", jobID, err)
	}

	// Move the job to its stage; skipped outcomes leave it pending_score.
	if out.Stage == "" {
		return nil
	}
	const uq = `
		UPDATE jobs
		SET stage = $2,
		    exclude_reason = $3,
		    exclude_evidence = $4
		WHERE id = $1`
	if _, err := pool.Exec(ctx, uq, jobID, out.Stage,
		nilIfEmpty(out.ExcludeReason), nilIfEmpty(out.ExcludeEvidence)); err != nil {
		return fmt.Errorf("set job %d stage %s: %w", jobID, out.Stage, err)
	}
	return nil
}

// --- helpers to safely project a possibly-nil analysis ---

func scoreOf(a *scoring.Analysis) any {
	if a == nil {
		return nil
	}
	return a.FitScore
}

func strOrNil(a *scoring.Analysis, get func(*scoring.Analysis) string) any {
	if a == nil {
		return nil
	}
	v := get(a)
	if v == "" {
		return nil
	}
	return v
}

func yearsOrNil(a *scoring.Analysis) any {
	if a == nil || a.YearsRequired == nil {
		return nil
	}
	return *a.YearsRequired
}

func quoteOrNil(a *scoring.Analysis) any {
	if a == nil || a.SponsorshipQuote == nil || *a.SponsorshipQuote == "" {
		return nil
	}
	return *a.SponsorshipQuote
}

func boolOrNil(a *scoring.Analysis) any {
	if a == nil || a.USEligible == nil {
		return nil
	}
	return *a.USEligible
}

// skillsOf returns a slice as a text[] parameter: 0 = matched, 1 = missing.
func skillsOf(a *scoring.Analysis, which int) any {
	if a == nil {
		return []string{}
	}
	s := a.SkillsMatched
	if which == 1 {
		s = a.SkillsMissing
	}
	if s == nil {
		return []string{}
	}
	return s
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
