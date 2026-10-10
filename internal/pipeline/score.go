package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
	"wera/internal/filter"
	"wera/internal/metrics"
	"wera/internal/relevance"
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
	if err := RankPending(ctx, pool, prof); err != nil {
		return nil, err
	}
	return &FilterStats{Excluded: excluded, Pending: pending}, nil
}

// RankPending re-ranks the user's unscored matches against their profile
// with the local relevance model (no LLM). The estimates order the LLM
// queue and preview jobs on the dashboard until real scores arrive.
func RankPending(ctx context.Context, pool *pgxpool.Pool, prof *store.Profile) error {
	docs, err := store.RankDocs(ctx, pool, prof.UserID)
	if err != nil {
		return err
	}
	return store.SetEstimates(ctx, pool, prof.UserID, relevance.Rank(prof.Markdown, docs))
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
	FactsExtracted   int // postings whose shared facts were extracted now
	ExcludedByFacts  int // excluded from shared facts, no per-user LLM call
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
	s.FactsExtracted += o.FactsExtracted
	s.ExcludedByFacts += o.ExcludedByFacts
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
// onlyIDs, when not empty, scores just those jobs (on-demand scoring of
// a job the user opened).
func ScoreUser(ctx context.Context, pool *pgxpool.Pool, env *config.Env, log *slog.Logger, prof *store.Profile, limit int, onlyIDs []int64, maxCostUSD float64, m *metrics.Registry) (*ScoreStats, error) {
	if env.CoralAPIKey == "" {
		log.Info("CORAL_API_KEY not set; skipping scoring stage")
		return nil, nil
	}
	if prof.Markdown == "" {
		return nil, scoring.ErrNoProfile
	}
	pending, err := store.PendingScoreJobs(ctx, pool, prof.UserID, prof.ProfileHash, limit, onlyIDs)
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

	// Each outcome is saved as soon as it arrives, so the dashboard shows
	// scores while the rest of the batch is still running.
	var mu sync.Mutex
	scorer.OnOutcome = func(out scoring.Outcome) {
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
			} else {
				log.Debug("job scored", "user_id", prof.UserID, "job_id", out.JobID,
					"stage", out.Stage, "fit_score", scoreInt(out.Analysis),
					"tokens", out.Usage.PromptTokens, "cached", out.Usage.CachedTokens,
					"cost", fmt.Sprintf("%.6f", out.CostUSD), "ms", out.LatencyMS)
			}
		}
		mu.Lock()
		defer mu.Unlock()
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
	extractor := &scoring.Extractor{
		Client:      client,
		Model:       env.CoralModel,
		Concurrency: env.ScoringConcurrency,
		Log:         log,
		OnResult: func(r scoring.FactsResult) {
			if r.Usage.PromptTokens > 0 {
				if err := store.RecordLLMUsage(ctx, pool, prof.UserID, "facts", env.CoralModel,
					r.Usage.PromptTokens, r.Usage.CachedTokens, r.Usage.CompletionTokens, r.CostUSD); err != nil {
					log.Warn("recording facts usage failed", "err", err)
				}
				mu.Lock()
				st.FactsExtracted++
				st.PromptTokens += r.Usage.PromptTokens
				st.CachedTokens += r.Usage.CachedTokens
				st.CompletionTokens += r.Usage.CompletionTokens
				mu.Unlock()
			}
			if r.Facts != nil {
				if err := store.SaveFacts(ctx, pool, r, env.CoralModel); err != nil {
					log.Warn("saving facts failed", "job_id", r.JobID, "err", err)
				}
			}
		},
	}

	// Work through the queue (best estimates first) in small chunks:
	// shared facts (reused, or extracted once for everyone), exclusions
	// from facts without an LLM call, then compact fit scoring. The first
	// scores land after two quick round trips instead of after the batch.
	const chunk = 24
	for start := 0; start < len(jobs) && ctx.Err() == nil; start += chunk {
		part := jobs[start:min(start+chunk, len(jobs))]
		remaining := maxCostUSD - extractor.Spent() - scorer.Spent()
		if remaining <= 0 {
			st.Skipped += len(jobs) - start
			break
		}
		ids := make([]int64, len(part))
		for i, j := range part {
			ids[i] = j.ID
		}
		facts, err := store.FactsFor(ctx, pool, ids)
		if err != nil {
			return st, err
		}
		var missing []scoring.Job
		for _, j := range part {
			if facts[j.ID] == nil {
				missing = append(missing, j)
			}
		}
		if len(missing) > 0 {
			extractor.MaxCostUSD = extractor.Spent() + remaining
			for _, r := range extractor.Extract(ctx, missing) {
				if r.Facts != nil {
					facts[r.JobID] = r.Facts
				}
			}
		}

		var toScore []scoring.Job
		for _, j := range part {
			f := facts[j.ID]
			if f != nil {
				if reason, evidence := scoring.PostLLMExclusion(scoring.Merge(f, nil), rules); reason != "" {
					if err := store.ExcludeUserJob(ctx, pool, prof.UserID, j.ID, reason, evidence); err != nil {
						log.Warn("excluding job failed", "job_id", j.ID, "err", err)
					}
					st.ExcludedByFacts++
					continue
				}
			}
			toScore = append(toScore, j)
		}
		if len(toScore) == 0 {
			continue
		}
		scorer.Facts = facts
		scorer.MaxCostUSD = scorer.Spent() + (maxCostUSD - extractor.Spent() - scorer.Spent())
		scorer.Score(ctx, toScore)
	}
	st.CostUSD = scorer.Spent() + extractor.Spent()

	log.Info("scoring pass finished", "user_id", prof.UserID,
		"scored", st.Scored, "excluded", st.Excluded, "score_failed", st.Failed,
		"skipped", st.Skipped, "reused", st.Reused,
		"facts_extracted", st.FactsExtracted, "excluded_by_facts", st.ExcludedByFacts,
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
