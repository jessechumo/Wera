package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
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
// A changed content_hash makes every user's rule outcome for the job stale
// (user_jobs.content_hash no longer matches), so the next filter pass
// re-filters and re-scores it.
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
		    url             = EXCLUDED.url,
		    department      = EXCLUDED.department,
		    description     = EXCLUDED.description,
		    content_hash    = EXCLUDED.content_hash,
		    posted_at       = EXCLUDED.posted_at,
		    last_seen_at    = now(),
		    closed_at       = NULL
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

// filterChunk bounds how many rule outcomes are written per batch.
const filterChunk = 2000

// FilterForUser runs the rule engine over every open job the user has no
// current outcome for (new jobs, jobs whose content changed, and all jobs
// after a preference change) and upserts the outcomes into user_jobs. It
// returns the excluded and pending counts plus a per-reason breakdown (for
// metrics). A job that passes keeps its previous score link, so the user
// still sees the old score until the job is rescored.
func FilterForUser(ctx context.Context, pool *pgxpool.Pool, eng *filter.Engine, userID int64, prefs *config.Preferences) (excluded, pending int, byReason map[string]int, err error) {
	rows, err := pool.Query(ctx, `
		SELECT j.id, j.content_hash, j.title, COALESCE(j.location_raw, ''), COALESCE(j.description, '')
		FROM jobs j
		LEFT JOIN user_jobs uj ON uj.job_id = j.id AND uj.user_id = $1
		WHERE j.closed_at IS NULL
		  AND (uj.job_id IS NULL OR uj.content_hash <> j.content_hash)
		ORDER BY j.id`, userID)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("load jobs to filter: %w", err)
	}
	type pendingJob struct {
		id                            int64
		hash, title, loc, description string
	}
	var jobs []pendingJob
	for rows.Next() {
		var j pendingJob
		if err := rows.Scan(&j.id, &j.hash, &j.title, &j.loc, &j.description); err != nil {
			rows.Close()
			return 0, 0, nil, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, nil, err
	}

	byReason = map[string]int{}
	const upsert = `
		INSERT INTO user_jobs (user_id, job_id, content_hash, stage, matched_categories,
		                       exclude_reason, exclude_evidence, flags, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (user_id, job_id) DO UPDATE
		SET content_hash = EXCLUDED.content_hash, stage = EXCLUDED.stage,
		    matched_categories = EXCLUDED.matched_categories,
		    exclude_reason = EXCLUDED.exclude_reason, exclude_evidence = EXCLUDED.exclude_evidence,
		    flags = EXCLUDED.flags, updated_at = now()`
	for start := 0; start < len(jobs); start += filterChunk {
		end := min(start+filterChunk, len(jobs))
		b := &pgx.Batch{}
		for _, j := range jobs[start:end] {
			res := eng.Apply(prefs, j.title, j.loc, j.description)
			if res.Stage == filter.StageExcluded {
				excluded++
				byReason[res.Reason]++
				b.Queue(upsert, userID, j.id, j.hash, res.Stage, res.Categories,
					res.Reason, res.Evidence, res.Flags)
			} else {
				pending++
				b.Queue(upsert, userID, j.id, j.hash, res.Stage, res.Categories,
					nil, nil, res.Flags)
			}
		}
		if err := pool.SendBatch(ctx, b).Close(); err != nil {
			return 0, 0, nil, fmt.Errorf("save rule outcomes: %w", err)
		}
	}
	return excluded, pending, byReason, nil
}

// KnownExtIDs returns the ext_ids already stored for a company (open or
// closed), so detail-per-job sources only fetch new postings.
func KnownExtIDs(ctx context.Context, pool *pgxpool.Pool, companyID int64) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `SELECT ext_id FROM jobs WHERE company_id = $1`, companyID)
	if err != nil {
		return nil, fmt.Errorf("load known jobs for company %d: %w", companyID, err)
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		known[id] = true
	}
	return known, rows.Err()
}

// TouchJobs marks already-stored postings as still listed: last_seen_at
// moves to now and a closed posting that reappeared is reopened. Their
// content is left as stored.
func TouchJobs(ctx context.Context, pool *pgxpool.Pool, companyID int64, extIDs []string) error {
	if len(extIDs) == 0 {
		return nil
	}
	_, err := pool.Exec(ctx, `
		UPDATE jobs SET last_seen_at = now(), closed_at = NULL
		WHERE company_id = $1 AND ext_id = ANY($2::text[])`, companyID, extIDs)
	if err != nil {
		return fmt.Errorf("touch jobs for company %d: %w", companyID, err)
	}
	return nil
}
