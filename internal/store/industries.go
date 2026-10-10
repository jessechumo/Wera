package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// IndustryCounts holds the per-industry numbers behind GET /api/industries.
type IndustryCounts struct {
	Companies int64 `json:"companies"`
	OpenJobs  int64 `json:"open_jobs"`
	Matches   int64 `json:"matches"`   // open, scored jobs
	TopScore  *int  `json:"top_score"` // best fit score among matches
}

// CountByIndustry returns company and job counts keyed by industry id.
func CountByIndustry(ctx context.Context, pool *pgxpool.Pool) (map[string]IndustryCounts, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.industry,
		       count(DISTINCT c.id) FILTER (WHERE c.enabled),
		       count(j.id) FILTER (WHERE j.closed_at IS NULL),
		       count(j.id) FILTER (WHERE j.closed_at IS NULL AND j.stage = 'scored'),
		       max(a.fit_score) FILTER (WHERE j.closed_at IS NULL AND j.stage = 'scored')
		FROM companies c
		LEFT JOIN jobs j ON j.company_id = c.id
		LEFT JOIN LATERAL (
			SELECT fit_score FROM analyses
			WHERE job_id = j.id AND kind = 'score'
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) a ON true
		GROUP BY c.industry`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]IndustryCounts{}
	for rows.Next() {
		var id string
		var c IndustryCounts
		if err := rows.Scan(&id, &c.Companies, &c.OpenJobs, &c.Matches, &c.TopScore); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}
