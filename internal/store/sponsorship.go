package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SponsorshipStat summarizes what a company's postings say about visa
// sponsorship. It is built from data Wera already extracted while scoring
// (shared job facts, then older full analyses), and the company's
// certified H-1B applications from the Department of Labor's disclosures.
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
	// H-1B applications certified in the loaded period (nil: no match).
	H1BFilings    *int64   `json:"h1b_filings"`
	H1BNewHires   *int64   `json:"h1b_new_hires"`
	H1BMedianWage *float64 `json:"h1b_median_wage"`
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
		  WHERE j.added_by IS NULL),
		h AS (
		  SELECT m.company_id, count(*) AS filings, count(*) FILTER (WHERE l.new_employment) AS new_hires,
		         percentile_cont(0.5) WITHIN GROUP (ORDER BY l.wage_from) AS median
		  FROM company_h1b_employers m JOIN h1b_lca l ON l.employer_key = m.employer_key
		  GROUP BY m.company_id)
		SELECT c.id, c.name, c.industry,
		       count(*) FILTER (WHERE p.open),
		       count(p.s),
		       count(*) FILTER (WHERE p.s = 'yes'),
		       count(*) FILTER (WHERE p.s = 'no'),
		       count(*) FILTER (WHERE p.s = 'unknown'),
		       (array_agg(p.quote ORDER BY p.last_seen_at DESC) FILTER (WHERE p.quote IS NOT NULL))[1],
		       max(p.last_seen_at), h.filings, h.new_hires, h.median
		FROM companies c
		LEFT JOIN per_job p ON p.company_id = c.id
		LEFT JOIN h ON h.company_id = c.id
		WHERE $1 = '' OR c.name ILIKE '%' || $1 || '%'
		GROUP BY c.id, h.filings, h.new_hires, h.median
		HAVING count(p.s) > 0 OR h.filings > 0
		ORDER BY coalesce(h.filings, 0) DESC, count(*) FILTER (WHERE p.s = 'yes') DESC, count(p.s) DESC, c.name
		LIMIT $2`, query, normalizeLimit(limit, 50, 1000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SponsorshipStat{}
	for rows.Next() {
		var s SponsorshipStat
		if err := rows.Scan(&s.CompanyID, &s.Company, &s.Industry, &s.OpenJobs, &s.Analyzed,
			&s.Yes, &s.No, &s.Unknown, &s.LatestQuote, &s.LastSeen, &s.H1BFilings, &s.H1BNewHires, &s.H1BMedianWage); err != nil {
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
