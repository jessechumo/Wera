-- +goose Up
-- Inference spending limits. Every user-attributed LLM cost counts toward
-- the user's calendar-month budget: scoring and deep reviews (analyses)
-- plus other calls such as profile generation (llm_usage).
ALTER TABLE users ADD COLUMN monthly_budget_usd NUMERIC(10,2) NOT NULL DEFAULT 10;

CREATE TABLE llm_usage (
  id                BIGSERIAL PRIMARY KEY,
  user_id           BIGINT REFERENCES users(id) ON DELETE SET NULL,
  kind              TEXT NOT NULL,               -- e.g. 'profile'
  model             TEXT NOT NULL,
  prompt_tokens     INT NOT NULL DEFAULT 0,
  cached_tokens     INT NOT NULL DEFAULT 0,
  completion_tokens INT NOT NULL DEFAULT 0,
  cost_usd          NUMERIC(12,6) NOT NULL DEFAULT 0,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX llm_usage_user_created_idx ON llm_usage (user_id, created_at);
CREATE INDEX analyses_created_idx ON analyses (created_at);

-- +goose Down
DROP INDEX analyses_created_idx;
DROP TABLE llm_usage;
ALTER TABLE users DROP COLUMN monthly_budget_usd;
