// Command dispatch for `wera rescore` and `wera refilter`: maintenance
// tools that requeue jobs without new LLM cost for the refilter path.
package main

import (
	"context"
	"flag"
	"fmt"

	"wera/internal/config"
	"wera/internal/filter"
	"wera/internal/store"
)

// runRescore implements `wera rescore --all`: re-queues every open scored
// job back to pending_score so the next run scores it against the current
// profile (a changed profile produces new analyses rows; old ones are
// kept for comparison).
func runRescore(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rescore", flag.ContinueOnError)
	all := fs.Bool("all", false, "requeue all open scored jobs (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*all {
		return fmt.Errorf("rescore requires --all")
	}

	env, err := config.LoadEnv()
	if err != nil {
		return err
	}
	log := config.NewLogger(env)
	pool, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	n, err := store.RequeueScored(ctx, pool)
	if err != nil {
		return err
	}
	log.Info("requeued scored jobs for rescoring", "jobs", n)
	return nil
}

// runRefilter implements `wera refilter --all`: it re-applies the
// roles.yaml rule engine to every open job (no LLM cost). Jobs that pass
// land in pending_score and are scored on the next run.
func runRefilter(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("refilter", flag.ContinueOnError)
	all := fs.Bool("all", false, "reapply rules to all open jobs (required)")
	rolesPath := fs.String("roles", config.DefaultRolesPath, "path to roles.yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*all {
		return fmt.Errorf("refilter requires --all")
	}

	env, err := config.LoadEnv()
	if err != nil {
		return err
	}
	log := config.NewLogger(env)
	roles, err := config.LoadRoles(*rolesPath)
	if err != nil {
		return err
	}
	eng, err := filter.New(roles)
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	n, err := store.RequeueForRefilter(ctx, pool)
	if err != nil {
		return err
	}
	excluded, pending, _, err := store.ApplyFilters(ctx, pool, eng)
	if err != nil {
		return fmt.Errorf("requeued %d jobs but refiltering failed: %w", n, err)
	}
	log.Info("refiltered all open jobs", "requeued", n,
		"excluded", excluded, "pending_score", pending)
	return nil
}
