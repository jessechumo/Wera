package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
	"wera/internal/filter"
	"wera/internal/metrics"
	"wera/internal/store"
)

// advisoryLockKey is the fixed key (PLAN.md section 7) that serializes
// pipeline runs across processes.
const advisoryLockKey = 4242

// Pipeline is one full fetch + filter + score run, shared by
// `wera pipeline` and `wera worker`.
type Pipeline struct {
	Pool      *pgxpool.Pool
	Env       *config.Env
	Log       *slog.Logger
	Companies []config.Company
	Engine    *filter.Engine
	Metrics   *metrics.Registry // optional
}

// RunOnce executes a single pipeline run under the Postgres advisory
// lock. It returns ran=false (with a log line, no error) when another
// process already holds the lock, so overlapping runs never happen.
func (p *Pipeline) RunOnce(ctx context.Context) (ran bool, err error) {
	// The advisory lock is session-scoped, so it lives on one held
	// connection for the whole run.
	gotLock, release, err := store.TryAdvisoryLock(ctx, p.Pool, advisoryLockKey)
	if err != nil {
		return false, err
	}
	if !gotLock {
		p.Log.Warn("another pipeline run is in progress; skipping this run")
		return false, nil
	}
	defer func() {
		// Use a fresh context so a canceled worker still unlocks.
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		release(unlockCtx)
	}()

	runID, err := store.StartRun(ctx, p.Pool)
	if err != nil {
		return true, err
	}
	// Even on failure the run row must be closed; use a fresh context so
	// a canceled worker still records the outcome.
	finalize := func(totals store.RunTotals) {
		finCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if ferr := store.FinishRun(finCtx, p.Pool, runID, totals); ferr != nil {
			p.Log.Error("recording run failed", "run_id", runID, "err", ferr)
		}
	}

	fetcher := &Fetcher{Pool: p.Pool, Env: p.Env, Log: p.Log, Sources: NewSourceRegistry(p.Env), Metrics: p.Metrics}
	fstats, fetchErr := fetcher.Run(ctx, p.Companies, "")

	var excluded int
	var sstats *ScoreStats
	var scoreErr error
	if fetchErr == nil {
		excluded, sstats, scoreErr = p.matchAll(ctx)
	}

	totals := store.RunTotals{Status: "ok", JobsExcluded: excluded}
	if fstats != nil {
		totals.CompaniesOK = fstats.CompaniesOK
		totals.CompaniesFailed = fstats.CompaniesFailed
		totals.JobsSeen = fstats.JobsSeen
		totals.JobsNew = fstats.JobsNew
	}
	if sstats != nil {
		totals.JobsScored = sstats.Scored + sstats.Excluded + sstats.Failed
		totals.PromptTokens = sstats.PromptTokens
		totals.CachedTokens = sstats.CachedTokens
		totals.CompletionTokens = sstats.CompletionTokens
		totals.CostUSD = sstats.CostUSD
	}
	if ctx.Err() != nil {
		totals.Status = "failed"
		totals.Error = "run canceled: " + ctx.Err().Error()
	} else if fetchErr != nil {
		totals.Status = "failed"
		totals.Error = fetchErr.Error()
	} else if scoreErr != nil {
		totals.Status = "failed"
		totals.Error = scoreErr.Error()
	} else if totals.CompaniesFailed > 0 {
		totals.Status = "partial"
	}
	if p.Metrics != nil && totals.Status != "failed" {
		p.Metrics.RunLastSuccess.SetToCurrentTime()
	}
	finalize(totals)

	p.Log.Info("pipeline run finished", "run_id", runID, "status", totals.Status,
		"companies_ok", totals.CompaniesOK, "companies_failed", totals.CompaniesFailed,
		"jobs_seen", totals.JobsSeen, "jobs_new", totals.JobsNew,
		"jobs_excluded", totals.JobsExcluded, "jobs_scored", totals.JobsScored,
		"prompt_tokens", totals.PromptTokens, "cached_tokens", totals.CachedTokens,
		"cost_usd", fmt.Sprintf("%.6f", totals.CostUSD))

	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	return true, fetchErr
}

// matchAll filters and scores new jobs for every user with a ready
// profile. Each user's spend is limited by Allowance (per-run cap, their
// monthly budget, the global monthly cap); jobs left over stay pending.
// One user's failure is logged and does not stop the others; the first
// error is returned so the run is marked failed.
func (p *Pipeline) matchAll(ctx context.Context) (excluded int, total *ScoreStats, firstErr error) {
	profiles, err := store.ReadyProfiles(ctx, p.Pool)
	if err != nil {
		return 0, nil, fmt.Errorf("load profiles: %w", err)
	}
	total = &ScoreStats{}
	for _, prof := range profiles {
		if ctx.Err() != nil {
			return excluded, total, ctx.Err()
		}
		fst, err := FilterUser(ctx, p.Pool, p.Engine, prof, p.Metrics)
		if err != nil {
			p.Log.Error("filtering failed", "user_id", prof.UserID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		excluded += fst.Excluded
		allowance, why, err := Allowance(ctx, p.Pool, p.Env, prof.UserID)
		if err != nil {
			p.Log.Error("loading budget failed", "user_id", prof.UserID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if allowance <= 0 {
			p.Log.Warn("not scoring: "+why, "user_id", prof.UserID)
			continue
		}
		sst, err := ScoreUser(ctx, p.Pool, p.Env, p.Log, prof, 0, allowance, p.Metrics)
		total.Add(sst)
		if err != nil {
			p.Log.Error("scoring failed", "user_id", prof.UserID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return excluded, total, firstErr
}
