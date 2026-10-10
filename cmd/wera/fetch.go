package main

import (
	"context"
	"flag"
	"fmt"

	"wera/internal/config"
	"wera/internal/filter"
	"wera/internal/pipeline"
	"wera/internal/store"
)

// runFetch implements `wera fetch [--company X]`: fetch + normalize, then
// the rule filter for every user with a ready profile (no LLM). Every run
// is recorded in the runs table.
func runFetch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	company := fs.String("company", "", "fetch only this company (name or token)")
	companiesPath := fs.String("companies", config.DefaultCompaniesPath, "path to companies.yaml")
	rolesPath := fs.String("roles", config.DefaultRolesPath, "path to roles.yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}

	env, err := config.LoadEnv()
	if err != nil {
		return err
	}
	log := config.NewLogger(env)

	comps, err := config.LoadCompanies(*companiesPath)
	if err != nil {
		return err
	}
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

	runID, err := store.StartRun(ctx, pool)
	if err != nil {
		return err
	}

	fetcher := &pipeline.Fetcher{
		Pool:    pool,
		Env:     env,
		Log:     log,
		Sources: pipeline.NewSourceRegistry(env),
	}
	stats, err := fetcher.Run(ctx, comps.Companies, *company)

	var excluded, pending int
	if err == nil {
		var profiles []*store.Profile
		if profiles, err = store.ReadyProfiles(ctx, pool); err == nil {
			for _, prof := range profiles {
				fst, ferr := pipeline.FilterUser(ctx, pool, eng, prof, nil)
				if ferr != nil {
					err = ferr
					break
				}
				excluded += fst.Excluded
				pending += fst.Pending
			}
		}
	}

	totals := store.RunTotals{
		CompaniesOK:     stats.CompaniesOK,
		CompaniesFailed: stats.CompaniesFailed,
		JobsSeen:        stats.JobsSeen,
		JobsNew:         stats.JobsNew,
		JobsExcluded:    excluded,
	}
	totals.Status = "ok"
	if totals.CompaniesFailed > 0 {
		totals.Status = "partial"
	}
	if totals.CompaniesOK == 0 && totals.CompaniesFailed > 0 {
		totals.Status = "failed"
	}
	if err != nil {
		totals.Status = "failed"
		totals.Error = err.Error()
	}
	if ferr := store.FinishRun(ctx, pool, runID, totals); ferr != nil {
		return fmt.Errorf("recording run: %v (fetch error: %v)", ferr, err)
	}
	if err != nil {
		return err
	}

	log.Info("fetch run finished", "run_id", runID, "status", totals.Status,
		"companies_ok", totals.CompaniesOK, "companies_failed", totals.CompaniesFailed,
		"jobs_seen", totals.JobsSeen, "jobs_new", totals.JobsNew,
		"jobs_excluded", totals.JobsExcluded, "jobs_pending", pending)
	return nil
}
