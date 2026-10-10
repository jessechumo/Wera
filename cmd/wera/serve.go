// Command dispatch for `wera serve`: the REST API + metrics.
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wera/internal/api"
	"wera/internal/config"
	"wera/internal/metrics"
	"wera/internal/store"
)

// runServe implements `wera serve`: REST API on HTTP_ADDR (localhost by
// default per PLAN.md section 9) with graceful shutdown on SIGINT/SIGTERM.
func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
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

	p, err := buildPipeline(ctx)
	if err != nil {
		return err
	}
	reg := metrics.New()
	p.Metrics = reg

	inds, err := config.LoadIndustries(config.DefaultIndustriesPath)
	if err != nil {
		return err
	}
	srv := &api.Server{
		Pool:        pool,
		Log:         log,
		Metrics:     reg,
		Industries:  inds.Industries,
		RunPipeline: p.RunOnce,

		SignupEnabled: env.SignupEnabled,
		CookieSecure:  env.CookieSecure,
		PublicOrigins: env.PublicOrigins,
		TrustProxy:    env.TrustProxy,
	}

	httpServer := &http.Server{
		Addr:              env.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErr := make(chan error, 1)
	go func() { serverErr <- httpServer.ListenAndServe() }()

	// Expired sessions are useless; sweep them hourly.
	go func() {
		for {
			if n, err := store.DeleteExpiredSessions(ctx, pool); err != nil {
				log.Warn("session cleanup failed", "err", err)
			} else if n > 0 {
				log.Info("expired sessions removed", "sessions", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour):
			}
		}
	}()

	log.Info("api listening", "addr", env.HTTPAddr)
	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		log.Info("shutdown signal received; draining")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}
