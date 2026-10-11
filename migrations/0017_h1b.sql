-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Certified H-1B Labor Condition Applications from the US Department of
-- Labor's public disclosure files: what employers file before hiring or
-- extending an H-1B worker (title, occupation, wage, worksite). Only
-- business fields are kept: never contact or attorney names or emails.
CREATE TABLE h1b_lca (
  case_number     TEXT PRIMARY KEY,
  fiscal_year     INT NOT NULL,
  decision_date   DATE NOT NULL,
  employer_name   TEXT NOT NULL,
  employer_key    TEXT NOT NULL,          -- normalized name (see internal/h1b)
  job_title       TEXT NOT NULL,
  soc_code        TEXT NOT NULL DEFAULT '',
  soc_title       TEXT NOT NULL DEFAULT '',
  wage_from       NUMERIC,                -- per year
  wage_to         NUMERIC,                -- per year, NULL when a single figure
  wage_level      TEXT NOT NULL DEFAULT '',  -- prevailing wage level I to IV
  worksite_city   TEXT NOT NULL DEFAULT '',
  worksite_state  TEXT NOT NULL DEFAULT '',
  positions       INT NOT NULL DEFAULT 1,
  new_employment  BOOLEAN NOT NULL DEFAULT false, -- a new hire, not an extension
  full_time       BOOLEAN NOT NULL DEFAULT true
);
CREATE INDEX h1b_lca_employer_idx ON h1b_lca (employer_key);
CREATE INDEX h1b_lca_title_trgm_idx ON h1b_lca USING gin (job_title gin_trgm_ops);
CREATE INDEX h1b_lca_employer_trgm_idx ON h1b_lca USING gin (employer_key gin_trgm_ops);

-- Which filing employers are which tracked company (name matching, run
-- after each import).
CREATE TABLE company_h1b_employers (
  company_id    BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
  employer_key  TEXT NOT NULL,
  PRIMARY KEY (company_id, employer_key)
);
CREATE INDEX company_h1b_employers_key_idx ON company_h1b_employers (employer_key);

-- The files imported, for showing where and how recent the data is.
CREATE TABLE h1b_imports (
  id           BIGSERIAL PRIMARY KEY,
  source       TEXT NOT NULL,
  rows_read    INT NOT NULL,
  rows_kept    INT NOT NULL,
  first_date   DATE,
  last_date    DATE,
  imported_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE h1b_imports;
DROP TABLE company_h1b_employers;
DROP TABLE h1b_lca;
