-- +goose Up
-- Companies are grouped by industry (config/industries.yaml) instead of the
-- original ai_infra/trading/other groups.
ALTER TABLE companies RENAME COLUMN grp TO industry;
UPDATE companies SET industry = 'ai_ml' WHERE industry = 'ai_infra';
CREATE INDEX companies_industry_idx ON companies (industry);

-- +goose Down
DROP INDEX companies_industry_idx;
UPDATE companies SET industry = 'ai_infra' WHERE industry = 'ai_ml';
ALTER TABLE companies RENAME COLUMN industry TO grp;
