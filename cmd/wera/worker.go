// Command dispatch for `wera pipeline` and `wera worker`.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wera/internal/config"
	"wera/internal/filter"
	"wera/internal/pipeline"
	"wera/internal/store"
)

// buildPipeline loads config, YAML, the filter engine and the pool, and
// assembles the shared pipeline runner.
func buildPipeline(ctx context.Context) (*pipeline.Pipeline, error) {
	env, err := config.LoadEnv()
	if err != nil {
		return nil, err
	}
	log := config.NewLogger(env)

	comps, err := config.LoadCompanies(config.DefaultCompaniesPath)
	if err != nil {
		return nil, err
	}
	roles, err := config.LoadRoles(config.DefaultRolesPath)
	if err != nil {
		return nil, err
	}
	eng, err := filter.New(roles)
	if err != nil {
		return nil, err
	}
	pool, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return nil, err
	}

	return &pipeline.Pipeline{
		Pool:        pool,
		Env:         env,
		Log:         log,
		Companies:   comps.Companies,
		Engine:      eng,
		ProfilePath: config.DefaultProfilePath,
	}, nil
}

// runPipeline implements `wera pipeline`: one full fetch + filter + score
// run under the Postgres advisory lock. If another pipeline is running,
// this process logs and exits cleanly.
func runPipeline(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pipeline", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := buildPipeline(ctx)
	if err != nil {
		return err
	}
	defer p.Pool.Close()

	ran, err := p.RunOnce(ctx)
	if err != nil {
		return err
	}
	if !ran {
		return nil // another run held the lock; already logged
	}
	return nil
}

// runWorker implements `wera worker`: it loops the pipeline every
// RUN_INTERVAL and shuts down gracefully on SIGINT/SIGTERM.
func runWorker(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	p, err := buildPipeline(ctx)
	if err != nil {
		return err
	}
	defer p.Pool.Close()

	// Graceful shutdown: a signal cancels ctx; an in-flight run finishes
	// its current step quickly (every HTTP call honors ctx) and the run
	// row is closed even then.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	p.Log.Info("worker started", "interval", p.Env.RunInterval.String())
	for {
		if _, err := p.RunOnce(ctx); err != nil {
			// Log and keep looping: a failed run (e.g. one flaky board)
			// must not kill the worker.
			p.Log.Error("pipeline run failed", "err", err)
		}
		select {
		case <-ctx.Done():
			p.Log.Info("shutdown signal received; worker stopping")
			return nil
		case <-time.After(p.Env.RunInterval):
		}
	}
}
