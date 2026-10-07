package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/filter"
)

// JobInput is one normalized job ready to be upserted.
type JobInput struct {
	CompanyID   int64
	Source      string
	ExtID       string
	Title       string
	LocationRaw string
	IsRemote    *bool
	URL         string
	Department  string
	Description string
	ContentHash string
	PostedAt    *time.Time
}

// SaveJobs upserts jobs by (source, ext_id) using one batch. It returns
// the number of jobs seen and how many were new insertions.
//
// Reappearance: an upsert clears closed_at and refreshes last_seen_at, so
// a reposted job is simply visible again (still deduped by content_hash).
// A changed content_hash resets stage to 'new' (dropping the old exclude
// reasons and flags) so the job is re-filtered and re-scored.
func SaveJobs(ctx context.Context, pool *pgxpool.Pool, jobs []JobInput) (seen, newly int, err error) {
	if len(jobs) == 0 {
		return 0, 0, nil
	}
	const q = `
		INSERT INTO jobs (company_id, source, ext_id, title, location_raw,
		                  is_remote, url, department, description,
		                  content_hash, posted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (source, ext_id) DO UPDATE
		SET title           = EXCLUDED.title,
		    location_raw    = EXCLUDED.location_raw,
		    is_remote       = EXCLUDED.is_remote,
		    url            = EXCLUDED.url,
		    department      = EXCLUDED.department,
		    description     = EXCLUDED.description,
		    content_hash    = EXCLUDED.content_hash,
		    posted_at       = EXCLUDED.posted_at,
		    last_seen_at    = now(),
		    closed_at       = NULL,
		    stage           = CASE WHEN jobs.content_hash = EXCLUDED.content_hash
		                           THEN jobs.stage ELSE 'new' END,
		    matched_categories = CASE WHEN jobs.content_hash = EXCLUDED.content_hash
		                              THEN jobs.matched_categories ELSE '{}' END,
		    exclude_reason    = CASE WHEN jobs.content_hash = EXCLUDED.content_hash
		                             THEN jobs.exclude_reason ELSE NULL END,
		    exclude_evidence  = CASE WHEN jobs.content_hash = EXCLUDED.content_hash
		                             THEN jobs.exclude_evidence ELSE NULL END,
		    flags             = CASE WHEN jobs.content_hash = EXCLUDED.content_hash
		                             THEN jobs.flags ELSE '{}' END
		RETURNING (xmax = 0) AS is_new`

	b := &pgx.Batch{}
	for _, j := range jobs {
		b.Queue(q, j.CompanyID, j.Source, j.ExtID, j.Title, j.LocationRaw,
			j.IsRemote, j.URL, j.Department, j.Description, j.ContentHash, j.PostedAt)
	}
	br := pool.SendBatch(ctx, b)
	defer br.Close()
	for range jobs {
		var isNew bool
		if err := br.QueryRow().Scan(&isNew); err != nil {
			return seen, newly, fmt.Errorf("upsert job: %w", err)
		}
		seen++
		if isNew {
			newly++
		}
	}
	return seen, newly, nil
}

// CloseMissingJobs marks jobs that no longer appear in a company's feed
// as closed. It is only called after a successful fetch, so a flaky feed
// never causes mass closing. Returns how many jobs were closed.
func CloseMissingJobs(ctx context.Context, pool *pgxpool.Pool, companyID int64, openExtIDs []string) (int64, error) {
	if openExtIDs == nil {
		openExtIDs = []string{}
	}
	tag, err := pool.Exec(ctx, `
		UPDATE jobs
		SET closed_at = now(), last_seen_at = now()
		WHERE company_id = $1
		  AND closed_at IS NULL
		  AND NOT (ext_id = ANY($2::text[]))`, companyID, openExtIDs)
	if err != nil {
		return 0, fmt.Errorf("close missing jobs for company %d: %w", companyID, err)
	}
	return tag.RowsAffected(), nil
}

// ApplyFilters runs the rule engine over every open job in stage 'new',
// moving them to 'excluded' or 'pending_score' in one batch. It returns
// the excluded and pending counts plus a per-reason breakdown (for
// metrics). (This is also what `wera refilter --all` reuses later;
// requeueing happens by resetting stage in SQL.)
func ApplyFilters(ctx context.Context, pool *pgxpool.Pool, eng *filter.Engine) (excluded, pending int, byReason map[string]int, err error) {
	rows, err := pool.Query(ctx, `
		SELECT id, title, COALESCE(location_raw, ''), COALESCE(description, '')
		FROM jobs
		WHERE stage = 'new'
		ORDER BY id`)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("load stage=new jobs: %w", err)
	}
	type pendingJob struct {
		id                      int64
		title, loc, description string
	}
	var jobs []pendingJob
	for rows.Next() {
		var j pendingJob
		if err := rows.Scan(&j.id, &j.title, &j.loc, &j.description); err != nil {
			rows.Close()
			return 0, 0, nil, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, nil, err
	}
	if len(jobs) == 0 {
		return 0, 0, map[string]int{}, nil
	}

	byReason = map[string]int{}
	b := &pgx.Batch{}
	for _, j := range jobs {
		res := eng.Apply(j.title, j.loc, j.description)
		if res.Stage == filter.StageExcluded {
			b.Queue(`UPDATE jobs
			         SET stage = $2, exclude_reason = $3, exclude_evidence = $4,
			             matched_categories = $5, flags = $6
			         WHERE id = $1`,
				j.id, res.Stage, res.Reason, res.Evidence, res.Categories, res.Flags)
			excluded++
			byReason[res.Reason]++
		} else {
			b.Queue(`UPDATE jobs
			         SET stage = $2, matched_categories = $3, flags = $4
			         WHERE id = $1`,
				j.id, res.Stage, res.Categories, res.Flags)
			pending++
		}
	}
	br := pool.SendBatch(ctx, b)
	for range jobs {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return 0, 0, nil, fmt.Errorf("apply filter: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return 0, 0, nil, err
	}
	return excluded, pending, byReason, nil
}
