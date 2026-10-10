package store

import (
	"context"
	"fmt"
	"runtime"
	"sync"
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

// SaveJobs upserts one company's jobs by (source, ext_id) using one batch,
// writing only new or changed postings. It returns
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
	// Only new or changed postings are written; unchanged ones (same
	// content hash) just get last_seen_at in one statement.
	stored, err := storedHashes(ctx, pool, jobs[0].CompanyID)
	if err != nil {
		return 0, 0, err
	}
	var changed []JobInput
	var unchangedIDs []string
	for _, j := range jobs {
		if h, ok := stored[j.Source+"\x00"+j.ExtID]; ok && h == j.ContentHash {
			unchangedIDs = append(unchangedIDs, j.ExtID)
		} else {
			changed = append(changed, j)
		}
	}
	if err := TouchJobs(ctx, pool, jobs[0].CompanyID, unchangedIDs); err != nil {
		return 0, 0, err
	}
	seen = len(unchangedIDs)
	jobs = changed
	if len(jobs) == 0 {
		return seen, 0, nil
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

// FilterForUser runs the rule engine over every open job the user has no
// current outcome for (new jobs, jobs whose content changed, and all jobs
// after a preference change) and upserts the outcomes into user_jobs. It
// returns the excluded and pending counts plus a per-reason breakdown (for
// metrics). A job that passes keeps its previous score link, so the user
// still sees the old score until the job is rescored.
//
// It is built for a new user facing tens of thousands of open jobs: title
// and location rules run first on light rows, descriptions are loaded only
// for the jobs that pass them, and outcomes are written with one COPY plus
// one upsert instead of a statement per job.
func FilterForUser(ctx context.Context, pool *pgxpool.Pool, eng *filter.Engine, userID int64, prefs *config.Preferences) (excluded, pending int, byReason map[string]int, err error) {
	rows, err := pool.Query(ctx, `
		SELECT j.id, j.content_hash, j.title, COALESCE(j.location_raw, '')
		FROM jobs j
		LEFT JOIN user_jobs uj ON uj.job_id = j.id AND uj.user_id = $1
		WHERE j.closed_at IS NULL
		  AND (uj.job_id IS NULL OR uj.content_hash <> j.content_hash)`, userID)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("load jobs to filter: %w", err)
	}
	type outcome struct {
		id         int64
		hash       string
		title, loc string
		result     filter.Result
	}
	var outs []outcome
	for rows.Next() {
		var o outcome
		if err := rows.Scan(&o.id, &o.hash, &o.title, &o.loc); err != nil {
			rows.Close()
			return 0, 0, nil, err
		}
		outs = append(outs, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, nil, err
	}
	if len(outs) == 0 {
		return 0, 0, map[string]int{}, nil
	}
	// Title and location rules on every core (the engine is read-only).
	workers := runtime.GOMAXPROCS(0)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := w; i < len(outs); i += workers {
				outs[i].result = eng.ApplyTitle(prefs, outs[i].title, outs[i].loc)
			}
		}(w)
	}
	wg.Wait()
	var survivors []int64
	for _, o := range outs {
		if o.result.Stage != filter.StageExcluded {
			survivors = append(survivors, o.id)
		}
	}

	// Descriptions only for the jobs that passed the title and location rules.
	descs := make(map[int64]string, len(survivors))
	if len(survivors) > 0 {
		drows, err := pool.Query(ctx, `SELECT id, COALESCE(description, '') FROM jobs WHERE id = ANY($1::bigint[])`, survivors)
		if err != nil {
			return 0, 0, nil, fmt.Errorf("load descriptions: %w", err)
		}
		for drows.Next() {
			var id int64
			var d string
			if err := drows.Scan(&id, &d); err != nil {
				drows.Close()
				return 0, 0, nil, err
			}
			descs[id] = d
		}
		drows.Close()
		if err := drows.Err(); err != nil {
			return 0, 0, nil, err
		}
	}

	byReason = map[string]int{}
	copyRows := make([][]any, 0, len(outs))
	for i := range outs {
		o := &outs[i]
		if o.result.Stage != filter.StageExcluded {
			o.result = eng.ApplyDescription(prefs, o.result, descs[o.id])
		}
		var reason, evidence any
		if o.result.Stage == filter.StageExcluded {
			excluded++
			byReason[o.result.Reason]++
			reason, evidence = o.result.Reason, o.result.Evidence
		} else {
			pending++
		}
		copyRows = append(copyRows, []any{o.id, o.hash, o.result.Stage, o.result.Categories,
			reason, evidence, o.result.Flags})
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE filter_in (
		  job_id BIGINT, content_hash TEXT, stage TEXT, matched_categories TEXT[],
		  exclude_reason TEXT, exclude_evidence TEXT, flags TEXT[]
		) ON COMMIT DROP`); err != nil {
		return 0, 0, nil, fmt.Errorf("create staging table: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"filter_in"},
		[]string{"job_id", "content_hash", "stage", "matched_categories", "exclude_reason", "exclude_evidence", "flags"},
		pgx.CopyFromRows(copyRows)); err != nil {
		return 0, 0, nil, fmt.Errorf("copy rule outcomes: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_jobs (user_id, job_id, content_hash, stage, matched_categories,
		                       exclude_reason, exclude_evidence, flags, updated_at)
		SELECT $1, job_id, content_hash, stage, matched_categories, exclude_reason, exclude_evidence, flags, now()
		FROM filter_in
		ON CONFLICT (user_id, job_id) DO UPDATE
		SET content_hash = EXCLUDED.content_hash, stage = EXCLUDED.stage,
		    matched_categories = EXCLUDED.matched_categories,
		    exclude_reason = EXCLUDED.exclude_reason, exclude_evidence = EXCLUDED.exclude_evidence,
		    flags = EXCLUDED.flags, updated_at = now()`, userID); err != nil {
		return 0, 0, nil, fmt.Errorf("save rule outcomes: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, nil, err
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

// storedHashes maps source+NUL+ext_id to the stored content hash for a
// company's jobs.
func storedHashes(ctx context.Context, pool *pgxpool.Pool, companyID int64) (map[string]string, error) {
	rows, err := pool.Query(ctx, `SELECT source, ext_id, content_hash FROM jobs WHERE company_id = $1`, companyID)
	if err != nil {
		return nil, fmt.Errorf("load stored hashes for company %d: %w", companyID, err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var src, id, h string
		if err := rows.Scan(&src, &id, &h); err != nil {
			return nil, err
		}
		out[src+"\x00"+id] = h
	}
	return out, rows.Err()
}
