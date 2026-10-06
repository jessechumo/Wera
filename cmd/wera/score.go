// Command dispatch for `wera score`: LLM scoring of pending jobs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"wera/internal/config"
	"wera/internal/scoring"
	"wera/internal/store"
)

// runScore implements `wera score [--limit N]`: it scores jobs in stage
// pending_score against the candidate profile via Coral Bricks, stores
// structured analyses with token/cost accounting, and applies post-LLM
// exclusions.
func runScore(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	limit := fs.Int("limit", 0, "score at most N jobs (0 = all pending)")
	profilePath := fs.String("profile", config.DefaultProfilePath, "path to the candidate profile markdown")
	rolesPath := fs.String("roles", config.DefaultRolesPath, "path to roles.yaml")
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

	profile, err := os.ReadFile(*profilePath)
	if err != nil {
		return fmt.Errorf("read %s: %w (copy profile/profile.example.md and fill it in)", *profilePath, err)
	}
	if len(profile) == 0 {
		return scoring.ErrNoProfile
	}
	roles, err := config.LoadRoles(*rolesPath)
	if err != nil {
		return err
	}

	pool, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	jobs, err := store.PendingScoreJobs(ctx, pool, *limit)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		log.Info("no pending jobs to score")
		return nil
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

	var scored, excluded, failed, skipped int
	var prompt, cached, completion int64
	for _, out := range outcomes {
		if out.Stage != "" {
			if err := store.SaveAnalysis(ctx, pool, out.JobID, env.CoralModel, profileHash, out); err != nil {
				log.Error("saving analysis failed", "job_id", out.JobID, "err", err)
				continue
			}
		}
		prompt += out.Usage.PromptTokens
		cached += out.Usage.CachedTokens
		completion += out.Usage.CompletionTokens
		switch out.Stage {
		case scoring.StageScored:
			scored++
		case scoring.StageExcluded:
			excluded++
		case scoring.StageFailed:
			failed++
		default:
			skipped++
		}
		if out.Err != nil {
			log.Warn("job skipped after error", "job_id", out.JobID, "err", out.Err)
		}
	}

	log.Info("scoring run finished",
		"scored", scored, "excluded", excluded, "score_failed", failed, "skipped", skipped,
		"prompt_tokens", prompt, "cached_tokens", cached, "completion_tokens", completion,
		"cost_usd", fmt.Sprintf("%.6f", scorer.Spent()))
	if scored+excluded+failed == 0 && skipped > 0 {
		return fmt.Errorf("no jobs were scored (%d skipped); check the log for errors or the budget guard", skipped)
	}
	return nil
}
