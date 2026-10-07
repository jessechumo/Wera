// Command wera is the single binary for the Wera job tracker. Subcommands
// are dispatched here; see PLAN.md section 11.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"wera/internal/store"
)

const usage = `wera - entry-level infrastructure job tracker

Usage:
  wera migrate                  apply database migrations
  wera companies validate       load YAML, report duplicates/bad ATS values
  wera discover <name> [slug]   try greenhouse/lever/ashby with candidate slugs
  wera fetch [--company X]      fetch + normalize + rule filter only (no LLM)
  wera score [--limit N]        score pending jobs
  wera pipeline                 one full run
  wera worker                   loop pipeline every RUN_INTERVAL
  wera serve                    REST API
  wera bench [--n 200]          scoring benchmark
  wera rescore --all            requeue after profile change
  wera refilter --all           reapply roles.yaml rules to all open jobs (no LLM cost)
  wera deep [--top N]           deep review of top-scored jobs
  wera healthcheck              exit 0 if the database is reachable (container healthchecks)
`

func main() {
	// Load .env if present. Real environment variables always win,
	// and a missing .env is not an error.
	_ = godotenv.Load()

	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]

	ctx := context.Background()

	var err error
	switch cmd {
	case "migrate":
		err = runMigrate(ctx)

	case "fetch":
		err = runFetch(ctx, args)

	case "discover":
		err = runDiscover(ctx, args)

	case "companies":
		err = runCompanies(ctx, args)

	case "score":
		err = runScore(ctx, args)

	case "pipeline":
		err = runPipeline(ctx, args)

	case "worker":
		err = runWorker(ctx, args)

	case "serve":
		err = runServe(ctx, args)

	case "bench":
		err = runBench(ctx, args)

	case "rescore":
		err = runRescore(ctx, args)

	case "refilter":
		err = runRefilter(ctx, args)

	case "deep":
		err = runDeep(ctx, args)

	case "healthcheck":
		err = runHealthcheck(ctx, args)

	case "help", "-h", "--help":
		fmt.Print(usage)

	default:
		fmt.Fprintf(os.Stderr, "wera: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "wera %s: %v\n", cmd, err)
		os.Exit(1)
	}
}

// databaseURL returns DATABASE_URL from the environment or the local default
// for a Docker-based development database. Port 5433: a native PostgreSQL
// already occupies 5432 on the dev laptop.
func databaseURL() string {
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://wera:wera@localhost:5433/wera?sslmode=disable"
}

func runMigrate(ctx context.Context) error {
	return store.Migrate(ctx, databaseURL())
}
