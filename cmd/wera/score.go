// Command dispatch for `wera score`: LLM scoring of pending jobs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"wera/internal/config"
	"wera/internal/pipeline"
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
	if _, err := os.Stat(*profilePath); err != nil {
		return fmt.Errorf("profile: %w (copy profile/profile.example.md and fill it in)", err)
	}
	log := config.NewLogger(env)

	pool, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	st, err := pipeline.ScorePending(ctx, pool, env, log, *profilePath, *limit)
	if err != nil {
		return err
	}
	if st == nil {
		return errors.New("scoring was skipped")
	}
	if st.Scored+st.Excluded+st.Failed == 0 && st.Skipped > 0 {
		return fmt.Errorf("no jobs were scored (%d skipped); check the log for errors or the budget guard", st.Skipped)
	}
	return nil
}
