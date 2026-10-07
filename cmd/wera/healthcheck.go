// Command dispatch for `wera healthcheck`: exit 0 if the database is
// reachable, exit 1 otherwise. Used as the Docker healthcheck for the
// api and worker containers (PLAN.md section 13) — distroless images
// have no shell or curl, so the binary checks itself.
package main

import (
	"context"

	"wera/internal/config"
	"wera/internal/store"
)

// runHealthcheck opens (and pings) the database pool; store.Open fails
// if the database is unreachable, which main turns into exit code 1.
func runHealthcheck(ctx context.Context, args []string) error {
	env, err := config.LoadEnv()
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return nil
}
