package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
	"wera/internal/filter"
	"wera/internal/metrics"
	"wera/internal/scoring"
	"wera/internal/store"
)

// FilterStats summarizes one user's rule-filter pass.
type FilterStats struct {
	Excluded int
	Pending  int
}

// FilterUser applies the rule engine with the user's preferences to every
// open job they have no current outcome for.
func FilterUser(ctx context.Context, pool *pgxpool.Pool, eng *filter.Engine, prof *store.Profile, m *metrics.Registry) (*FilterStats, error) {
	excluded, pending, byReason, err := store.FilterForUser(ctx, pool, eng, prof.UserID, &prof.Preferences)
	if err != nil {
		return nil, fmt.Errorf("filter for user %d: %w", prof.UserID, err)
	}
	if m != nil {
		for reason, n := range byReason {
			m.JobsExcluded.WithLabelValues(reason).Add(float64(n))
		}
	}
	return &FilterStats{Excluded: excluded, Pending: pending}, nil
}

// ExclusionsFor maps a user's preferences to their post-LLM rules.
func ExclusionsFor(p *config.Preferences) scoring.Exclusions {
	return scoring.Exclusions{
		MaxYears:           p.MaxYearsRequired,
		RequireSponsorship: p.NeedsSponsorship,
		USOnly:             p.USOnly,
		AllowSenior:        p.AcceptsLevel("senior") || p.AcceptsLevel("management"),
	}
}

// ScoreStats summarizes one scoring pass.
type ScoreStats struct {
	Scored           int
	Excluded         int
	Failed           int // score_failed (invalid JSON twice)
	Skipped          int // left pending: budget guard or call error
	Reused           int // linked to an existing analysis, no LLM call
	PromptTokens     int64
	CachedTokens     int64
	CompletionTokens int64
	CostUSD          float64
}

// Add accumulates another pass's numbers.
func (s *ScoreStats) Add(o *ScoreStats) {
	if o == nil {
		return
	}
	s.Scored += o.Scored
	s.Excluded += o.Excluded
	s.Failed += o.Failed
	s.Skipped += o.Skipped
	s.Reused += o.Reused
	s.PromptTokens += o.PromptTokens
	s.CachedTokens += o.CachedTokens
	s.CompletionTokens += o.CompletionTokens
	s.CostUSD += o.CostUSD
}

// ScoreUser scores up to limit of one user's pending jobs (0 = all) and
// stores their analyses, spending at most maxCostUSD. Jobs that already
// have an analysis for the user's exact profile text reuse it for free.
// It returns (nil, nil) when CORAL_API_KEY is not set, so the pipeline
// simply skips the LLM stage.
func ScoreUser(ctx context.Context, pool *pgxpool.Pool, env *config.Env, log *slog.Logger, prof *store.Profile, limit int, maxCostUSD float64, m *metrics.Registry) (*ScoreStats, error) {
	if env.CoralAPIKey == "" {
		log.Info("CORAL_API_KEY not set; skipping scoring stage")
		return nil, nil
	}
	if prof.Markdown == "" {
		return nil, scoring.ErrNoProfile
	}
	pending, err := store.PendingScoreJobs(ctx, pool, prof.UserID, prof.ProfileHash, limit)
	if err != nil {
		return nil, err
	}
	st := &ScoreStats{}
	rules := ExclusionsFor(&prof.Preferences)

	// Reuse analyses that already exist for this profile text.
	var jobs []scoring.Job
	for _, pj := range pending {
		if pj.Cached == nil {
			jobs = append(jobs, pj.Job)
			continue
		}
		stage, reason, evidence := scoring.StageFailed, "", ""
		if pj.Cached.Analysis != nil {
			stage = scoring.StageScored
			if reason, evidence = scoring.PostLLMExclusion(pj.Cached.Analysis, rules); reason != "" {
				stage = scoring.StageExcluded
			}
		}
		if err := store.LinkAnalysis(ctx, pool, prof.UserID, pj.Job.ID, pj.Cached.ID, stage, reason, evidence); err != nil {
			return st, err
		}
		st.Reused++
	}
	if len(jobs) == 0 {
		log.Info("no pending jobs to score", "user_id", prof.UserID, "reused", st.Reused)
		return st, nil
	}

	client := scoring.NewClient(scoring.ClientOptions{
		BaseURL: env.CoralBaseURL,
		APIKey:  env.CoralAPIKey,
		Model:   env.CoralModel,
		Log:     log,
	})
	scorer := &scoring.Scorer{
		Client:      client,
		Profile:     []byte(prof.Markdown),
		ProfileHash: prof.ProfileHash,
		Model:       env.CoralModel,
		Rules:       rules,
		MaxCostUSD:  maxCostUSD,
		Concurrency: env.ScoringConcurrency,
		Log:         log,
	}
	log.Info("scoring jobs", "user_id", prof.UserID, "jobs", len(jobs), "reused", st.Reused,
		"model", env.CoralModel, "concurrency", env.ScoringConcurrency,
		"max_cost_usd", fmt.Sprintf("%.4f", maxCostUSD))

	outcomes := scorer.Score(ctx, jobs)

	st.CostUSD = scorer.Spent()
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
			if err := store.SaveAnalysis(ctx, pool, prof.UserID, out.JobID, env.CoralModel, prof.ProfileHash, out); err != nil {
				log.Error("saving analysis failed", "job_id", out.JobID, "err", err)
				continue
			}
			log.Info("job scored", "user_id", prof.UserID, "job_id", out.JobID, "model", env.CoralModel,
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
	log.Info("scoring pass finished", "user_id", prof.UserID,
		"scored", st.Scored, "excluded", st.Excluded, "score_failed", st.Failed,
		"skipped", st.Skipped, "reused", st.Reused,
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
