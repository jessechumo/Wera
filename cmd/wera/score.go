// Command dispatch for `wera score`: LLM scoring of pending jobs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"wera/internal/config"
	"wera/internal/pipeline"
	"wera/internal/store"
)

// runScore implements `wera score [--limit N] [--user EMAIL]`: it scores
// each user's jobs in stage pending_score against their profile via Coral
// Bricks, stores structured analyses with token/cost accounting, and
// applies the user's post-LLM exclusions. Without --user every user with a
// ready profile is scored.
func runScore(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	limit := fs.Int("limit", 0, "score at most N jobs per user (0 = all pending)")
	email := fs.String("user", "", "score only this user's jobs (email)")
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

	profiles, err := userProfiles(ctx, pool, *email)
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return errors.New("no user has a complete profile yet")
	}
	total := &pipeline.ScoreStats{}
	for _, prof := range profiles {
		st, err := pipeline.ScoreUser(ctx, pool, env, log, prof, *limit, env.MaxCostPerRunUSD-total.CostUSD, nil)
		if err != nil {
			return err
		}
		total.Add(st)
	}
	if total.Scored+total.Excluded+total.Failed+total.Reused == 0 && total.Skipped > 0 {
		return fmt.Errorf("no jobs were scored (%d skipped); check the log for errors or the budget guard", total.Skipped)
	}
	return nil
}
