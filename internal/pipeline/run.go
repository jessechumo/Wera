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
	Pool        *pgxpool.Pool
	Env         *config.Env
	Log         *slog.Logger
	Companies   []config.Company
	Engine      *filter.Engine
	ProfilePath string
	Metrics     *metrics.Registry // optional
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
	fstats, fetchErr := fetcher.Run(ctx, p.Companies, p.Engine, "")

	var sstats *ScoreStats
	var scoreErr error
	if fetchErr == nil {
		sstats, scoreErr = ScorePending(ctx, p.Pool, p.Env, p.Log, p.ProfilePath, 0, p.Metrics)
	}

	totals := store.RunTotals{Status: "ok"}
	if fstats != nil {
		totals.CompaniesOK = fstats.CompaniesOK
		totals.CompaniesFailed = fstats.CompaniesFailed
		totals.JobsSeen = fstats.JobsSeen
		totals.JobsNew = fstats.JobsNew
		totals.JobsExcluded = fstats.JobsExcluded
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
