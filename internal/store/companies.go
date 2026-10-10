package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
)

// SyncCompanies upserts every company from companies.yaml into the
// companies table (keyed by name) and returns name -> id.
func SyncCompanies(ctx context.Context, pool *pgxpool.Pool, comps []config.Company) (map[string]int64, error) {
	if len(comps) == 0 {
		return map[string]int64{}, nil
	}
	b := &pgx.Batch{}
	for _, c := range comps {
		b.Queue(`INSERT INTO companies (name, ats, token, industry, enabled)
		         VALUES ($1, $2, $3, $4, $5)
		         ON CONFLICT (name) DO UPDATE
		         SET ats = EXCLUDED.ats,
		               token = EXCLUDED.token,
		               industry = EXCLUDED.industry,
		               enabled = EXCLUDED.enabled`,
			c.Name, c.ATS, c.Token, c.Industry, c.IsEnabled())
	}
	br := pool.SendBatch(ctx, b)
	for range comps {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return nil, fmt.Errorf("upsert company: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return nil, err
	}

	names := make([]string, len(comps))
	for i, c := range comps {
		names[i] = c.Name
	}
	rows, err := pool.Query(ctx, `SELECT id, name FROM companies WHERE name = ANY($1)`, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[string]int64, len(comps))
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		ids[name] = id
	}
	return ids, rows.Err()
}

// UpdateFetchStatus records the outcome of one company's fetch in the
// companies table.
func UpdateFetchStatus(ctx context.Context, pool *pgxpool.Pool, companyID int64, ok bool, errMsg string) error {
	_, err := pool.Exec(ctx, `
		UPDATE companies
		SET last_fetch_at = now(),
		    last_fetch_ok = $2,
		    last_fetch_error = NULLIF($3, '')
		WHERE id = $1`, companyID, ok, errMsg)
	if err != nil {
		return fmt.Errorf("update fetch status for company %d: %w", companyID, err)
	}
	return nil
}
