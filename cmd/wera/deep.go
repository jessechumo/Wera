// Command dispatch for `wera deep`: background-mode deep review of the
// top-scored jobs (PLAN.md sections 8 and 14, Milestone 7).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
	"wera/internal/scoring"
	"wera/internal/store"
)

// deepPending is one submitted background response awaiting a terminal
// status.
type deepPending struct {
	job       store.DeepCandidate
	respID    string
	submitted time.Time
}

// runDeep implements `wera deep [--top N]`: it picks the top scored
// jobs (latest score fit_score >= --min-score, default 75) that have no
// deep analysis for the current profile hash, submits one background
// request per job to Coral's Responses API (background: true), polls
// until every response is terminal, and stores each structured reply as
// analyses.kind='deep'. Reruns pick up only jobs still missing a review.
func runDeep(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("deep", flag.ContinueOnError)
	top := fs.Int("top", 10, "deep-review at most N jobs")
	minScore := fs.Int("min-score", 75, "only jobs whose latest score analysis fit_score is at least this")
	email := fs.String("user", "", "review this user's top jobs (email; optional with one user)")
	pollEvery := fs.Duration("poll-every", 5*time.Second, "how often to poll each background response")
	timeout := fs.Duration("timeout", 15*time.Minute, "overall wall-clock budget for the whole review")
	maxTokens := fs.Int("max-output-tokens", 8000, "max output tokens per deep review (reasoning models burn some on reasoning before the reply)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	env, err := config.LoadEnv()
	if err != nil {
		return err
	}
	if env.CoralAPIKey == "" {
		return errors.New("CORAL_API_KEY is not set: add it to .env (see .env.example)")
	}
	log := config.NewLogger(env)

	pool, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	prof, err := onlyProfile(ctx, pool, *email)
	if err != nil {
		return err
	}
	profile := []byte(prof.Markdown)
	profileHash := prof.ProfileHash
	jobs, err := store.DeepJobs(ctx, pool, prof.UserID, *top, *minScore, profileHash)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		log.Info("no jobs need a deep review", "min_score", *minScore, "top", *top)
		return nil
	}

	client := scoring.NewClient(scoring.ClientOptions{
		BaseURL: env.CoralBaseURL,
		APIKey:  env.CoralAPIKey,
		Model:   env.CoralDeepModel,
		Log:     log,
	})

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	// --- fire: submit every job's background request up front ---------
	start := time.Now()
	var queue []deepPending
	for _, dc := range jobs {
		id, err := client.CreateBackgroundResponse(ctx,
			scoring.CacheKey(profileHash), scoring.DeepInstructions,
			scoring.BuildDeepInput(profile, dc.Job), *maxTokens)
		if err != nil {
			log.Error("submit failed; job stays without a deep review",
				"job_id", dc.Job.ID, "title", dc.Job.Title, "err", err)
			continue
		}
		queue = append(queue, deepPending{job: dc, respID: id, submitted: time.Now()})
		log.Info("queued", "job_id", dc.Job.ID, "title", dc.Job.Title,
			"fit_score", dc.FitScore, "response_id", id)
	}
	if len(queue) == 0 {
		return errors.New("no background requests could be submitted")
	}
	log.Info("deep review queued", "jobs", len(queue), "model", env.CoralDeepModel)

	// --- poll: wait for every response to reach a terminal state -------
	var saved, failed int
	for len(queue) > 0 {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out with %d responses still pending", len(queue))
		case <-time.After(*pollEvery):
		}

		var still []deepPending
		for _, p := range queue {
			state, err := client.RetrieveResponse(ctx, p.respID)
			if err != nil {
				// Transient poll errors keep the response queued; the
				// next pass retries it.
				log.Warn("poll failed; will retry", "response_id", p.respID, "err", err)
				still = append(still, p)
				continue
			}
			switch {
			case state.Status == scoring.StatusCompleted:
				if err := saveDeep(ctx, pool, env, prof.UserID, profileHash, p, state, log); err != nil {
					failed++
				} else {
					saved++
				}
			case scoring.TerminalStatus(state.Status):
				failed++
				log.Error("background response ended unsuccessfully",
					"job_id", p.job.Job.ID, "response_id", p.respID,
					"status", state.Status, "detail", state.ErrText)
			default: // queued / in_progress
				still = append(still, p)
			}
		}
		queue = still
	}

	log.Info("deep review finished",
		"saved", saved, "failed", failed, "wall_s", time.Since(start).Seconds())
	if failed > 0 {
		return fmt.Errorf("deep review finished with %d saved and %d failed; rerun to retry the failures", saved, failed)
	}
	return nil
}

// saveDeep parses one completed background response and persists it as
// analyses.kind='deep'. An invalid reply is not saved (so a rerun
// retries the job); the failure is logged with the raw content.
func saveDeep(ctx context.Context, pool *pgxpool.Pool, env *config.Env, userID int64,
	profileHash string, p deepPending, state *scoring.ResponseState, log *slog.Logger) error {

	analysis, err := scoring.ParseDeepAnalysis(state.Content)
	latency := time.Since(p.submitted).Milliseconds()
	if err != nil {
		log.Warn("invalid deep JSON; not saved (rerun will retry)",
			"job_id", p.job.Job.ID, "err", err, "content", state.Content)
		return err
	}

	cost := 0.0
	if state.CostUSD != nil && *state.CostUSD > 0 {
		cost = *state.CostUSD // authoritative, from the provider
	} else if prices, ok := scoring.PriceFor(env.CoralDeepModel); ok {
		cost = prices.CostUSD(state.Usage.PromptTokens, state.Usage.CachedTokens, state.Usage.CompletionTokens)
	}

	summary := ""
	if len(analysis.WhyFit) > 0 {
		summary = analysis.WhyFit[0]
	}
	if err := store.SaveDeepAnalysis(ctx, pool, userID, p.job.Job.ID, env.CoralDeepModel,
		profileHash, p.job.FitScore, summary, scoring.StripFences(state.Content),
		state.Usage, cost, latency); err != nil {
		log.Error("save deep analysis failed", "job_id", p.job.Job.ID, "err", err)
		return err
	}

	log.Info("deep review saved",
		"job_id", p.job.Job.ID, "company", p.job.Job.Company, "title", p.job.Job.Title,
		"fit_score", p.job.FitScore, "latency_ms", latency,
		"cached_tokens", state.Usage.CachedTokens, "prompt_tokens", state.Usage.PromptTokens,
		"cost_usd", cost)
	return nil
}
