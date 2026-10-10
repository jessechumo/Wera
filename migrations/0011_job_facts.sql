-- +goose Up
-- User-independent facts about each posting, extracted once per content
-- version and shared by every user: rule-like exclusions run on them
-- without a per-user LLM call, and per-user fit scoring sees a compact job
-- card (digest + facts) instead of the full description.
CREATE TABLE job_facts (
  job_id            BIGINT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
  content_hash      TEXT NOT NULL,       -- jobs.content_hash the facts describe
  model             TEXT NOT NULL,
  facts             JSONB NOT NULL,      -- scoring.Facts
  prompt_tokens     INT NOT NULL DEFAULT 0,
  cached_tokens     INT NOT NULL DEFAULT 0,
  completion_tokens INT NOT NULL DEFAULT 0,
  cost_usd          NUMERIC(12,6) NOT NULL DEFAULT 0,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE job_facts;
