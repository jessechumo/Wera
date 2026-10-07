package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
	"wera/internal/metrics"
	"wera/internal/scoring"
	"wera/internal/store"
)

// ScoreStats summarizes one scoring pass.
type ScoreStats struct {
	Scored           int
	Excluded         int
	Failed           int // score_failed (invalid JSON twice)
	Skipped          int // left pending: budget guard or call error
	PromptTokens     int64
	CachedTokens     int64
	CompletionTokens int64
	CostUSD          float64
}

// ScorePending scores up to limit jobs in stage 'pending_score' (0 = all)
// and stores their analyses. It returns (nil, nil) when CORAL_API_KEY is
// not set, so the pipeline simply skips the LLM stage.
func ScorePending(ctx context.Context, pool *pgxpool.Pool, env *config.Env, log *slog.Logger, profilePath string, limit int, m *metrics.Registry) (*ScoreStats, error) {
	if env.CoralAPIKey == "" {
		log.Info("CORAL_API_KEY not set; skipping scoring stage")
		return nil, nil
	}
	profile, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w (copy profile/profile.example.md and fill it in)", profilePath, err)
	}
	if len(profile) == 0 {
		return nil, scoring.ErrNoProfile
	}
	roles, err := config.LoadRoles(config.DefaultRolesPath)
	if err != nil {
		return nil, err
	}
	jobs, err := store.PendingScoreJobs(ctx, pool, limit)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		log.Info("no pending jobs to score")
		return &ScoreStats{}, nil
	}

	profileHash := scoring.ProfileHash(profile)
	client := scoring.NewClient(scoring.ClientOptions{
		BaseURL: env.CoralBaseURL,
		APIKey:  env.CoralAPIKey,
		Model:   env.CoralModel,
		Log:     log,
	})
	scorer := &scoring.Scorer{
		Client:      client,
		Profile:     profile,
		ProfileHash: profileHash,
		Model:       env.CoralModel,
		MaxYears:    roles.Seniority.MaxYearsRequired,
		MaxCostUSD:  env.MaxCostPerRunUSD,
		Concurrency: env.ScoringConcurrency,
		Log:         log,
	}
	log.Info("scoring jobs", "jobs", len(jobs), "model", env.CoralModel,
		"concurrency", env.ScoringConcurrency, "max_cost_usd", env.MaxCostPerRunUSD)

	outcomes := scorer.Score(ctx, jobs)

	st := &ScoreStats{CostUSD: scorer.Spent()}
	for _, out := range outcomes {
		if m != nil {
			m.LLMTokens.WithLabelValues("prompt").Add(float64(out.Usage.PromptTokens))
			m.LLMTokens.WithLabelValues("cached").Add(float64(out.Usage.CachedTokens))
			m.LLMTokens.WithLabelValues("completion").Add(float64(out.Usage.CompletionTokens))
			m.LLMCost.Add(out.CostUSD)
			m.LLMLatency.Observe(float64(out.LatencyMS) / 1000)
			result := out.Stage
			if result == "" {
				result = "skipped"
			}
			m.LLMRequests.WithLabelValues(env.CoralModel, result).Inc()
		}
		if out.Stage != "" {
			if err := store.SaveAnalysis(ctx, pool, out.JobID, env.CoralModel, profileHash, out); err != nil {
				log.Error("saving analysis failed", "job_id", out.JobID, "err", err)
				continue
			}
			log.Info("job scored", "job_id", out.JobID, "model", env.CoralModel,
				"stage", out.Stage, "fit_score", scoreInt(out.Analysis),
				"tokens", out.Usage.PromptTokens, "cached", out.Usage.CachedTokens,
				"cost", fmt.Sprintf("%.6f", out.CostUSD), "ms", out.LatencyMS)
		}
		st.PromptTokens += out.Usage.PromptTokens
		st.CachedTokens += out.Usage.CachedTokens
		st.CompletionTokens += out.Usage.CompletionTokens
		switch out.Stage {
		case scoring.StageScored:
			st.Scored++
		case scoring.StageExcluded:
			st.Excluded++
		case scoring.StageFailed:
			st.Failed++
		default:
			st.Skipped++
			if out.Err != nil {
				log.Warn("job skipped after error", "job_id", out.JobID, "err", out.Err)
			}
		}
	}
	log.Info("scoring pass finished",
		"scored", st.Scored, "excluded", st.Excluded, "score_failed", st.Failed, "skipped", st.Skipped,
		"prompt_tokens", st.PromptTokens, "cached_tokens", st.CachedTokens,
		"completion_tokens", st.CompletionTokens, "cost_usd", fmt.Sprintf("%.6f", st.CostUSD))
	return st, nil
}

func scoreInt(a *scoring.Analysis) any {
	if a == nil {
		return nil
	}
	return a.FitScore
}
