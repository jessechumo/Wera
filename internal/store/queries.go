package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// JobQuery carries every /api/jobs filter.
type JobQuery struct {
	Group           string
	Category        string
	MinScore        int
	Sponsorship     string // "yes" | "unknown" (only these two are meaningful)
	WorkMode        string
	Status          string // application status
	Q               string // title/company search
	Since           time.Time
	IncludeExcluded bool
	Sort            string // "score" (default) | "newest"
	Limit           int
	Offset          int
}

// JobView is one row of the API job list: the job plus company group,
// the latest score analysis, and the application status.
type JobView struct {
	ID              int64      `json:"id"`
	CompanyID       int64      `json:"company_id"`
	Company         string     `json:"company"`
	Group           string     `json:"group"`
	Source          string     `json:"source"`
	ExtID           string     `json:"ext_id"`
	Title           string     `json:"title"`
	LocationRaw     string     `json:"location_raw"`
	IsRemote        *bool      `json:"is_remote"`
	URL             string     `json:"url"`
	Department      *string    `json:"department"`
	PostedAt        *time.Time `json:"posted_at"`
	FirstSeenAt     time.Time  `json:"first_seen_at"`
	Stage           string     `json:"stage"`
	MatchedCategory []string   `json:"matched_categories"`
	ExcludeReason   *string    `json:"exclude_reason"`
	ExcludeEvidence *string    `json:"exclude_evidence"`
	FitScore        *int       `json:"fit_score"`
	Verdict         *string    `json:"verdict"`
	Seniority       *string    `json:"seniority"`
	YearsRequired   *int       `json:"years_required"`
	Sponsorship     *string    `json:"sponsorship"`
	WorkMode        *string    `json:"work_mode"`
	LocationSummary *string    `json:"location_summary"`
	SkillsMatched   []string   `json:"skills_matched"`
	SkillsMissing   []string   `json:"skills_missing"`
	Reason          *string    `json:"reason"`
	AppStatus       *string    `json:"application_status"`
	AppNotes        *string    `json:"application_notes"`
}

// latestAnalysisJoin selects the most recent 'score' analysis per job.
const latestAnalysisJoin = `
	LEFT JOIN LATERAL (
		SELECT fit_score, verdict, seniority, years_required, sponsorship,
		       work_mode, location_summary, skills_matched, skills_missing, reason
		FROM analyses a
		WHERE a.job_id = j.id AND a.kind = 'score'
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT 1
	) a ON true
	LEFT JOIN applications ap ON ap.job_id = j.id`

const jobViewSelect = `
	SELECT j.id, j.company_id, c.name, c.grp, j.source, j.ext_id, j.title,
	       j.location_raw, j.is_remote, j.url, j.department, j.posted_at,
	       j.first_seen_at, j.stage, j.matched_categories,
	       j.exclude_reason, j.exclude_evidence,
	       a.fit_score, a.verdict, a.seniority, a.years_required,
	       a.sponsorship, a.work_mode, a.location_summary,
	       a.skills_matched, a.skills_missing, a.reason,
	       ap.status, ap.notes
	FROM jobs j
	JOIN companies c ON c.id = j.company_id` +
	latestAnalysisJoin

// jobViewScan lists the scan targets shared by every JobView query.
func jobViewScan(v *JobView) []any {
	return []any{&v.ID, &v.CompanyID, &v.Company, &v.Group, &v.Source, &v.ExtID,
		&v.Title, &v.LocationRaw, &v.IsRemote, &v.URL, &v.Department, &v.PostedAt,
		&v.FirstSeenAt, &v.Stage, &v.MatchedCategory,
		&v.ExcludeReason, &v.ExcludeEvidence,
		&v.FitScore, &v.Verdict, &v.Seniority, &v.YearsRequired,
		&v.Sponsorship, &v.WorkMode, &v.LocationSummary,
		&v.SkillsMatched, &v.SkillsMissing, &v.Reason,
		&v.AppStatus, &v.AppNotes}
}

// normalizeLimit clamps a page size for API queries.
func normalizeLimit(limit, def, max int) int {
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}

// ListJobs runs the /api/jobs query. By default it hides excluded,
// score_failed and closed jobs; IncludeExcluded reveals them (closed jobs
// stay hidden).
func ListJobs(ctx context.Context, pool *pgxpool.Pool, q JobQuery) ([]JobView, error) {
	where := []string{"j.closed_at IS NULL"}
	args := []any{}

	if !q.IncludeExcluded {
		where = append(where, "j.stage NOT IN ('excluded','score_failed')")
	}
	if q.Group != "" {
		args = append(args, q.Group)
		where = append(where, fmt.Sprintf("c.grp = $%d", len(args)))
	}
	if q.Category != "" {
		args = append(args, q.Category)
		where = append(where, fmt.Sprintf("j.matched_categories @> ARRAY[$%d::text]", len(args)))
	}
	if q.MinScore > 0 {
		args = append(args, q.MinScore)
		where = append(where, fmt.Sprintf("a.fit_score >= $%d", len(args)))
	}
	if q.Sponsorship != "" {
		args = append(args, q.Sponsorship)
		where = append(where, fmt.Sprintf("a.sponsorship = $%d", len(args)))
	}
	if q.WorkMode != "" {
		args = append(args, q.WorkMode)
		where = append(where, fmt.Sprintf("a.work_mode = $%d", len(args)))
	}
	if q.Status != "" {
		args = append(args, q.Status)
		where = append(where, fmt.Sprintf("ap.status = $%d", len(args)))
	}
	if q.Q != "" {
		args = append(args, "%"+strings.ToLower(q.Q)+"%")
		where = append(where, fmt.Sprintf("(LOWER(j.title) LIKE $%d OR LOWER(c.name) LIKE $%d)",
			len(args), len(args)))
	}
	if !q.Since.IsZero() {
		args = append(args, q.Since)
		where = append(where, fmt.Sprintf("j.first_seen_at >= $%d", len(args)))
	}

	orderBy := "a.fit_score DESC NULLS LAST, j.first_seen_at DESC"
	if q.Sort == "newest" {
		orderBy = "j.first_seen_at DESC"
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, normalizeLimit(q.Limit, 50, 200), offset)

	sqlText := jobViewSelect + "\nWHERE " + strings.Join(where, " AND ") +
		fmt.Sprintf("\nORDER BY %s LIMIT $%d OFFSET $%d", orderBy, len(args)-1, len(args))

	rows, err := pool.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	var out []JobView
	for rows.Next() {
		var v JobView
		if err := rows.Scan(jobViewScan(&v)...); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetJob returns one JobView by id, regardless of stage (the detail view
// is always reachable; excluded jobs keep their evidence).
func GetJob(ctx context.Context, pool *pgxpool.Pool, id int64) (*JobView, error) {
	var v JobView
	err := pool.QueryRow(ctx, jobViewSelect+"\nWHERE j.id = $1", id).
		Scan(jobViewScan(&v)...)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
