-- +goose Up
-- Local relevance estimate (internal/relevance, 35..85) for each user's
-- matches: orders the LLM scoring queue and previews unscored jobs.
ALTER TABLE user_jobs ADD COLUMN estimated_score SMALLINT;
CREATE INDEX user_jobs_estimate_idx ON user_jobs (user_id, stage, estimated_score DESC);

-- +goose Down
DROP INDEX user_jobs_estimate_idx;
ALTER TABLE user_jobs DROP COLUMN estimated_score;
