-- +goose Up
-- Cheaper scoring: jobs are ranked for each user by embedding similarity
-- (computed once per posting and once per profile text), and only each
-- user's best-ranked jobs get a per-user AI score.

-- When the user last used Wera (dashboard or extension), updated at most
-- hourly. Users who stay away get fewer AI scores until they return.
ALTER TABLE users ADD COLUMN last_seen_at TIMESTAMPTZ;
UPDATE users SET last_seen_at = COALESCE(last_login_at, created_at);
ALTER TABLE users ALTER COLUMN last_seen_at SET DEFAULT now();

-- One embedding per posting, shared by every user. Text is built from the
-- posting's facts, so it is recomputed when the content (and with it the
-- facts) changes, or when the embedding model changes.
CREATE TABLE job_embeddings (
  job_id        BIGINT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
  content_hash  TEXT NOT NULL,
  model         TEXT NOT NULL,
  embedding     REAL[] NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One embedding per profile text.
CREATE TABLE profile_embeddings (
  user_id       BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  profile_hash  TEXT NOT NULL,
  model         TEXT NOT NULL,
  embedding     REAL[] NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE profile_embeddings;
DROP TABLE job_embeddings;
ALTER TABLE users DROP COLUMN last_seen_at;
