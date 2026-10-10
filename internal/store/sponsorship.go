package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SponsorshipStat summarizes what a company's postings say about visa
// sponsorship. It is built from data Wera already extracted while scoring
// (shared job facts, then older full analyses); external filing data (for
// example H-1B disclosures) can be joined in later.
type SponsorshipStat struct {
	CompanyID   int64      `json:"company_id"`
	Company     string     `json:"company"`
	Industry    string     `json:"industry"`
	OpenJobs    int64      `json:"open_jobs"`
	Analyzed    int64      `json:"analyzed"`
	Yes         int64      `json:"yes"`
	No          int64      `json:"no"`
	Unknown     int64      `json:"unknown"`
	Signal      string     `json:"signal"` // sponsors | mixed | does_not_sponsor | unclear
	LatestQuote *string    `json:"latest_quote"`
	LastSeen    *time.Time `json:"last_seen"`
}

// SponsorshipByCompany aggregates per company, optionally filtered by name.
func SponsorshipByCompany(ctx context.Context, pool *pgxpool.Pool, query string, limit int) ([]SponsorshipStat, error) {
	rows, err := pool.Query(ctx, `
		WITH per_job AS (
		  SELECT j.id, j.company_id, j.closed_at IS NULL AS open, j.last_seen_at,
		         coalesce(f.facts->>'sponsorship', a.sponsorship) AS s,
		         coalesce(f.facts->>'sponsorship_quote', a.sponsorship_quote) AS quote
		  FROM jobs j
		  LEFT JOIN job_facts f ON f.job_id = j.id
		  LEFT JOIN LATERAL (
		    SELECT sponsorship, sponsorship_quote FROM analyses
		    WHERE job_id = j.id AND kind = 'score' AND sponsorship IS NOT NULL
		    ORDER BY created_at DESC LIMIT 1) a ON f.job_id IS NULL
		  WHERE j.added_by IS NULL)
		SELECT c.id, c.name, c.industry,
		       count(*) FILTER (WHERE p.open),
		       count(p.s),
		       count(*) FILTER (WHERE p.s = 'yes'),
		       count(*) FILTER (WHERE p.s = 'no'),
		       count(*) FILTER (WHERE p.s = 'unknown'),
		       (array_agg(p.quote ORDER BY p.last_seen_at DESC) FILTER (WHERE p.quote IS NOT NULL))[1],
		       max(p.last_seen_at)
		FROM companies c JOIN per_job p ON p.company_id = c.id
		WHERE $1 = '' OR c.name ILIKE '%' || $1 || '%'
		GROUP BY c.id
		HAVING count(p.s) > 0
		ORDER BY count(*) FILTER (WHERE p.s = 'yes') DESC, count(p.s) DESC, c.name
		LIMIT $2`, query, normalizeLimit(limit, 50, 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SponsorshipStat{}
	for rows.Next() {
		var s SponsorshipStat
		if err := rows.Scan(&s.CompanyID, &s.Company, &s.Industry, &s.OpenJobs, &s.Analyzed,
			&s.Yes, &s.No, &s.Unknown, &s.LatestQuote, &s.LastSeen); err != nil {
			return nil, err
		}
		s.Signal = sponsorSignal(s.Yes, s.No)
		out = append(out, s)
	}
	return out, rows.Err()
}

func sponsorSignal(yes, no int64) string {
	switch {
	case yes > 0 && no == 0:
		return "sponsors"
	case yes > 0 && no > 0:
		return "mixed"
	case no > 0:
		return "does_not_sponsor"
	}
	return "unclear"
}
