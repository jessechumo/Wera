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
	"wera/internal/pipeline"
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
	roles, err := config.LoadRoles(config.DefaultRolesPath)
	if err != nil {
		return err
	}
	// After a profile save, match that user's jobs right away instead of
	// waiting for the next scheduled run.
	matchUser := func(ctx context.Context, userID int64) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		prof, err := store.GetProfile(ctx, pool, userID)
		if err != nil || prof == nil || !prof.Ready() {
			return
		}
		ex, st, err := p.MatchUser(ctx, prof)
		if err != nil {
			log.Error("matching after profile save failed", "user_id", userID, "err", err)
			return
		}
		scored := 0
		if st != nil {
			scored = st.Scored + st.Reused
		}
		log.Info("matched after profile save", "user_id", userID, "excluded", ex, "scored", scored)
	}
	// "Run now" from the dashboard. If it deferred companies for job board
	// maintenance, one catch-up run follows when the window ends (the
	// advisory lock keeps it from overlapping a worker run).
	runNow := func(ctx context.Context) (bool, error) {
		ran, err := p.RunOnce(ctx)
		if at := p.CatchUpAt; !at.IsZero() {
			log.Info("catch-up run after job board maintenance", "at", at.Add(2*time.Minute).Format(time.RFC3339))
			go func() {
				time.Sleep(time.Until(at) + 2*time.Minute)
				if _, err := p.RunOnce(context.Background()); err != nil {
					log.Error("catch-up run failed", "err", err)
				}
			}()
		}
		return ran, err
	}
	srv := &api.Server{
		Pool:        pool,
		Log:         log,
		Metrics:     reg,
		Industries:  inds.Industries,
		RunPipeline: runNow,
		Roles:       roles,
		Env:         env,
		MatchUser:   matchUser,
		PrepareUser: func(ctx context.Context, userID int64) error {
			prof, err := store.GetProfile(ctx, pool, userID)
			if err != nil || prof == nil || !prof.Ready() {
				return err
			}
			_, err = pipeline.FilterUser(ctx, pool, p.Engine, prof, reg)
			return err
		},
		ScoreNow: p.ScoreNow,

		SignupEnabled: env.SignupEnabled,
		UserBudgetUSD: env.UserBudgetUSD,
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
