-- +goose Up
-- Wera initial schema (PLAN.md section 6).

CREATE TABLE companies (
  id          BIGSERIAL PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  ats         TEXT NOT NULL CHECK (ats IN ('greenhouse','lever','ashby')),
  token       TEXT NOT NULL,
  grp         TEXT NOT NULL,
  enabled     BOOLEAN NOT NULL DEFAULT TRUE,
  last_fetch_at     TIMESTAMPTZ,
  last_fetch_ok     BOOLEAN,
  last_fetch_error  TEXT,
  UNIQUE (ats, token)
);

CREATE TABLE jobs (
  id              BIGSERIAL PRIMARY KEY,
  company_id      BIGINT NOT NULL REFERENCES companies(id),
  source          TEXT NOT NULL,              -- greenhouse|lever|ashby
  ext_id          TEXT NOT NULL,              -- id in the source system
  title           TEXT NOT NULL,
  location_raw    TEXT,
  is_remote       BOOLEAN,
  url             TEXT NOT NULL,
  department      TEXT,
  description     TEXT,                       -- plain text, HTML stripped
  content_hash    TEXT NOT NULL,              -- sha256(title+location+description)
  posted_at       TIMESTAMPTZ,                -- from source when available
  first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  closed_at       TIMESTAMPTZ,                -- set when it disappears from the feed
  -- rule-filter outcome
  stage           TEXT NOT NULL DEFAULT 'new' -- new|excluded|pending_score|scored|score_failed
                  CHECK (stage IN ('new','excluded','pending_score','scored','score_failed')),
  matched_categories TEXT[] NOT NULL DEFAULT '{}',
  exclude_reason  TEXT,                       -- e.g. 'title:senior', 'location:non_us', 'sponsorship:explicit_no', 'llm:years>3'
  exclude_evidence TEXT,                      -- the matched sentence, for auditing filters
  flags           TEXT[] NOT NULL DEFAULT '{}', -- e.g. {'itar'}
  UNIQUE (source, ext_id)
);
CREATE INDEX jobs_stage_idx ON jobs (stage);
CREATE INDEX jobs_first_seen_idx ON jobs (first_seen_at DESC);

CREATE TABLE analyses (
  id               BIGSERIAL PRIMARY KEY,
  job_id           BIGINT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  kind             TEXT NOT NULL DEFAULT 'score' CHECK (kind IN ('score','deep')),
  model            TEXT NOT NULL,
  profile_hash     TEXT NOT NULL,
  fit_score        INT CHECK (fit_score BETWEEN 0 AND 100),
  verdict          TEXT,                       -- strong|good|stretch|poor
  seniority        TEXT,                       -- entry|junior|mid|senior|unknown
  years_required   INT,
  sponsorship      TEXT,                       -- yes|no|unknown
  sponsorship_quote TEXT,
  work_mode        TEXT,                       -- remote|hybrid|onsite|unknown
  us_eligible      BOOLEAN,
  location_summary TEXT,
  skills_matched   TEXT[],
  skills_missing   TEXT[],
  reason           TEXT,                       -- one or two sentences
  raw              JSONB NOT NULL,             -- full model JSON
  prompt_tokens    INT, cached_tokens INT, completion_tokens INT,
  cost_usd         NUMERIC(12,6),
  latency_ms       INT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (job_id, kind, profile_hash)
);

CREATE TABLE applications (                    -- written by the dashboard; I apply manually
  job_id      BIGINT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
  status      TEXT NOT NULL DEFAULT 'saved'
              CHECK (status IN ('saved','applied','interviewing','offer','rejected','not_interested')),
  notes       TEXT,
  applied_at  TIMESTAMPTZ,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE runs (
  id              BIGSERIAL PRIMARY KEY,
  started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at     TIMESTAMPTZ,
  status          TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','ok','partial','failed')),
  companies_ok    INT DEFAULT 0,
  companies_failed INT DEFAULT 0,
  jobs_seen       INT DEFAULT 0,
  jobs_new        INT DEFAULT 0,
  jobs_excluded   INT DEFAULT 0,
  jobs_scored     INT DEFAULT 0,
  prompt_tokens   BIGINT DEFAULT 0, cached_tokens BIGINT DEFAULT 0, completion_tokens BIGINT DEFAULT 0,
  cost_usd        NUMERIC(12,6) DEFAULT 0,
  error           TEXT
);

-- +goose Down
DROP TABLE runs;
DROP TABLE applications;
DROP TABLE analyses;
DROP TABLE jobs;
DROP TABLE companies;
