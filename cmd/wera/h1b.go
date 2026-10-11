// Command dispatch for `wera h1b`: load the Department of Labor's H-1B
// disclosure files and match their employers to tracked companies.
package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"wera/internal/config"
	"wera/internal/h1b"
	"wera/internal/store"
)

const h1bUsage = `usage:
  wera h1b import FILE.xlsx [FILE.xlsx...]   load LCA disclosure files, then match employers
  wera h1b match                             rematch employers to companies (after adding companies)
  wera h1b status                            what is loaded

Download the files ("LCA Programs (H-1B, H-1B1, E-3)", one per quarter) from
https://www.dol.gov/agencies/eta/foreign-labor/performance in a browser
(the site refuses scripted downloads). A file covers a quarter or the
fiscal year to date; cases already loaded are skipped`

func runH1B(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New(h1bUsage)
	}
	env, err := config.LoadEnv()
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch args[0] {
	case "import":
		if len(args) < 2 {
			return errors.New(h1bUsage)
		}
		for _, path := range args[1:] {
			start := time.Now()
			w := store.NewH1BWriter(pool)
			read, kept, err := h1b.ReadFile(path, func(c h1b.Case) error { return w.Add(ctx, c) })
			if err != nil {
				return err
			}
			if err := w.Done(ctx, filepath.Base(path), read, kept); err != nil {
				return err
			}
			fmt.Printf("%s: %d rows, %d certified H-1B, %d new (%s)\n", filepath.Base(path), read, kept, w.Saved, time.Since(start).Round(time.Second))
		}
		fallthrough
	case "match":
		comps, err := config.LoadCompanies(config.DefaultCompaniesPath)
		if err != nil {
			return err
		}
		aliases := map[string][]string{}
		for _, c := range comps.Companies {
			aliases[c.Name] = c.H1BNames
		}
		n, err := store.MatchH1BEmployers(ctx, pool, aliases)
		if err != nil {
			return err
		}
		fmt.Printf("matched filing employers to %d companies\n", n)
		return nil
	case "status":
		var cases, employers int64
		var first, last *time.Time
		if err := pool.QueryRow(ctx, `SELECT count(*), count(DISTINCT employer_key), min(decision_date), max(decision_date) FROM h1b_lca`).
			Scan(&cases, &employers, &first, &last); err != nil {
			return err
		}
		var companies int64
		if err := pool.QueryRow(ctx, `SELECT count(DISTINCT company_id) FROM company_h1b_employers`).Scan(&companies); err != nil {
			return err
		}
		fmt.Printf("%d certified H-1B applications from %d employers", cases, employers)
		if first != nil {
			fmt.Printf(", decided %s to %s", first.Format("2006-01-02"), last.Format("2006-01-02"))
		}
		fmt.Printf("; %d tracked companies matched\n", companies)
		return nil
	}
	return errors.New(h1bUsage)
}
