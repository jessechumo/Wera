// Package pipeline implements the fetch-filter-score steps shared by the
// `wera fetch`, `wera pipeline`, and `wera worker` commands.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"wera/internal/config"
	"wera/internal/filter"
	"wera/internal/metrics"
	"wera/internal/normalize"
	"wera/internal/sources"
	"wera/internal/store"
)

// FetchStats summarizes one fetch + rule-filter pass.
type FetchStats struct {
	CompaniesOK     int
	CompaniesFailed int
	JobsSeen        int
	JobsNew         int
	JobsExcluded    int
	JobsPending     int
}

// Fetcher coordinates fetching all enabled companies concurrently.
// One company failing (404, bad token, timeout) never fails the run.
type Fetcher struct {
	Pool    *pgxpool.Pool
	Env     *config.Env
	Log     *slog.Logger
	Sources map[string]sources.Source
	Metrics *metrics.Registry // optional
}

// Run fetches every company in the list (all enabled ones when only is
// empty, otherwise the company matching only by name or token), upserts
// their jobs, closes postings that disappeared, and applies the rule
// filter to all stage='new' jobs.
func (f *Fetcher) Run(ctx context.Context, companies []config.Company, eng *filter.Engine, only string) (*FetchStats, error) {
	ids, err := store.SyncCompanies(ctx, f.Pool, companies)
	if err != nil {
		return nil, fmt.Errorf("sync companies: %w", err)
	}

	var todo []config.Company
	for _, c := range companies {
		if only != "" {
			if strings.EqualFold(c.Name, only) || c.Token == only {
				todo = append(todo, c)
			}
			continue
		}
		if c.IsEnabled() {
			todo = append(todo, c)
		}
	}
	if only != "" && len(todo) == 0 {
		return nil, fmt.Errorf("company %q not found in companies.yaml", only)
	}

	stats := &FetchStats{}
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(f.Env.FetchConcurrency)
	for _, c := range todo {
		c := c
		g.Go(func() error {
			f.fetchCompany(gctx, c, ids[c.Name], stats, &mu)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return stats, err
	}

	excluded, pending, byReason, err := store.ApplyFilters(ctx, f.Pool, eng)
	if err != nil {
		return stats, fmt.Errorf("apply filters: %w", err)
	}
	stats.JobsExcluded = excluded
	stats.JobsPending = pending
	if f.Metrics != nil {
		f.Metrics.JobsNewTotal.Add(float64(stats.JobsNew))
		for reason, n := range byReason {
			f.Metrics.JobsExcluded.WithLabelValues(reason).Add(float64(n))
		}
	}
	return stats, nil
}

// fetchCompany fetches one company end to end and updates stats/log lines.
func (f *Fetcher) fetchCompany(ctx context.Context, c config.Company, companyID int64, stats *FetchStats, mu *sync.Mutex) {
	src, err := sources.Get(f.Sources, c.ATS)
	if err == nil {
		var raw []sources.RawJob
		var seen, newly int
		start := time.Now()
		raw, err = src.Fetch(ctx, c.Token)
		if f.Metrics != nil {
			f.Metrics.FetchDuration.Observe(time.Since(start).Seconds())
		}
		if err == nil {
			seen, newly, err = f.saveCompanyJobs(ctx, c, src, companyID, raw)
		}
		if err == nil {
			f.Log.Info("company fetched", "company", c.Name, "ats", c.ATS,
				"jobs", len(raw), "new", newly, "ms", time.Since(start).Milliseconds())
			if f.Metrics != nil {
				f.Metrics.FetchTotal.WithLabelValues(c.Name, "ok").Inc()
			}
			mu.Lock()
			stats.CompaniesOK++
			stats.JobsSeen += seen
			stats.JobsNew += newly
			mu.Unlock()
			return
		}
	}

	// Any failure here is recorded and never fails the run.
	f.Log.Warn("fetch failed", "company", c.Name, "ats", c.ATS, "err", err.Error())
	if f.Metrics != nil {
		f.Metrics.FetchTotal.WithLabelValues(c.Name, "error").Inc()
	}
	if uerr := store.UpdateFetchStatus(ctx, f.Pool, companyID, false, err.Error()); uerr != nil {
		f.Log.Error("recording fetch failure failed", "company", c.Name, "err", uerr.Error())
	}
	mu.Lock()
	stats.CompaniesFailed++
	mu.Unlock()
}

// saveCompanyJobs upserts the fetched postings, closes postings that
// disappeared, and marks the fetch as successful. It returns the number
// of jobs seen and newly inserted.
func (f *Fetcher) saveCompanyJobs(ctx context.Context, c config.Company, src sources.Source, companyID int64, raw []sources.RawJob) (seen, newly int, err error) {
	inputs := make([]store.JobInput, 0, len(raw))
	openIDs := make([]string, 0, len(raw))
	for _, rj := range raw {
		openIDs = append(openIDs, rj.ExtID)
		inputs = append(inputs, store.JobInput{
			CompanyID:   companyID,
			Source:      src.Name(),
			ExtID:       rj.ExtID,
			Title:       rj.Title,
			LocationRaw: rj.LocationRaw,
			IsRemote:    rj.IsRemote,
			URL:         rj.URL,
			Department:  rj.Department,
			Description: rj.DescriptionText,
			ContentHash: normalize.ContentHash(rj.Title, rj.LocationRaw, rj.DescriptionText),
			PostedAt:    rj.PostedAt,
		})
	}
	seen, newly, err = store.SaveJobs(ctx, f.Pool, inputs)
	if err != nil {
		return seen, newly, fmt.Errorf("save jobs: %w", err)
	}
	if _, err := store.CloseMissingJobs(ctx, f.Pool, companyID, openIDs); err != nil {
		return seen, newly, fmt.Errorf("close missing: %w", err)
	}
	if err := store.UpdateFetchStatus(ctx, f.Pool, companyID, true, ""); err != nil {
		return seen, newly, fmt.Errorf("update status: %w", err)
	}
	return seen, newly, nil
}
