// Command dispatch for `wera bench`: the scoring benchmark for the
// showcase post (PLAN.md section 8.1, Milestone 6).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"wera/internal/config"
	"wera/internal/scoring"
	"wera/internal/store"
)

// runBench implements `wera bench [--n 200]`: it re-scores N pending or
// recent jobs in parallel (without persisting anything), then the same
// --serial-n jobs with SCORING_CONCURRENCY=1, and prints throughput,
// cache percentage, cost per job, and the parallel-vs-serial speedup.
func runBench(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	n := fs.Int("n", 200, "number of jobs for the parallel batch")
	serialN := fs.Int("serial-n", 20, "number of jobs re-run serially for the speedup")
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
	log := config.NewLogger(env)

	profile, err := os.ReadFile(*profilePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", *profilePath, err)
	}
	roles, err := config.LoadRoles(config.DefaultRolesPath)
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if *serialN > *n {
		*serialN = *n
	}
	jobs, err := store.BenchJobs(ctx, pool, *n)
	if err != nil {
		return err
	}
	if len(jobs) < 2 {
		return errors.New("not enough scoreable jobs to benchmark")
	}
	nParallel := len(jobs)
	if *serialN > nParallel {
		*serialN = nParallel
	}
	log.Info("bench: scoring jobs (nothing is persisted; stages unchanged)",
		"parallel_jobs", nParallel, "serial_jobs", *serialN,
		"concurrency", env.ScoringConcurrency, "model", env.CoralModel)

	profileHash := scoring.ProfileHash(profile)
	newScorer := func(concurrency int) *scoring.Scorer {
		return &scoring.Scorer{
			Client: scoring.NewClient(scoring.ClientOptions{
				BaseURL: env.CoralBaseURL,
				APIKey:  env.CoralAPIKey,
				Model:   env.CoralModel,
				Log:     log,
			}),
			Profile:     profile,
			ProfileHash: profileHash,
			Model:       env.CoralModel,
			MaxYears:    roles.Seniority.MaxYearsRequired,
			Concurrency: concurrency,
			Log:         log,
		}
	}

	// Phase 1: parallel batch.
	start := time.Now()
	parOuts := newScorer(env.ScoringConcurrency).Score(ctx, jobs[:nParallel])
	parWall := time.Since(start)
	par := summarize(parOuts, parWall)

	// Phase 2: the same first serial-n jobs, one at a time.
	start = time.Now()
	serOuts := newScorer(1).Score(ctx, jobs[:*serialN])
	serWall := time.Since(start)
	ser := summarize(serOuts, serWall)

	printBenchReport("parallel", nParallel, env.ScoringConcurrency, par)
	printBenchReport("serial", *serialN, 1, ser)

	throughput := float64(nParallel) / parWall.Seconds()
	serThroughput := float64(*serialN) / serWall.Seconds()
	speedup := serWall.Seconds() / (parWall.Seconds() * float64(*serialN) / float64(nParallel))
	fmt.Printf("\nspeedup (concurrency %d vs 1): %.2fx\n", env.ScoringConcurrency, speedup)
	fmt.Printf("throughput: parallel %.1f jobs/s, serial %.1f jobs/s\n", throughput, serThroughput)
	return nil
}

// benchStats aggregates one benchmark phase.
type benchStats struct {
	Wall             time.Duration
	Done             int
	PromptTokens     int64
	CachedTokens     int64
	CompletionTokens int64
	CostUSD          float64
}

func summarize(outs []scoring.Outcome, wall time.Duration) benchStats {
	var s benchStats
	s.Wall = wall
	for _, o := range outs {
		if o.Stage == scoring.StageSkipped {
			continue
		}
		s.Done++
		s.PromptTokens += o.Usage.PromptTokens
		s.CachedTokens += o.Usage.CachedTokens
		s.CompletionTokens += o.Usage.CompletionTokens
		s.CostUSD += o.CostUSD
	}
	return s
}

func printBenchReport(label string, n, concurrency int, s benchStats) {
	cachedPct := 0.0
	if s.PromptTokens > 0 {
		cachedPct = float64(s.CachedTokens) / float64(s.PromptTokens) * 100
	}
	costPerJob := 0.0
	if s.Done > 0 {
		costPerJob = s.CostUSD / float64(s.Done)
	}
	fmt.Printf("\n%s batch (n=%d, concurrency=%d)\n", label, n, concurrency)
	fmt.Printf("  jobs scored: %d   wall time: %.1fs   throughput: %.1f jobs/s\n",
		s.Done, s.Wall.Seconds(), float64(s.Done)/s.Wall.Seconds())
	fmt.Printf("  prompt tokens: %d   cached: %d (%.1f%%)   completion: %d\n",
		s.PromptTokens, s.CachedTokens, cachedPct, s.CompletionTokens)
	fmt.Printf("  cost: $%.6f   cost/job: $%.6f\n", s.CostUSD, costPerJob)
}
